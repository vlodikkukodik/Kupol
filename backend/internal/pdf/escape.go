package pdf

import (
	"strings"
)

// Экранирование текста в разметке Typst.
//
// Текст документа — данные, а не разметка: знаки Typst («#», «*», «[»…) обязаны дойти до страницы буквами.
// Экранируем обратным слешём всё, что значимо в разметке, а в начале строки — ещё и то, что запускает
// блочные конструкции (заголовок, список, комментарий): текст фрагмента может содержать переводы строк.
//
// Перевод строки внутри текста становится переводом строки Typst («\» в конце строки), пустая строка —
// новым абзацем. Проверяется пыткой в TestRenderAdversarialText: тест компилирует текст со всеми знаками.

// alwaysSpecial — знаки, значимые в любом положении (и в конце строки: «*» закрывает выделение).
const alwaysSpecial = "\\/*_`$[]<>@~#"

// lineSpecial — знаки блочных конструкций: только в начале строки (после пробелов).
const lineSpecial = "=+-/"

// escape — строка s в разметке Typst.
func escape(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 8)
	atLineStart := true
	for i := 0; i < len(s); i++ {
		r := rune(s[i])
		if r >= 0x80 { // многобайтовые — как есть, по байтам не режем
			j := i
			for j < len(s) && s[j] >= 0x80 {
				j++
			}
			b.WriteString(s[i:j])
			atLineStart = false
			i = j - 1
			continue
		}
		switch {
		case r == '\r':
			// управляющие символы документ не принимает; просто пропускаем
		case r == '\n':
			j := i
			for j < len(s) && s[j] == '\n' {
				j++
			}
			if j-i == 1 {
				b.WriteString("\\\n") // перевод строки Typst
			} else {
				b.WriteString(s[i:j]) // пустая строка — новый абзац
			}
			atLineStart = true
			i = j - 1
		case strings.ContainsRune(alwaysSpecial, r):
			b.WriteByte('\\')
			b.WriteByte(byte(r))
			atLineStart = false
		case atLineStart && strings.ContainsRune(lineSpecial, r):
			b.WriteByte('\\')
			b.WriteByte(byte(r))
			atLineStart = false
		default:
			if r != ' ' && r != '\t' {
				atLineStart = false
			}
			b.WriteByte(byte(r))
		}
	}
	return b.String()
}

// escapeString — литерал строки Typst (для аргументов вида #image("…"), #link("…")): кавычки и слеши.
func escapeString(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 4)
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' || s[i] == '"' {
			b.WriteByte('\\')
		}
		b.WriteByte(s[i])
	}
	return b.String()
}
