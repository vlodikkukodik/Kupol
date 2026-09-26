package documents

import (
	"bytes"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
)

// Content — содержимое документа, которое правит человек: всё, кроме шифра, типа и статуса
// (их меняют отдельные действия). Это же лежит в снимке версии; та же форма — тело запроса редактора.
type Content struct {
	Title      string       `json:"title"`
	Level      *int         `json:"level,omitempty"`
	DirectLink string       `json:"direct_link,omitempty"`
	Grif       string       `json:"grif,omitempty"`
	Composed   *Composed    `json:"composed"`
	Props      *Props       `json:"props,omitempty"`
	Blocks     []InputBlock `json:"blocks"`
	// IT — перевод заголовка и блоков на итальянский; nil, если перевода нет.
	IT *Translation `json:"it,omitempty"`
}

// Translation — заголовок и блоки дела на втором языке интерфейса.
type Translation struct {
	Title  string       `json:"title"`
	Blocks []InputBlock `json:"blocks"`
}

// translationsFromRaw разбирает колонку documents.translations в перевод (или nil, если пусто).
func translationsFromRaw(raw JSONText) *Translation {
	if len(raw) == 0 {
		return nil
	}
	var m map[string]Translation
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil
	}
	it, ok := m["it"]
	if !ok {
		return nil
	}
	return &it
}

// translationsToRaw сериализует перевод в форму колонки documents.translations.
func translationsToRaw(it *Translation) JSONText {
	m := map[string]Translation{}
	if it != nil {
		m["it"] = *it
	}
	raw, err := json.Marshal(m)
	if err != nil {
		return JSONText("{}")
	}
	return JSONText(raw)
}

// input собирает документ для проверки: шифр, тип и статус — от самого документа, остальное — от содержимого.
func (c Content) input(code, typ, status string) Input {
	return Input{
		Code: code, Type: typ, Status: status,
		Title: c.Title, Level: c.Level, DirectLink: c.DirectLink, Grif: c.Grif,
		Composed: c.Composed, Props: c.Props, Blocks: c.Blocks, IT: c.IT,
		// Черновик объекта живёт без шифра: номер О-№ присваивается при публикации.
		allowNoCode: true,
	}
}

func contentFromInput(in Input) Content {
	return Content{
		Title: in.Title, Level: in.Level, DirectLink: in.DirectLink, Grif: in.Grif,
		Composed: in.Composed, Props: in.Props, Blocks: in.Blocks, IT: in.IT,
	}
}

// contentFromDocument — содержимое документа в форме для редактора и снимков.
func contentFromDocument(d *Document) Content {
	return contentFromInput(inputFromDocument(d))
}

// canonical — компактный JSON содержимого: им сравнивают снимки и ищут отличия.
func (c Content) canonical() ([]byte, error) {
	if c.Blocks == nil {
		c.Blocks = []InputBlock{}
	}
	return json.Marshal(c)
}

// decodeContent разбирает содержимое; strict — неизвестные поля запрещены (запрос сохранения),
// иначе они молча отбрасываются (снимки и автосохранения читаются терпимо).
func decodeContent(raw []byte, strict bool) (Content, error) {
	var c Content
	dec := json.NewDecoder(bytes.NewReader(raw))
	if strict {
		dec.DisallowUnknownFields()
	}
	if err := dec.Decode(&c); err != nil {
		return Content{}, err
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return Content{}, errors.New("после содержимого есть лишнее")
	}
	if c.Blocks == nil {
		c.Blocks = []InputBlock{}
	}
	return c, nil
}

// applyContent переносит проверенное содержимое (prepared) в существующий документ; шифр, тип, статус, автор,
// даты и номер редакции остаются прежними.
func applyContent(dst *Document, src Document) {
	dst.Title = src.Title
	dst.Level = src.Level
	dst.DirectLink = src.DirectLink
	dst.Grif = src.Grif
	dst.ComposedYear, dst.ComposedMonth, dst.ComposedDay = src.ComposedYear, src.ComposedMonth, src.ComposedDay
	dst.DangerClass, dst.DeviationPoints, dst.Department = src.DangerClass, src.DeviationPoints, src.Department
	dst.Category, dst.ContainmentStatus, dst.DiscoveryPlace = src.Category, src.ContainmentStatus, src.DiscoveryPlace
	dst.Blocks = src.Blocks
	dst.Translations = src.Translations
}

// JSONText — значение колонки jsonb: в БД уходит строкой, читается как есть.
type JSONText []byte

func (j JSONText) Value() (driver.Value, error) {
	if len(j) == 0 {
		return "null", nil
	}
	return string(j), nil
}

func (j *JSONText) Scan(src any) error {
	switch v := src.(type) {
	case []byte:
		*j = append((*j)[:0], v...)
	case string:
		*j = JSONText(v)
	case nil:
		*j = nil
	default:
		return fmt.Errorf("documents: JSONText: неожиданный тип %T", src)
	}
	return nil
}

func (j JSONText) MarshalJSON() ([]byte, error) {
	if len(j) == 0 {
		return []byte("null"), nil
	}
	return j, nil
}

// equalJSON — одинаково ли по смыслу два JSON (порядок ключей и пробелы не важны).
func equalJSON(a, b []byte) bool {
	var x, y any
	if json.Unmarshal(a, &x) != nil || json.Unmarshal(b, &y) != nil {
		return false
	}
	return reflect.DeepEqual(x, y)
}
