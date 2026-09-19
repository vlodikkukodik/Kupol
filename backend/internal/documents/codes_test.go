package documents

import (
	"errors"
	"testing"
)

func TestParseCodeCanonicalAndSlug(t *testing.T) {
	cases := []struct {
		in        string
		typ       Type
		canonical string
		slug      string
	}{
		// примеры из спецификации
		{"О-041", TypeObject, "О-041", "O-041"},
		{"ПРИКАЗ-1978-12", TypeOrder, "ПРИКАЗ-1978-12", "PRIKAZ-1978-12"},
		{"ИНЦ-1982-07", TypeIncident, "ИНЦ-1982-07", "INC-1982-07"},
		{"ЛД-0157", TypePersonnel, "ЛД-0157", "LD-0157"},
		{"ОТД-2", TypeUnit, "ОТД-2", "OTD-2"},
		{"ОБ-14", TypeUnit, "ОБ-14", "OB-14"},
		{"ПРОТ-1979-03", TypeProtocol, "ПРОТ-1979-03", "PROT-1979-03"},
		{"ПОК-1980-22", TypeTestimony, "ПОК-1980-22", "POK-1980-22"},
		{"МЕМО-5", TypeMemo, "МЕМО-5", "MEMO-5"},

		// латиница и смешанная раскладка сходятся в одну форму
		{"O-041", TypeObject, "О-041", "O-041"},
		{"o-041", TypeObject, "О-041", "O-041"},
		{"prikaz-1978-12", TypeOrder, "ПРИКАЗ-1978-12", "PRIKAZ-1978-12"},
		{"Ld-0157", TypePersonnel, "ЛД-0157", "LD-0157"},
		{"OTD-2", TypeUnit, "ОТД-2", "OTD-2"},
		{"ОTD-2", TypeUnit, "ОТД-2", "OTD-2"}, // кириллическая О + латинские TD
		{"MEMO-5", TypeMemo, "МЕМО-5", "MEMO-5"},
		{"мемо-5", TypeMemo, "МЕМО-5", "MEMO-5"},

		// нормализация числовых частей
		{"О-41", TypeObject, "О-041", "O-041"},
		{"О-0041", TypeObject, "О-041", "O-041"},
		{"О-0", TypeObject, "О-0", "O-0"},   // Праисточник
		{"О-000", TypeObject, "О-0", "O-0"}, // тот же
		{"О-1234", TypeObject, "О-1234", "O-1234"},
		{"ПРИКАЗ-1978-2", TypeOrder, "ПРИКАЗ-1978-02", "PRIKAZ-1978-02"},
		{"ИНЦ-1982-7", TypeIncident, "ИНЦ-1982-07", "INC-1982-07"},
		{"ПОК-1980-022", TypeTestimony, "ПОК-1980-22", "POK-1980-22"},
		{"ЛД-157", TypePersonnel, "ЛД-0157", "LD-0157"},
		{"ОТД-02", TypeUnit, "ОТД-2", "OTD-2"},
		{"МЕМО-005", TypeMemo, "МЕМО-5", "MEMO-5"},

		// пробелы по краям, разные тире и подчёркивание вместо дефиса
		{"  О-041  ", TypeObject, "О-041", "O-041"},
		{"О-041\n", TypeObject, "О-041", "O-041"}, // перевод строки из файла или ввода
		{"О–041", TypeObject, "О-041", "O-041"},
		{"О—041", TypeObject, "О-041", "O-041"},
		{"О−041", TypeObject, "О-041", "O-041"},
		{"ПРИКАЗ—1978—12", TypeOrder, "ПРИКАЗ-1978-12", "PRIKAZ-1978-12"},
		{"O_041", TypeObject, "О-041", "O-041"},
	}
	for _, c := range cases {
		got, err := ParseCode(c.in)
		if err != nil {
			t.Errorf("%q: %v", c.in, err)
			continue
		}
		if got.Type != c.typ || got.Canonical != c.canonical || got.Slug != c.slug {
			t.Errorf("%q: тип=%s канон=%q slug=%q; ожидалось %s %q %q", c.in, got.Type, got.Canonical, got.Slug, c.typ, c.canonical, c.slug)
		}
	}
}

func TestParseCodeRejects(t *testing.T) {
	for _, in := range []string{
		"", "   ", "О", "О-", "О-х", "О-12345", "О--1", "О-1-2",
		"ПРИКАЗ-78-12", "ПРИКАЗ-1978", "ПРИКАЗ-1978-1234", "ПРИКАЗ-1978-",
		"ИНЦ-1982", "ЛД-12345", "ЛД-", "ОТД-1234", "ОТД", "ОБ-", "МЕМО-", "МЕМО-12345",
		"ХХХ-001", "OBJECT-1", "DOC-1", "О 041", "О.041", "О/041", "../О-041", "О-041/../x",
		"О-041 ; DROP", "О-٠٤١", // арабо-индийские цифры
		"ПРИКАЗ-1978-12-1",
	} {
		if c, err := ParseCode(in); !errors.Is(err, ErrBadCode) {
			t.Errorf("%q принят как %+v", in, c)
		}
	}
}

func TestParseCodeIsIdempotent(t *testing.T) {
	for _, in := range []string{"O-41", "ПРИКАЗ-1978-2", "ld-157", "МЕМО-005", "ОТД-02", "О-0"} {
		a, err := ParseCode(in)
		if err != nil {
			t.Fatal(err)
		}
		same := func(x Code) bool { return x.Type == a.Type && x.Canonical == a.Canonical && x.Slug == a.Slug }
		b, err := ParseCode(a.Canonical)
		if err != nil || !same(b) {
			t.Errorf("%q: повторный разбор канона %q дал %+v, %v", in, a.Canonical, b, err)
		}
		c, err := ParseCode(a.Slug)
		if err != nil || !same(c) {
			t.Errorf("%q: разбор slug %q дал %+v, %v", in, a.Slug, c, err)
		}
	}
}

func TestParseCodeObjectNumber(t *testing.T) {
	c, _ := ParseCode("О-041")
	if c.ObjectNumber == nil || *c.ObjectNumber != 41 {
		t.Fatalf("номер: %v", c.ObjectNumber)
	}
	z, _ := ParseCode("О-0")
	if z.ObjectNumber == nil || *z.ObjectNumber != 0 {
		t.Fatalf("О-0: %v", z.ObjectNumber)
	}
	o, _ := ParseCode("ПРИКАЗ-1978-12")
	if o.ObjectNumber != nil {
		t.Errorf("у приказа нет номера объекта: %v", o.ObjectNumber)
	}
}

func TestObjectCode(t *testing.T) {
	for n, want := range map[int]string{0: "О-0", 1: "О-001", 41: "О-041", 999: "О-999", 1000: "О-1000", 9999: "О-9999"} {
		if got := ObjectCode(n).Canonical; got != want {
			t.Errorf("ObjectCode(%d) = %q, ожидалось %q", n, got, want)
		}
	}
}

func TestSlugsAreDistinctForDifferentCodes(t *testing.T) {
	seen := map[string]string{}
	for _, in := range []string{"О-1", "О-01", "ЛД-1", "ОТД-1", "ОБ-1", "МЕМО-1", "ПРИКАЗ-1978-1", "ИНЦ-1978-1", "ПРОТ-1978-1", "ПОК-1978-1"} {
		c, err := ParseCode(in)
		if err != nil {
			t.Fatal(err)
		}
		if prev, dup := seen[c.Slug]; dup && prev != c.Canonical {
			t.Errorf("slug %q у двух разных шифров: %q и %q", c.Slug, prev, c.Canonical)
		}
		seen[c.Slug] = c.Canonical
	}
}

func TestTypeNames(t *testing.T) {
	for _, ty := range Types {
		if !ty.Valid() || ty.Name() == "" {
			t.Errorf("тип %q: valid=%v name=%q", ty, ty.Valid(), ty.Name())
		}
	}
	if Type("ghost").Valid() || Type("ghost").Name() != "" {
		t.Error("неизвестный тип принят")
	}
	if len(Types) != len(typeNames) || len(Types) != len(codeSpecs) {
		t.Errorf("списки типов расходятся: %d/%d/%d", len(Types), len(typeNames), len(codeSpecs))
	}
}
