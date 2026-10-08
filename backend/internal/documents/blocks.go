package documents

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"kupol/internal/i18n"
)

// Block — блок документа в том виде, как хранится в БД (канонический JSON).
// Level — минимальный уровень читателя; nil — как у документа.
type Block struct {
	ID    string          `json:"id"`
	Type  string          `json:"type"`
	Level *int            `json:"level,omitempty"`
	Data  json.RawMessage `json:"data"`
}

// InputBlock — блок в загружаемом файле; id и данные необязательны.
type InputBlock struct {
	ID    string          `json:"id"`
	Type  string          `json:"type"`
	Level *int            `json:"level"`
	Data  json.RawMessage `json:"data"`
}

// OutBlock — блок в ответе читателю. Собирается заново из разрешённых полей.
// У закрытого блока (Type == "redacted") нет ни идентификатора, ни данных, кроме нужного уровня.
type OutBlock struct {
	ID   string `json:"id,omitempty"`
	Type string `json:"type"`
	Data any    `json:"data" tstype:"unknown"`
}

// LinkTarget — то, что читатель вправе узнать о документе, на который ведёт ссылка.
type LinkTarget struct {
	Code  string
	Slug  string
	Title string
	Type  Type
	// TypeName — название типа на языке читателя; пусто — по-русски
	TypeName string
}

// LinkResolver возвращает описание цели ссылки или nil, если документа нет либо читатель его не видит.
// Оба случая неразличимы: наличие закрытого документа ссылкой не раскрывается.
type LinkResolver func(code string) *LinkTarget

const (
	maxBlocks         = 500
	blockTypeRedacted = "redacted"
)

var blockIDRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,31}$`)

// kindSpec — правила одного типа блока.
type kindSpec struct {
	decode func(raw json.RawMessage) (any, error)
	check  func(v any, p *Problems, path string)
	render func(v any, r *renderer) any
	// links — шифры документов, на которые ссылается блок (только doc_link).
	links func(v any) []string
}

type renderer struct {
	viewer  int
	resolve LinkResolver
}

var kinds = map[string]kindSpec{}

// register добавляет тип блока. T — структура данных; в файле неизвестные поля запрещены.
func register[T any](name string, check func(*T, *Problems, string), render func(*T, *renderer) any) {
	kinds[name] = newSpec(check, render)
}

// newSpec описывает тип блока по структуре данных T.
func newSpec[T any](check func(*T, *Problems, string), render func(*T, *renderer) any) kindSpec {
	return kindSpec{
		decode: func(raw json.RawMessage) (any, error) {
			var v T
			if len(bytes.TrimSpace(raw)) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
				raw = []byte("{}")
			}
			dec := json.NewDecoder(bytes.NewReader(raw))
			dec.DisallowUnknownFields()
			if err := dec.Decode(&v); err != nil {
				return nil, err
			}
			if dec.More() {
				return nil, errors.New("после данных блока есть лишнее содержимое")
			}
			return &v, nil
		},
		check:  func(v any, p *Problems, path string) { check(v.(*T), p, path) },
		render: func(v any, r *renderer) any { return render(v.(*T), r) },
	}
}

// describeJSONError переводит ошибку разбора JSON в понятное сообщение: формат и значения для Problems.Add.
func describeJSONError(err error) (string, []any) {
	var ute *json.UnmarshalTypeError
	var se *json.SyntaxError
	switch {
	case errors.As(err, &ute):
		field := ute.Field
		if field == "" {
			return "ожидается %s, получено %s", []any{jsonTypeName(ute.Type.String()), ute.Value}
		}
		return "поле %q: ожидается %s, получено %s", []any{field, jsonTypeName(ute.Type.String()), ute.Value}
	case errors.As(err, &se):
		return "некорректный JSON: %s", []any{se.Error()}
	case strings.HasPrefix(err.Error(), "json: unknown field "):
		return "неизвестное поле %s", []any{strings.TrimPrefix(err.Error(), "json: unknown field ")}
	}
	return "%s", []any{err.Error()}
}

// jsonTypeName — название типа JSON для сообщения; i18n.Msg: на итальянском его переведут при подстановке.
func jsonTypeName(goType string) any {
	switch {
	case strings.HasPrefix(goType, "[]"):
		return i18n.Msg("массив")
	case goType == "string":
		return i18n.Msg("строка")
	case strings.HasPrefix(goType, "int"), strings.HasPrefix(goType, "uint"), strings.HasPrefix(goType, "float"):
		return i18n.Msg("число")
	case goType == "bool":
		return i18n.Msg("true или false")
	case strings.Contains(goType, "Rich"):
		return i18n.Msg("строка или массив фрагментов")
	}
	return goType
}

// NormalizeBlocks проверяет блоки загружаемого документа и возвращает их в каноническом виде.
// Замечания добавляются в p с путями вида «blocks[3].data.text».
func NormalizeBlocks(in []InputBlock, p *Problems) []Block {
	return normalizeBlocksAt(in, p, "")
}

// normalizeBlocksAt — то же, но пути замечаний начинаются с base («documents[2].» для пакета).
func normalizeBlocksAt(in []InputBlock, p *Problems, base string) []Block {
	if len(in) > maxBlocks {
		p.Add(base+"blocks", "слишком много блоков (%d, не больше %d)", len(in), maxBlocks)
		return nil
	}
	out := make([]Block, 0, len(in))
	seenID := map[string]int{}
	headers := 0

	for i, ib := range in {
		path := fmt.Sprintf("%sblocks[%d]", base, i)

		id := ib.ID
		if id == "" {
			id = fmt.Sprintf("b%d", i+1) // стабильный по положению; для блоков, на которые что-то ссылается, задавайте id явно
		}
		if !blockIDRe.MatchString(id) {
			p.Add(path+".id", "идентификатор %q не подходит: строчные латинские буквы, цифры, «_» и «-», до 32 знаков, начинается с буквы или цифры", id)
		} else if prev, dup := seenID[id]; dup {
			p.Add(path+".id", "идентификатор %q уже занят блоком blocks[%d]", id, prev)
		}
		seenID[id] = i

		if ib.Level != nil && (*ib.Level < 0 || *ib.Level > MaxLevel) {
			p.Add(path+".level", "уровень должен быть от 0 до %d", MaxLevel)
		}

		if ib.Type == "" {
			p.Add(path+".type", "не указан тип блока")
			continue
		}
		spec, ok := kinds[ib.Type]
		if !ok {
			p.Add(path+".type", "неизвестный тип блока %q; допустимы: %s", ib.Type, strings.Join(KindNames(), ", "))
			continue
		}
		if ib.Type == "dossier_header" {
			if headers++; headers > 1 {
				p.Add(path+".type", "шапка досье может быть в документе только одна")
			}
		}

		v, err := spec.decode(ib.Data)
		if err != nil {
			format, args := describeJSONError(err)
			p.Add(path+".data", format, args...)
			continue
		}
		before := len(p.list)
		spec.check(v, p, path+".data")
		if len(p.list) > before {
			continue
		}
		canon, err := json.Marshal(v)
		if err != nil {
			p.Add(path+".data", "не удалось сохранить: %v", err)
			continue
		}
		out = append(out, Block{ID: id, Type: ib.Type, Level: ib.Level, Data: canon})
	}
	return out
}

// KindNames — допустимые типы блоков (в стабильном порядке).
func KindNames() []string {
	return []string{
		"heading", "paragraph", "list", "quote", "dossier_header", "experiment_log", "stamp", "memo",
		"clipping", "table", "doc_link", "divider", "page", "footnote", "appendix",
		"containment_procedure", "directive", "incident_timeline", "personnel_record", "roster", "hypothesis", "qa", "routing",
		"image", "audio",
	}
}

// LinkCodes возвращает шифры документов, на которые ссылаются блоки (для проверки битых ссылок
// и пакетного разрешения при чтении).
func LinkCodes(blocks []Block) ([]string, error) {
	var codes []string
	for _, b := range blocks {
		spec, ok := kinds[b.Type]
		if !ok || spec.links == nil {
			continue
		}
		v, err := spec.decode(b.Data)
		if err != nil {
			return nil, fmt.Errorf("блок %s: %w", b.ID, err)
		}
		codes = append(codes, spec.links(v)...)
	}
	return codes, nil
}

// RenderBlocks собирает блоки для читателя с допуском viewer.
//
// Блок, закрытый от читателя (его уровень или уровень документа выше допуска), в ответ не попадает
// вовсе: вместо него идёт метка {"type":"redacted","data":{"level":N}} без идентификатора, типа и
// содержимого. Соседние закрытые блоки одного уровня сливаются в одну метку. Данные открытых блоков
// собираются заново из известных полей, поэтому лишнее (в том числе закрытое) попасть в ответ не может.
func RenderBlocks(blocks []Block, docLevel, viewer int, resolve LinkResolver) ([]OutBlock, error) {
	r := &renderer{viewer: viewer, resolve: resolve}
	out := make([]OutBlock, 0, len(blocks))

	for _, b := range blocks {
		eff := docLevel
		if b.Level != nil && *b.Level > eff {
			eff = *b.Level
		}
		if eff > viewer {
			if n := len(out); n > 0 && out[n-1].Type == blockTypeRedacted && out[n-1].Data.(redactedData).Level == eff {
				continue
			}
			out = append(out, OutBlock{Type: blockTypeRedacted, Data: redactedData{Level: eff}})
			continue
		}
		spec, ok := kinds[b.Type]
		if !ok {
			return nil, fmt.Errorf("блок %s: неизвестный тип %q", b.ID, b.Type)
		}
		v, err := spec.decode(b.Data)
		if err != nil {
			return nil, fmt.Errorf("блок %s: %w", b.ID, err)
		}
		out = append(out, OutBlock{ID: b.ID, Type: b.Type, Data: spec.render(v, r)})
	}
	return out, nil
}

type redactedData struct {
	Level int `json:"level"`
}
