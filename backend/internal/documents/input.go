package documents

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"
)

// MaxInputBytes — предельный размер загружаемого файла (документа или пакета).
const MaxInputBytes = 4 << 20

// Input — документ в загружаемом файле (формат описан в docs/documents.md). Тот же формат
// отдаёт Export, а позже будет использовать редактор блоков.
type Input struct {
	Code       string       `json:"code,omitempty"`
	Type       string       `json:"type"`
	Title      string       `json:"title"`
	Status     string       `json:"status,omitempty"`
	Level      *int         `json:"level,omitempty"`
	DirectLink string       `json:"direct_link,omitempty"`
	Grif       string       `json:"grif,omitempty"`
	Composed   *Composed    `json:"composed"`
	Props      *Props       `json:"props,omitempty"`
	Blocks     []InputBlock `json:"blocks"`

	// allowNoCode — объект можно оставить без шифра при любом статусе (редактор: номер О-№ присвоится
	// при публикации). При загрузке из файла не задаётся: там объект без шифра допустим только опубликованным.
	allowNoCode bool
}

// Props — свойства Объекта (у других типов их нет).
type Props struct {
	DangerClass       *int   `json:"danger_class,omitempty"`
	DeviationPoints   *int   `json:"deviation_points,omitempty"`
	Department        string `json:"department,omitempty"`
	Category          string `json:"category,omitempty"`
	ContainmentStatus string `json:"containment_status,omitempty"`
	DiscoveryPlace    string `json:"discovery_place,omitempty"`
}

// ParseInputs разбирает файл: один документ или пакет {"documents": [...]} (batch — это пакет).
// Неизвестные поля — ошибка: опечатка в имени поля не должна тихо теряться.
func ParseInputs(raw []byte) (inputs []Input, batch bool, err error) {
	if len(raw) > MaxInputBytes {
		return nil, false, fmt.Errorf("файл слишком большой (%d байт, не больше %d)", len(raw), MaxInputBytes)
	}
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return nil, false, errors.New("файл пуст")
	}

	var probe map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &probe); err != nil {
		return nil, false, fmt.Errorf("некорректный JSON: %w", err)
	}
	if list, isBatch := probe["documents"]; isBatch {
		if len(probe) != 1 {
			return nil, true, errors.New(`в пакете {"documents": [...]} других полей быть не должно`)
		}
		var docs []json.RawMessage
		if err := json.Unmarshal(list, &docs); err != nil {
			return nil, true, fmt.Errorf(`"documents" должен быть массивом документов: %w`, err)
		}
		if len(docs) == 0 {
			return nil, true, errors.New(`в пакете нет документов`)
		}
		out := make([]Input, len(docs))
		var probs Problems
		for i, d := range docs {
			in, err := decodeInput(d)
			if err != nil {
				format, args := describeJSONError(err)
				probs.Add(fmt.Sprintf("documents[%d]", i), format, args...)
				continue
			}
			out[i] = in
		}
		if probs.Any() {
			return nil, true, &ValidationError{Problems: probs.List()}
		}
		return out, true, nil
	}

	in, err := decodeInput(trimmed)
	if err != nil {
		format, args := describeJSONError(err)
		return nil, false, oneProblem("$", format, args...)
	}
	return []Input{in}, false, nil
}

func decodeInput(raw []byte) (Input, error) {
	var in Input
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		return Input{}, err
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return Input{}, errors.New("после документа есть лишнее содержимое")
	}
	return in, nil
}

// prepared — проверенный документ, готовый к сохранению.
type prepared struct {
	Doc  Document
	Code *Code // nil — шифр не задан (объект, номер присвоится при публикации)
	// LinkCodes — на какие документы ссылаются блоки; DepartmentCode — отдел объекта.
	LinkCodes      []string
	DepartmentCode string
}

// prepare проверяет документ и собирает запись. path — «documents[2]» или «$».
// Все замечания копятся в p, чтобы автор увидел их сразу.
func (in *Input) prepare(path string, p *Problems) *prepared {
	at := func(field string) string {
		if path == "$" {
			return field
		}
		return path + "." + field
	}
	before := len(p.list)
	pr := &prepared{Doc: Document{Revision: 1}}
	d := &pr.Doc

	// тип и шифр
	typ := Type(in.Type)
	if !typ.Valid() {
		p.Add(at("type"), "недопустимый тип %q; допустимы: %s", in.Type, typeList())
	}
	d.Type = string(typ)

	status := Status(in.Status)
	if in.Status == "" {
		status = StatusDraft
	}
	if !status.Valid() {
		p.Add(at("status"), "недопустимый статус %q; допустимы: draft, review, published, archived", in.Status)
	}
	d.Status = string(status)

	if in.Code != "" {
		code, err := ParseCode(in.Code)
		switch {
		case err != nil:
			p.Add(at("code"), "%q — не шифр документа (примеры: О-041, ПРИКАЗ-1978-12, ИНЦ-1982-07, ЛД-0157, ОТД-2, ПРОТ-1979-03, ПОК-1980-22, МЕМО-5)", in.Code)
		case typ.Valid() && code.Type != typ:
			p.Add(at("code"), "шифр %s относится к типу %q, а в документе указан тип %q", code.Canonical, code.Type, typ)
		default:
			pr.Code = &code
			d.Code, d.Slug = &code.Canonical, &code.Slug
			d.ObjectNumber = code.ObjectNumber
		}
	} else if typ.Valid() && !(typ == TypeObject && (status == StatusPublished || in.allowNoCode)) {
		p.Add(at("code"), "не указан шифр. Номер «О-№» присваивается автоматически только при публикации объекта (status: published); остальным документам шифр нужен всегда")
	}

	// название, уровень, режим прямой ссылки, гриф
	d.Title = in.Title
	p.text(at("title"), in.Title, 1, 300)

	if in.Level != nil {
		d.Level = *in.Level
		if *in.Level < 0 || *in.Level > MaxLevel {
			p.Add(at("level"), "уровень должен быть от 0 до %d (7 — только Директорат)", MaxLevel)
		}
	}
	d.DirectLink = string(DirectLinkNotFound)
	if in.DirectLink != "" {
		d.DirectLink = in.DirectLink
		if !DirectLink(in.DirectLink).Valid() {
			p.Add(at("direct_link"), "недопустимое значение %q; допустимы: not_found (404), forbidden («Доступ запрещён»)", in.DirectLink)
		}
	}
	d.Grif = DefaultGrif
	if in.Grif != "" {
		d.Grif = in.Grif
	}
	p.text(at("grif"), d.Grif, 1, 100)

	// дата составления
	if in.Composed == nil {
		p.Add(at("composed"), "не указана дата составления (внутри вселенной): {\"year\": 1979, \"month\": 3, \"day\": 14}, месяц и день необязательны")
	} else {
		c := in.Composed
		d.ComposedYear, d.ComposedMonth, d.ComposedDay = c.Year, c.Month, c.Day
		switch {
		case c.Year < 1900 || c.Year > 2099:
			p.Add(at("composed.year"), "год должен быть от 1900 до 2099")
		case c.Month != nil && (*c.Month < 1 || *c.Month > 12):
			p.Add(at("composed.month"), "месяц должен быть от 1 до 12")
		case c.Day != nil && c.Month == nil:
			p.Add(at("composed.day"), "день нельзя указать без месяца")
		case c.Day != nil && !validDay(c.Year, *c.Month, *c.Day):
			p.Add(at("composed.day"), "в %d-м месяце %d года нет %d-го числа", *c.Month, c.Year, *c.Day)
		}
	}

	// свойства Объекта
	if in.Props != nil {
		if typ != TypeObject {
			if typ.Valid() {
				p.Add(at("props"), "свойства (класс, п.о., категория и т.д.) бывают только у типа object")
			}
		} else {
			in.Props.apply(d, pr, at("props"), p)
		}
	}

	// блоки
	base := ""
	if path != "$" {
		base = path + "."
	}
	blocks := normalizeBlocksAt(in.Blocks, p, base)
	d.Blocks = BlockList(blocks)
	if links, err := LinkCodes(blocks); err == nil {
		pr.LinkCodes = links
	}

	if len(p.list) > before {
		return nil
	}
	return pr
}

func (pr *Props) apply(d *Document, out *prepared, path string, p *Problems) {
	if pr.DangerClass != nil {
		v := *pr.DangerClass
		d.DangerClass = &v
		if v < 1 || v > 5 {
			p.Add(path+".danger_class", "класс опасности — от 1 до 5")
		}
	}
	if pr.DeviationPoints != nil {
		v := *pr.DeviationPoints
		d.DeviationPoints = &v
		if v < 0 {
			p.Add(path+".deviation_points", "пункты отклонения не могут быть отрицательными")
		}
	}
	if pr.Department != "" {
		c, err := ParseCode(pr.Department)
		if err != nil || c.Type != TypeUnit {
			p.Add(path+".department", "%q — не шифр отдела или филиала (примеры: ОТД-2, ОБ-14)", pr.Department)
		} else {
			d.Department = &c.Canonical
			out.DepartmentCode = c.Canonical
		}
	}
	if pr.Category != "" {
		d.Category = &pr.Category
		if !Category(pr.Category).Valid() {
			p.Add(path+".category", "недопустимое значение %q; допустимы: person (человек с аномальными свойствами), entity (существо или явление), place (место или локация)", pr.Category)
		}
	}
	if pr.ContainmentStatus != "" {
		d.ContainmentStatus = &pr.ContainmentStatus
		if !Containment(pr.ContainmentStatus).Valid() {
			p.Add(path+".containment_status", "недопустимое значение %q; допустимы: contained, lost, destroyed, studying", pr.ContainmentStatus)
		}
	}
	if pr.DiscoveryPlace != "" {
		d.DiscoveryPlace = &pr.DiscoveryPlace
		p.text(path+".discovery_place", pr.DiscoveryPlace, 1, 500)
	}
}

func validDay(year, month, day int) bool {
	if day < 1 || day > 31 {
		return false
	}
	t := time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.UTC)
	return t.Year() == year && int(t.Month()) == month && t.Day() == day
}

func typeList() string {
	s := ""
	for i, t := range Types {
		if i > 0 {
			s += ", "
		}
		s += string(t)
	}
	return s
}
