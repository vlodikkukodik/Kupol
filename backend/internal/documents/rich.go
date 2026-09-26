package documents

import (
	"bytes"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
)

// Run — фрагмент текста. Level > 0 закрывает фрагмент от читателей с меньшим допуском
// («зачернённый текст»); Bold и Italic — простое выделение.
type Run struct {
	Text   string `json:"text"`
	Level  int    `json:"level,omitempty"`
	Bold   bool   `json:"bold,omitempty"`
	Italic bool   `json:"italic,omitempty"`
}

// Rich — текст из фрагментов. В файле можно писать просто строкой ("Текст") — она станет одним фрагментом.
type Rich []Run

const (
	maxRichRuns = 200
	maxRichText = 20000 // символов в одном тексте
)

// UnmarshalJSON принимает строку или массив фрагментов; неизвестные поля во фрагменте — ошибка.
func (r *Rich) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) == 0 {
		return errors.New("пустое значение")
	}
	switch b[0] {
	case '"':
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		*r = Rich{{Text: s}}
		return nil
	case '[':
		var runs []Run
		dec := json.NewDecoder(bytes.NewReader(b))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&runs); err != nil {
			return err
		}
		*r = runs
		return nil
	default:
		return errors.New("ожидается строка или массив фрагментов {\"text\": ...}")
	}
}

// plain — текст без разметки и уровней (для проверок длины).
func (r Rich) plain() string {
	var b strings.Builder
	for _, run := range r {
		b.WriteString(run.Text)
	}
	return b.String()
}

// check проверяет текст. required — хотя бы один непустой фрагмент.
func (r Rich) check(p *Problems, path string, required bool) {
	if len(r) > maxRichRuns {
		p.Add(path, "слишком много фрагментов (%d, не больше %d)", len(r), maxRichRuns)
		return
	}
	total := 0
	for i, run := range r {
		// Строка вместо массива ("text": "...") — это одно поле; замечание к нему без лишнего «.text».
		rp, tp := path, path
		if len(r) > 1 || run.Level != 0 || run.Bold || run.Italic {
			rp = path + "[" + itoa(i) + "]"
			tp = rp + ".text"
		}
		if run.Text == "" && (len(r) > 1 || !required) {
			p.Add(tp, "не может быть пустым")
		}
		if run.Level < 0 || run.Level > MaxLevel {
			p.Add(rp+".level", "уровень должен быть от 0 до %d", MaxLevel)
		}
		total += len([]rune(run.Text))
		for _, ch := range run.Text {
			if ch == 0 || (ch < 0x20 && ch != '\n' && ch != '\t') {
				p.Add(tp, "содержит управляющие символы")
				break
			}
		}
	}
	if total > maxRichText {
		p.Add(path, "слишком длинный текст (%d символов, не больше %d)", total, maxRichText)
	}
	if required && strings.TrimSpace(r.plain()) == "" {
		p.Add(path, "не может быть пустым")
	}
}

// MarshalJSON пишет канонический вид — всегда массив фрагментов.
func (r Rich) MarshalJSON() ([]byte, error) {
	if r == nil {
		return []byte("[]"), nil
	}
	return json.Marshal([]Run(r))
}

// OutRun — фрагмент в ответе читателю. Закрытый фрагмент не несёт ни текста, ни длины:
// только признак «закрыто» и нужный уровень.
type OutRun struct {
	Text     string `json:"text,omitempty"`
	Bold     bool   `json:"bold,omitempty"`
	Italic   bool   `json:"italic,omitempty"`
	Redacted bool   `json:"redacted,omitempty"`
	Level    int    `json:"level,omitempty"`
}

// render собирает ответ заново из разрешённых полей. Фрагменты выше допуска читателя
// заменяются меткой; соседние закрытые фрагменты одного уровня сливаются в одну.
func (r Rich) render(viewer int) []OutRun {
	out := make([]OutRun, 0, len(r))
	for _, run := range r {
		if run.Level > viewer {
			if n := len(out); n > 0 && out[n-1].Redacted && out[n-1].Level == run.Level {
				continue
			}
			out = append(out, OutRun{Redacted: true, Level: run.Level})
			continue
		}
		if run.Text == "" { // пустая ячейка таблицы и т.п.: пустой фрагмент клиенту не нужен
			continue
		}
		out = append(out, OutRun{Text: run.Text, Bold: run.Bold, Italic: run.Italic})
	}
	return out
}

func itoa(n int) string { return strconv.Itoa(n) }
