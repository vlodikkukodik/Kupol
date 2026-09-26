package accounts

import (
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

const (
	MinLoginLen    = 3
	MaxLoginLen    = 24
	MinPasswordLen = 8
	MaxPasswordLen = 128
	// maxLoginKeyLen ограничивает длину логина в ключах ограничителя (вводимое поле при входе не проверяется по формату).
	maxLoginKeyLen = 64
)

// reserved — логины, которые нельзя занять: названия званий, ролей и служебные слова.
// Сравнение без регистра и с заменой ё на е.
var reserved = map[string]struct{}{}

func init() {
	for _, w := range []string{
		"admin", "administrator", "root", "system", "support", "moderator", "null", "undefined", "me", "api",
		"kupol", "купол", "директорат", "directorate", "особый_совет", "особыйсовет", "совет", "комитет",
		"гражданин", "посетитель", "стажер", "сотрудник", "надзиратель", "куратор",
		"автор", "редактор", "модератор", "архивариус", "цак", "начальник", "начальник_купол",
	} {
		reserved[foldLogin(w)] = struct{}{}
	}
}

// foldLogin приводит логин к виду для сравнения: NFC, нижний регистр, ё -> е.
func foldLogin(s string) string {
	s = norm.NFC.String(s)
	s = strings.ToLower(s)
	return strings.ReplaceAll(s, "ё", "е")
}

// LoginKey — нормализованный логин для ключей ограничителя частоты.
func LoginKey(login string) string {
	k := foldLogin(strings.TrimSpace(login))
	if r := []rune(k); len(r) > maxLoginKeyLen {
		k = string(r[:maxLoginKeyLen])
	}
	return k
}

type script int

const (
	scriptNone script = iota
	scriptLatin
	scriptCyrillic
)

// NormalizeLogin проверяет логин и возвращает его в форме NFC (регистр сохраняется — так он и отображается).
// Правила: 3–24 символа; буквы латиницей или кириллицей, но не вперемешку (иначе можно подделать
// чужой ник, подменив «K» кириллической «К»); цифры 0–9; «_» и «-»; начинается с буквы или цифры.
func NormalizeLogin(input string) (string, error) {
	login := norm.NFC.String(input)

	n := utf8.RuneCountInString(login)
	if n < MinLoginLen || n > MaxLoginLen {
		return "", errors.New("Логин: от 3 до 24 символов")
	}

	var seen script
	for i, r := range login {
		var s script
		switch {
		case r >= '0' && r <= '9':
		case r == '_' || r == '-':
			if i == 0 {
				return "", errors.New("Логин должен начинаться с буквы или цифры")
			}
		case unicode.Is(unicode.Latin, r) && r < 0x80: // только базовая латиница: без диакритики и «похожих» символов
			s = scriptLatin
		case (r >= 'А' && r <= 'я') || r == 'Ё' || r == 'ё':
			s = scriptCyrillic
		default:
			return "", errors.New("Логин может содержать буквы (латиница или кириллица), цифры, «_» и «-»")
		}
		if s != scriptNone {
			if seen != scriptNone && seen != s {
				return "", errors.New("Логин: буквы только латиницей или только кириллицей, не вперемешку")
			}
			seen = s
		}
	}

	if _, isReserved := reserved[foldLogin(login)]; isReserved {
		return "", errors.New("Этот логин зарезервирован")
	}
	return login, nil
}

// ValidatePassword проверяет новый пароль (спецификация: не короче 8 символов).
func ValidatePassword(password, login string) error {
	n := utf8.RuneCountInString(password)
	if n < MinPasswordLen || n > MaxPasswordLen {
		return errors.New("Пароль: от 8 до 128 символов")
	}
	for _, r := range password {
		if unicode.IsControl(r) {
			return errors.New("Пароль не должен содержать управляющих символов")
		}
	}
	if login != "" && foldLogin(password) == foldLogin(login) {
		return errors.New("Пароль не должен совпадать с логином")
	}
	return nil
}

// preparePassword — NFKC-нормализация (рекомендация NIST SP 800-63B): один и тот же пароль,
// набранный на разных устройствах и раскладках (например, «й» как одна буква или как «и»+бревис),
// даёт один хеш.
func preparePassword(p string) string { return norm.NFKC.String(p) }
