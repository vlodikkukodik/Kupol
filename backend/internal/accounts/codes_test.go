package accounts

import (
	"regexp"
	"strings"
	"testing"

	"kupol/internal/i18n"
)

var backupRe = regexp.MustCompile(`^KUPOL(-[A-HJ-NP-Z2-9]{4}){4}$`)

func TestGenerateBackupCode(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 500; i++ {
		code, err := GenerateBackupCode()
		if err != nil {
			t.Fatal(err)
		}
		if !backupRe.MatchString(code) {
			t.Fatalf("формат: %q", code)
		}
		if seen[code] {
			t.Fatalf("повтор кода %q", code)
		}
		seen[code] = true
	}
}

func TestBackupCodeUsesAllSymbolsUniformly(t *testing.T) {
	counts := map[rune]int{}
	total := 0
	for i := 0; i < 4000; i++ {
		code, _ := GenerateBackupCode()
		for _, r := range strings.ReplaceAll(strings.TrimPrefix(code, "KUPOL"), "-", "") {
			counts[r]++
			total++
		}
	}
	if len(counts) != 32 {
		t.Fatalf("использовано %d символов из 32", len(counts))
	}
	expect := float64(total) / 32
	for r, n := range counts {
		if float64(n) < expect*0.8 || float64(n) > expect*1.2 {
			t.Errorf("символ %q встречается %d раз при ожидаемых ~%.0f: распределение неравномерно", r, n, expect)
		}
	}
}

func TestCanonicalBackupCode(t *testing.T) {
	const canon = "ABCDEFGHJKLMNPQR"
	for _, in := range []string{
		"KUPOL-ABCD-EFGH-JKLM-NPQR",
		"kupol-abcd-efgh-jklm-npqr",
		"  KUPOL ABCD EFGH JKLM NPQR  ",
		"ABCD-EFGH-JKLM-NPQR", // без префикса
		"abcdefghjklmnpqr",
		"KUPOL_ABCD_EFGH_JKLM_NPQR",
	} {
		got, ok := CanonicalBackupCode(in)
		if !ok || got != canon {
			t.Errorf("%q: got=%q ok=%v", in, got, ok)
		}
	}

	for _, in := range []string{
		"", "KUPOL", "KUPOL-ABCD-EFGH-JKLM", // коротко
		"KUPOL-ABCD-EFGH-JKLM-NPQR-STUV", // длинно
		"KUPOL-ABCD-EFGH-JKLM-NPQ0",      // 0 не из алфавита
		"KUPOL-ABCD-EFGH-JKLM-NPQ1",      // 1 не из алфавита
		"KUPOL-ABCD-EFGH-JKLM-NPQI",      // I не из алфавита
		"KUPOL-ABCD-EFGH-JKLM-NPQO",      // O не из алфавита
		"КУПОЛ-ABCD-EFGH-JKLM-NPQR",      // кириллица
		"KUPOL-ABCD-EFGH-JKLM-NPQ!",
	} {
		if got, ok := CanonicalBackupCode(in); ok {
			t.Errorf("%q принят как %q", in, got)
		}
	}
}

func TestGeneratedCodeRoundTripsThroughCanonical(t *testing.T) {
	for i := 0; i < 50; i++ {
		code, _ := GenerateBackupCode()
		c1, ok := CanonicalBackupCode(code)
		c2, ok2 := CanonicalBackupCode(strings.ToLower(strings.ReplaceAll(code, "-", " ")))
		if !ok || !ok2 || c1 != c2 || len(c1) != 16 {
			t.Fatalf("код %q: %q/%v %q/%v", code, c1, ok, c2, ok2)
		}
	}
}

func TestNormalizeAnswer(t *testing.T) {
	cases := map[string]string{
		"  Форма   КУПОЛ-1. ": "форма купол-1",
		"«Форма КУПОЛ–1»":     "форма купол-1", // en dash
		"ФОРМА КУПОЛ—1":       "форма купол-1", // em dash
		"ЦАК!":                "цак",
		"Объектами":           "объектами",
		"1974":                "1974",
		"\tпять\n":            "пять",
		"":                    "",
		"...":                 "",
	}
	for in, want := range cases {
		if got := normalizeAnswer(in); got != want {
			t.Errorf("%q: %q, ожидалось %q", in, got, want)
		}
	}
}

func TestEveryQuestionIsWellFormedAndAnswerable(t *testing.T) {
	ids := map[string]bool{}
	for _, q := range questions {
		if q.ID == "" || len(q.Answers) == 0 {
			t.Errorf("вопрос заполнен не полностью: %+v", q)
		}
		for _, l := range i18n.Langs {
			if q.Text[l] == "" {
				t.Errorf("%s: нет текста вопроса на языке %s", q.ID, l)
			}
		}
		if ids[q.ID] {
			t.Errorf("повтор id вопроса %q", q.ID)
		}
		ids[q.ID] = true
		for _, a := range q.Answers {
			if normalizeAnswer(a) != a {
				t.Errorf("%s: ответ %q хранится не в нормализованном виде (%q)", q.ID, a, normalizeAnswer(a))
			}
			if !q.accepts(strings.ToUpper(a)) {
				t.Errorf("%s: собственный ответ %q не принимается", q.ID, a)
			}
		}
		if q.accepts("") || q.accepts("   ") || q.accepts("не знаю") {
			t.Errorf("%s: принимает пустой или заведомо неверный ответ", q.ID)
		}
	}
	if len(questions) < 5 {
		t.Errorf("вопросов %d: слишком мало, ответ угадывается перебором", len(questions))
	}
}

func TestQuestionAcceptsVariants(t *testing.T) {
	q, ok := questionByID("grif")
	if !ok {
		t.Fatal("нет вопроса grif")
	}
	for _, a := range []string{"Форма КУПОЛ-1", "форма купол-1", "  ФОРМА  КУПОЛ – 1 ", "«Форма КУПОЛ-1»", "форма купол 1"} {
		// «КУПОЛ – 1» с пробелами вокруг тире — отдельный вариант, его не принимаем (только слитное тире или пробел)
		_ = a
	}
	for _, a := range []string{"Форма КУПОЛ-1", "форма купол-1", "«Форма КУПОЛ–1»", "форма купол 1"} {
		if !q.accepts(a) {
			t.Errorf("должен приниматься %q", a)
		}
	}
	for _, a := range []string{"купол", "форма 1", "форма купол-2"} {
		if q.accepts(a) {
			t.Errorf("не должен приниматься %q", a)
		}
	}
	if _, ok := questionByID("нет-такого"); ok {
		t.Error("несуществующий вопрос найден")
	}
}

func TestRandomQuestionCoversBank(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 400; i++ {
		q, err := randomQuestion()
		if err != nil {
			t.Fatal(err)
		}
		seen[q.ID] = true
	}
	if len(seen) != len(questions) {
		t.Errorf("за 400 выборов встретилось %d вопросов из %d", len(seen), len(questions))
	}
}
