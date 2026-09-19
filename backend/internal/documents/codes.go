// Package documents — документы архива: шифры, блоки, уровни допуска, чтение, каталог и загрузка.
package documents

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Type — тип документа (спецификация §6).
type Type string

const (
	TypeObject    Type = "object"    // Объект                 О-041
	TypeOrder     Type = "order"     // Приказ                 ПРИКАЗ-1978-12
	TypeIncident  Type = "incident"  // Инцидент               ИНЦ-1982-07
	TypePersonnel Type = "personnel" // Персонал (личное дело) ЛД-0157
	TypeUnit      Type = "unit"      // Отдел / филиал         ОТД-2, ОБ-14
	TypeProtocol  Type = "protocol"  // Протокол эксперимента  ПРОТ-1979-03
	TypeTestimony Type = "testimony" // Свидетельские показания ПОК-1980-22
	TypeMemo      Type = "memo"      // Меморандум             МЕМО-5
)

// Types — все типы в порядке показа в каталоге.
var Types = []Type{TypeObject, TypeOrder, TypeIncident, TypePersonnel, TypeUnit, TypeProtocol, TypeTestimony, TypeMemo}

var typeNames = map[Type]string{
	TypeObject:    "Объект",
	TypeOrder:     "Приказ",
	TypeIncident:  "Инцидент",
	TypePersonnel: "Личное дело",
	TypeUnit:      "Отдел / филиал",
	TypeProtocol:  "Протокол эксперимента",
	TypeTestimony: "Свидетельские показания",
	TypeMemo:      "Меморандум",
}

// Name — название типа для интерфейса.
func (t Type) Name() string { return typeNames[t] }

// Valid — известный тип.
func (t Type) Valid() bool { _, ok := typeNames[t]; return ok }

// codeSpec описывает формат шифра одного типа: латинский шаблон (для slug) и кириллический префикс.
type codeSpec struct {
	typ Type
	// slugRe — шаблон шифра латиницей (после транслитерации); группы — числовые части.
	slugRe *regexp.Regexp
	// prefixes: латинский префикс -> кириллический
	prefixes map[string]string
}

var codeSpecs = []codeSpec{
	{TypeObject, regexp.MustCompile(`^(O)-(\d{1,4})$`), map[string]string{"O": "О"}},
	{TypeOrder, regexp.MustCompile(`^(PRIKAZ)-(\d{4})-(\d{1,3})$`), map[string]string{"PRIKAZ": "ПРИКАЗ"}},
	{TypeIncident, regexp.MustCompile(`^(INC)-(\d{4})-(\d{1,3})$`), map[string]string{"INC": "ИНЦ"}},
	{TypePersonnel, regexp.MustCompile(`^(LD)-(\d{1,4})$`), map[string]string{"LD": "ЛД"}},
	{TypeUnit, regexp.MustCompile(`^(OTD|OB)-(\d{1,3})$`), map[string]string{"OTD": "ОТД", "OB": "ОБ"}},
	{TypeProtocol, regexp.MustCompile(`^(PROT)-(\d{4})-(\d{1,3})$`), map[string]string{"PROT": "ПРОТ"}},
	{TypeTestimony, regexp.MustCompile(`^(POK)-(\d{4})-(\d{1,3})$`), map[string]string{"POK": "ПОК"}},
	{TypeMemo, regexp.MustCompile(`^(MEMO)-(\d{1,4})$`), map[string]string{"MEMO": "МЕМО"}},
}

// translit — кириллица -> латиница (только заглавные; регистр приводится заранее).
var translit = map[rune]string{
	'А': "A", 'Б': "B", 'В': "V", 'Г': "G", 'Д': "D", 'Е': "E", 'Ё': "E", 'Ж': "ZH", 'З': "Z", 'И': "I",
	'Й': "Y", 'К': "K", 'Л': "L", 'М': "M", 'Н': "N", 'О': "O", 'П': "P", 'Р': "R", 'С': "S", 'Т': "T",
	'У': "U", 'Ф': "F", 'Х': "H", 'Ц': "C", 'Ч': "CH", 'Ш': "SH", 'Щ': "SCH", 'Ъ': "", 'Ы': "Y", 'Ь': "",
	'Э': "E", 'Ю': "YU", 'Я': "YA",
}

// Code — разобранный шифр документа.
type Code struct {
	Type Type
	// Canonical — шифр в каноническом виде, кириллицей: «О-041», «ПРИКАЗ-1978-12».
	Canonical string
	// Slug — тот же шифр латиницей для адреса: «O-041», «PRIKAZ-1978-12».
	Slug string
	// ObjectNumber — номер О-№ (только у Объектов).
	ObjectNumber *int
}

// ErrBadCode — строка не является шифром ни одного типа.
var ErrBadCode = errors.New("documents: некорректный шифр")

// hyphens — разные тире, которые вводят вместо дефиса.
var hyphens = strings.NewReplacer("–", "-", "—", "-", "−", "-", "‑", "-", "‐", "-", "_", "-")

// toLatin приводит ввод к «латинской» форме шифра: заглавные буквы, кириллица транслитерирована.
// Смешанная раскладка («O-041» с латинской O или «О-041» с кириллической) сходится в одну форму.
func toLatin(input string) string {
	s := strings.ToUpper(strings.TrimSpace(input))
	s = hyphens.Replace(s)
	var b strings.Builder
	for _, r := range s {
		if lat, ok := translit[r]; ok {
			b.WriteString(lat)
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// ParseCode разбирает шифр в любой раскладке и регистре и возвращает его в каноническом виде.
// Числовые части нормализуются: номер объекта — не короче 3 цифр (О-41 -> О-041, но О-0 остаётся),
// порядковый номер приказа, инцидента, протокола и показаний — не короче 2 (…-1978-2 -> …-1978-02),
// личное дело — ровно 4 цифры (ЛД-157 -> ЛД-0157).
func ParseCode(input string) (Code, error) {
	lat := toLatin(input)
	for _, spec := range codeSpecs {
		m := spec.slugRe.FindStringSubmatch(lat)
		if m == nil {
			continue
		}
		prefix := m[1]
		cyr := spec.prefixes[prefix]

		var parts []string
		var objNum *int
		switch spec.typ {
		case TypeObject:
			n, _ := strconv.Atoi(m[2])
			objNum = &n
			parts = []string{objectDigits(n)}
		case TypePersonnel:
			n, _ := strconv.Atoi(m[2])
			parts = []string{fmt.Sprintf("%04d", n)}
		case TypeUnit, TypeMemo:
			n, _ := strconv.Atoi(m[2])
			parts = []string{strconv.Itoa(n)}
		default: // ПРИКАЗ, ИНЦ, ПРОТ, ПОК: год + порядковый номер
			n, _ := strconv.Atoi(m[3])
			parts = []string{m[2], fmt.Sprintf("%02d", n)}
		}
		tail := strings.Join(parts, "-")
		return Code{
			Type:         spec.typ,
			Canonical:    cyr + "-" + tail,
			Slug:         prefix + "-" + tail,
			ObjectNumber: objNum,
		}, nil
	}
	return Code{}, ErrBadCode
}

// objectDigits — цифры номера объекта: О-0 остаётся «0», прочие дополняются нулями до трёх знаков.
func objectDigits(n int) string {
	if n == 0 {
		return "0"
	}
	return fmt.Sprintf("%03d", n)
}

// ObjectCode возвращает шифр объекта с номером n (О-№ присваивается автоматически при публикации).
func ObjectCode(n int) Code {
	c, _ := ParseCode("О-" + strconv.Itoa(n))
	return c
}

// MaxObjectNumber — наибольший допустимый номер объекта (четыре знака).
const MaxObjectNumber = 9999
