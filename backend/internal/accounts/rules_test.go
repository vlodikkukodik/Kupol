package accounts

import (
	"strings"
	"testing"
)

// Невидимые и комбинирующие символы задаются кодами, а не литералами: так их видно в исходнике.
var (
	zeroWidthSpace   = string(rune(0x200B))
	combiningAcute   = string(rune(0x0301))
	combiningBreve   = string(rune(0x0306))
	fullwidthLatinAB = string([]rune{0xFF41, 0xFF42, 0xFF43}) // «ａｂｃ»
	arabicIndicDigit = string([]rune{0x0661, 0x0662, 0x0663}) // «١٢٣»
)

func TestNormalizeLoginAccepts(t *testing.T) {
	for _, ok := range []string{
		"abc", "Vlad", "user_1", "a-b-c", "x1y2", "9lives", "Иван", "Стажёр7", "мой-логин", "Ёж_2",
		strings.Repeat("a", 24), strings.Repeat("я", 24),
	} {
		got, err := NormalizeLogin(ok)
		if err != nil || got != ok {
			t.Errorf("%q: got=%q err=%v, ожидался успех без изменений", ok, got, err)
		}
	}
}

func TestNormalizeLoginRejects(t *testing.T) {
	cases := map[string]string{
		"":                          "от 3 до 24",
		"ab":                        "от 3 до 24",
		strings.Repeat("a", 25):     "от 3 до 24",
		strings.Repeat("я", 25):     "от 3 до 24",
		"_abc":                      "начинаться",
		"-abc":                      "начинаться",
		"ab c":                      "может содержать",
		"ab.c":                      "может содержать",
		"ab@c":                      "может содержать",
		"аbс":                       "вперемешку",      // кириллические а и с + латинская b
		"Kуратор7":                  "вперемешку",      // латинская K + кириллица
		"éclair":                    "может содержать", // диакритика
		fullwidthLatinAB:            "может содержать", // полноширинная латиница
		"日本語":                       "может содержать",
		"abc" + zeroWidthSpace:      "может содержать", // невидимый символ
		"a" + combiningAcute + "bc": "может содержать", // сочетаемый ударный знак после NFC остаётся отдельным
		"admin":                     "зарезервирован",
		"ADMIN":                     "зарезервирован",
		"Купол":                     "зарезервирован",
		"kupol":                     "зарезервирован",
		"Директорат":                "зарезервирован",
		"стажёр":                    "зарезервирован",
		"СТАЖЕР":                    "зарезервирован", // ё и е не различаются
		"Особый_Совет":              "зарезервирован",
		arabicIndicDigit:            "может содержать", // арабо-индийские цифры
	}
	for in, want := range cases {
		if _, err := NormalizeLogin(in); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: err=%v, ожидалась ошибка с %q", in, err, want)
		}
	}
}

func TestNormalizeLoginAppliesNFC(t *testing.T) {
	// «й» как «и» + сочетаемая краткая: после NFC — одна буква, длина и допустимость считаются по ней
	decomposed := "и" + combiningBreve + "ван"
	if decomposed == "йван" {
		t.Fatal("тест некорректен: строка уже в форме NFC")
	}
	got, err := NormalizeLogin(decomposed)
	if err != nil || got != "йван" {
		t.Fatalf("got=%q err=%v, ожидалось «йван» в NFC", got, err)
	}
}

func TestLoginKey(t *testing.T) {
	if LoginKey("  СтАжЁр ") != LoginKey("стажер") {
		t.Error("ключ не должен зависеть от регистра, пробелов и ё/е")
	}
	if got := len([]rune(LoginKey(strings.Repeat("я", 500)))); got != maxLoginKeyLen {
		t.Errorf("длина ключа %d, ожидалось %d", got, maxLoginKeyLen)
	}
}

func TestValidatePassword(t *testing.T) {
	for _, ok := range []string{"12345678", "пароль-пароль", strings.Repeat("x", 128), "с пробелами внутри", "Изделие К-19"} {
		if err := ValidatePassword(ok, "vlad"); err != nil {
			t.Errorf("%q: %v", ok, err)
		}
	}
	bad := map[string]string{
		"1234567":                "от 8 до 128",
		"":                       "от 8 до 128",
		strings.Repeat("x", 129): "от 8 до 128",
		"passwo\x00rd":           "управляющих",
		"pass\nword1":            "управляющих",
		"pass\tword1":            "управляющих",
	}
	for in, want := range bad {
		if err := ValidatePassword(in, "vlad"); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: err=%v, ожидалась ошибка с %q", in, err, want)
		}
	}
	if err := ValidatePassword("VladVlad", "vladvlad"); err == nil || !strings.Contains(err.Error(), "совпадать с логином") {
		t.Errorf("пароль = логин (без регистра) должен отвергаться: %v", err)
	}
	// длина считается в символах, а не в байтах
	if err := ValidatePassword("абвгдежз", ""); err != nil {
		t.Errorf("8 кириллических символов — допустимая длина: %v", err)
	}
	if err := ValidatePassword("абвгдеж", ""); err == nil {
		t.Error("7 кириллических символов — слишком коротко, хотя это 14 байт")
	}
}

func TestPreparePasswordNFKC(t *testing.T) {
	a := preparePassword("и" + combiningBreve + "пароль") // «й» разложенная
	b := preparePassword("йпароль")
	if a != b {
		t.Fatal("одинаковый по виду пароль в разных формах Unicode должен давать один хеш")
	}
	if preparePassword(fullwidthLatinAB+"１２３") != "abc123" {
		t.Error("NFKC должна сводить полноширинные символы к обычным")
	}
}

func TestLevelName(t *testing.T) {
	want := map[int]string{0: "Гражданин", 1: "Посетитель", 2: "Стажёр", 3: "Сотрудник", 4: "Надзиратель", 5: "Куратор", 6: "Особый Совет"}
	for lvl, name := range want {
		if got := LevelName(lvl, false); got != name {
			t.Errorf("уровень %d: %q, ожидалось %q", lvl, got, name)
		}
	}
	if LevelName(3, true) != "Директорат" {
		t.Error("Директорат показывается вместо звания уровня")
	}
	if LevelName(-1, false) != "" || LevelName(7, false) != "" {
		t.Error("неизвестный уровень — пустое звание")
	}
	if ValidLevel(0) || ValidLevel(7) || !ValidLevel(1) || !ValidLevel(6) {
		t.Error("ValidLevel: допустимы 1..6")
	}
}
