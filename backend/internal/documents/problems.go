package documents

import (
	"fmt"
	"strings"
)

// Problem — замечание к загружаемому документу; Path указывает место в JSON: «blocks[3].data.text».
type Problem struct {
	Path    string `json:"path"`
	Message string `json:"message"`
}

func (p Problem) String() string { return p.Path + ": " + p.Message }

// Problems собирает замечания, чтобы автор видел все ошибки файла сразу, а не по одной.
type Problems struct {
	list []Problem
}

// Add добавляет замечание.
func (p *Problems) Add(path, format string, args ...any) {
	p.list = append(p.list, Problem{Path: path, Message: fmt.Sprintf(format, args...)})
}

// Any — есть ли замечания.
func (p *Problems) Any() bool { return len(p.list) > 0 }

// List — накопленные замечания.
func (p *Problems) List() []Problem { return p.list }

// ValidationError — документ не прошёл проверку.
type ValidationError struct {
	Problems []Problem
}

func (e *ValidationError) Error() string {
	lines := make([]string, len(e.Problems))
	for i, p := range e.Problems {
		lines[i] = "  " + p.String()
	}
	return fmt.Sprintf("документ не прошёл проверку (%d):\n%s", len(e.Problems), strings.Join(lines, "\n"))
}

// text проверяет длину строки в символах и возвращает её без изменений.
func (p *Problems) text(path, s string, minLen, maxLen int) {
	n := len([]rune(s))
	switch {
	case n < minLen && minLen == 1:
		p.Add(path, "не может быть пустым")
	case n < minLen:
		p.Add(path, "слишком короткое значение (%d символов, нужно не меньше %d)", n, minLen)
	case n > maxLen:
		p.Add(path, "слишком длинное значение (%d символов, не больше %d)", n, maxLen)
	}
	for _, r := range s {
		if r == 0 || (r < 0x20 && r != '\n' && r != '\t') {
			p.Add(path, "содержит управляющие символы")
			return
		}
	}
}

// oneOf проверяет значение по списку допустимых.
func (p *Problems) oneOf(path, v string, allowed ...string) {
	for _, a := range allowed {
		if v == a {
			return
		}
	}
	p.Add(path, "недопустимое значение %q, допустимы: %s", v, strings.Join(allowed, ", "))
}
