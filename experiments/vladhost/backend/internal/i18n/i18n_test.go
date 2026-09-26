package i18n

import (
	"regexp"
	"testing"
	"unicode"
)

var verbRe = regexp.MustCompile(`%[-+# 0-9.]*[a-zA-Z]`)

func hasCyrillic(s string) bool {
	for _, r := range s {
		if unicode.Is(unicode.Cyrillic, r) {
			return true
		}
	}
	return false
}

// Оба каталога содержат одни и те же ключи с одинаковыми подстановками: забытый или лишний ключ,
// а также разный набор %s/%d в переводе (это ломает вывод) ловятся здесь.
func TestCatalogsAreInSync(t *testing.T) {
	for key, r := range ru {
		i, ok := it[key]
		if !ok {
			t.Errorf("ключа %q нет в итальянском каталоге", key)
			continue
		}
		if got, want := verbRe.FindAllString(i, -1), verbRe.FindAllString(r, -1); len(got) != len(want) {
			t.Errorf("%q: подстановки в it %v не совпадают с ru %v", key, got, want)
		} else {
			for n := range got {
				if got[n] != want[n] {
					t.Errorf("%q: подстановки в it %v не совпадают с ru %v", key, got, want)
				}
			}
		}
	}
	for key := range it {
		if _, ok := ru[key]; !ok {
			t.Errorf("ключа %q нет в русском каталоге", key)
		}
	}
}

func TestCatalogsAreClean(t *testing.T) {
	for key, v := range it {
		if hasCyrillic(v) {
			t.Errorf("итальянский текст %q содержит кириллицу: %q", key, v)
		}
		if v == "" {
			t.Errorf("пустой итальянский текст %q", key)
		}
	}
	for key, v := range ru {
		if v == "" {
			t.Errorf("пустой русский текст %q", key)
		}
		if !hasCyrillic(v) && key != "ftp.banner" {
			t.Errorf("русский текст %q не содержит кириллицы: %q (не забыт ли перевод?)", key, v)
		}
	}
}

func TestFromAcceptLanguage(t *testing.T) {
	for header, want := range map[string]Lang{
		"":                           RU, // нет заголовка — язык по умолчанию
		"ru":                         RU,
		"it":                         IT,
		"it-IT":                      IT,
		"IT-it":                      IT,
		"it-IT,it;q=0.9,en;q=0.8":    IT,
		"en-US,en;q=0.9":             RU, // неподдерживаемый язык — по умолчанию
		"en-US,it;q=0.8,ru;q=0.5":    IT, // берётся поддерживаемый с наибольшим весом
		"ru;q=0.4, it;q=0.9":         IT,
		"it;q=0, ru":                 RU, // q=0 — язык запрещён
		"de, fr;q=0.9":               RU,
		"  it  ;  q=0.7 , ru;q=0.6 ": IT,
		"it;q=abc, ru;q=0.5":         IT, // мусорный вес считается за 1
		"*":                          RU,
		"garbage;;;,,,":              RU,
		"ru-RU,ru;q=0.9,it;q=0.9":    RU, // равные веса — побеждает первый
	} {
		if got := FromAcceptLanguage(header); got != want {
			t.Errorf("Accept-Language %q → %q, ожидали %q", header, got, want)
		}
	}
}

func TestTAndBi(t *testing.T) {
	if got := T(IT, "err.invite_ttl", 720); got != "Durata dell'invito: da 1 a 720 ore" {
		t.Errorf("T(it): %q", got)
	}
	if got := T(RU, "err.invite_ttl", 720); got != "Срок инвайта: от 1 до 720 часов" {
		t.Errorf("T(ru): %q", got)
	}
	if got := T(IT, "нет.такого.ключа"); got != "нет.такого.ключа" {
		t.Errorf("неизвестный ключ должен возвращаться как есть: %q", got)
	}
	if got := Bi("err.bad_path"); got != "Недопустимый путь / Percorso non valido" {
		t.Errorf("Bi: %q", got)
	}
	if got := Bi("err.archive.bad_path", "a.txt"); got != "Недопустимый путь в архиве: a.txt / Percorso non valido nell'archivio: a.txt" {
		t.Errorf("Bi с аргументом: %q", got)
	}
	if !Has(IT, "err.internal") || Has(IT, "err.nope") {
		t.Error("Has")
	}
}
