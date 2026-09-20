package documents

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Экспорт и импорт документа в интерфейсе команды (этап 3.6).
//
// Экспорт отдаёт то, что видит сам автор: полное содержимое, без фильтрации по допуску читателя (как и редактор).
// JSON — тот же формат, что принимает загрузка (docs/documents.md): экспорт → правка → импорт — рабочий круг.
// Markdown — для чтения и правки текста вне архива; обратно не загружается (уровни допуска в нём — пометки).
//
// Импорт из интерфейса всегда заводит ЧЕРНОВИК за тем, кто загрузил: статус из файла не берётся, документ идёт обычным
// путём — правка, проверка, рецензия. Так загрузка не обходит рецензию, в отличие от команды `kupol doc import`.

// ExportFormat — вид файла экспорта.
type ExportFormat string

const (
	ExportJSON     ExportFormat = "json"
	ExportMarkdown ExportFormat = "md"
)

// ExportFile — готовый файл для скачивания.
type ExportFile struct {
	Filename    string
	ContentType string
	Data        []byte
}

// TeamExport выгружает документ в файл. Доступ — как к чтению документа в панели команды.
func (s *Service) TeamExport(ctx context.Context, a Actor, id int64, format ExportFormat) (*ExportFile, error) {
	if format != ExportJSON && format != ExportMarkdown {
		return nil, &QueryError{Field: "format", Message: "json или md"}
	}
	d, err := s.viewable(ctx, s.db.WithContext(ctx), a, id)
	if err != nil {
		return nil, err
	}
	in := inputFromDocument(d)
	name := exportName(d)
	switch format {
	case ExportJSON:
		data, err := json.MarshalIndent(in, "", "  ")
		if err != nil {
			return nil, err
		}
		return &ExportFile{Filename: name + ".json", ContentType: "application/json; charset=utf-8", Data: append(data, '\n')}, nil
	default:
		return &ExportFile{Filename: name + ".md", ContentType: "text/markdown; charset=utf-8", Data: []byte(renderMarkdown(in))}, nil
	}
}

// exportName — имя файла без расширения: адрес документа (латиницей) либо номер, если шифра ещё нет.
func exportName(d *Document) string {
	if d.Slug != nil && *d.Slug != "" {
		return *d.Slug
	}
	return fmt.Sprintf("%s-%d", d.Type, d.ID)
}

// ImportResult — итог загрузки файла в интерфейсе.
type ImportResult struct {
	// DryRun — файл только проверен, ничего не создано.
	DryRun bool   `json:"dry_run"`
	Type   string `json:"type"`
	// TypeName — тип по-русски.
	TypeName string `json:"type_name"`
	Code     string `json:"code,omitempty"`
	Title    string `json:"title"`
	Blocks   int    `json:"blocks"`
	// StatusIgnored — статус из файла отброшен: документ всегда создаётся черновиком.
	StatusIgnored string `json:"status_ignored,omitempty"`
	// Document — созданный черновик; при проверке (DryRun) его нет.
	Document *TeamDocument `json:"document,omitempty"`
}

// TeamImport заводит черновик из файла формата загрузки (один документ). При dryRun только проверяет файл: те же замечания
// с путями, что и при настоящей загрузке, но ничего не создаёт.
func (s *Service) TeamImport(ctx context.Context, a Actor, raw []byte, dryRun bool) (*ImportResult, error) {
	if !a.CanWrite && !a.Directorate {
		return nil, ErrForbidden
	}
	inputs, batch, err := ParseInputs(raw)
	if err != nil {
		var ve *ValidationError
		if errors.As(err, &ve) {
			return nil, err
		}
		return nil, &ValidationError{Problems: []Problem{{Path: "$", Message: err.Error()}}}
	}
	if batch || len(inputs) != 1 {
		return nil, &ValidationError{Problems: []Problem{{Path: "$", Message: "в файле пакет документов: загружайте по одному документу"}}}
	}
	in := inputs[0]

	res := &ImportResult{DryRun: dryRun, Type: in.Type, Title: strings.TrimSpace(in.Title), Blocks: len(in.Blocks)}
	if in.Status != "" && in.Status != string(StatusDraft) {
		res.StatusIgnored = in.Status
	}
	// Объект из файла без шифра допустим: номер О-№ присвоится при публикации. Как и у черновика из редактора.
	content := contentFromInput(in)
	var probs Problems
	check := content.input(in.Code, in.Type, string(StatusDraft))
	pr := check.prepare("$", &probs)
	if probs.Any() {
		return nil, &ValidationError{Problems: probs.List()}
	}
	res.TypeName = Type(pr.Doc.Type).Name()
	if pr.Code != nil {
		res.Code = pr.Code.Canonical
	}

	if dryRun {
		if pr.Code != nil {
			var taken int64
			if err := s.db.WithContext(ctx).Raw("SELECT count(*) FROM documents WHERE code = ?", pr.Code.Canonical).Scan(&taken).Error; err != nil {
				return nil, err
			}
			if taken > 0 {
				return nil, ErrCodeTaken
			}
		}
		return res, nil
	}

	d, err := s.TeamCreate(ctx, a, CreateInput{Type: in.Type, Code: in.Code, Content: content})
	if err != nil {
		return nil, err
	}
	res.Document = d
	return res, nil
}

// ——— Markdown ———

// renderMarkdown собирает читаемый файл: сведения о документе в заголовке, затем блоки по порядку.
func renderMarkdown(in Input) string {
	var b strings.Builder
	b.WriteString("---\n")
	meta := func(key, val string) {
		if val != "" {
			b.WriteString(key + ": " + strconv.Quote(val) + "\n")
		}
	}
	meta("code", in.Code)
	meta("type", in.Type)
	meta("status", in.Status)
	if in.Level != nil {
		fmt.Fprintf(&b, "level: %d\n", *in.Level)
	}
	meta("grif", in.Grif)
	if in.Composed != nil {
		date := fmt.Sprintf("%04d", in.Composed.Year)
		if in.Composed.Month != nil {
			date += fmt.Sprintf("-%02d", *in.Composed.Month)
			if in.Composed.Day != nil {
				date += fmt.Sprintf("-%02d", *in.Composed.Day)
			}
		}
		meta("composed", date)
	}
	if p := in.Props; p != nil {
		if p.DangerClass != nil {
			fmt.Fprintf(&b, "danger_class: %d\n", *p.DangerClass)
		}
		if p.DeviationPoints != nil {
			fmt.Fprintf(&b, "deviation_points: %d\n", *p.DeviationPoints)
		}
		meta("department", p.Department)
		meta("category", p.Category)
		meta("containment_status", p.ContainmentStatus)
		meta("discovery_place", p.DiscoveryPlace)
	}
	b.WriteString("---\n\n")
	b.WriteString("# " + mdEscape(in.Title) + "\n")

	for _, blk := range in.Blocks {
		b.WriteString("\n")
		if blk.Level != nil && *blk.Level > 0 {
			fmt.Fprintf(&b, "<!-- допуск блока: %d -->\n", *blk.Level)
		}
		b.WriteString(renderBlockMarkdown(blk))
		b.WriteString("\n")
	}
	return b.String()
}

// decodeData разбирает данные блока терпимо: экспорт не должен падать на том, что редактор уже принял.
func decodeData[T any](raw json.RawMessage) T {
	var v T
	_ = json.NewDecoder(bytes.NewReader(raw)).Decode(&v)
	return v
}

func renderBlockMarkdown(blk InputBlock) string {
	switch blk.Type {
	case "heading":
		d := decodeData[headingData](blk.Data)
		depth := d.Depth
		if depth < 1 || depth > 3 {
			depth = 1
		}
		return strings.Repeat("#", depth+1) + " " + mdEscape(d.Text)
	case "paragraph":
		return mdRich(decodeData[paragraphData](blk.Data).Text)
	case "list":
		d := decodeData[listData](blk.Data)
		lines := make([]string, len(d.Items))
		for i, it := range d.Items {
			marker := "-"
			if d.Ordered {
				marker = strconv.Itoa(i+1) + "."
			}
			lines[i] = marker + " " + strings.ReplaceAll(mdRich(it), "  \n", " ")
		}
		return strings.Join(lines, "\n")
	case "quote":
		d := decodeData[quoteData](blk.Data)
		out := mdQuote(mdRich(d.Text))
		if d.Source != "" {
			out += "\n>\n> — " + mdEscape(d.Source)
		}
		return out
	case "dossier_header":
		return "<!-- шапка досье: строится из сведений в заголовке файла -->"
	case "experiment_log":
		d := decodeData[experimentLogData](blk.Data)
		var parts []string
		if d.Title != "" {
			parts = append(parts, "### "+mdEscape(d.Title))
		}
		for _, e := range d.Entries {
			head := ""
			if e.Date != "" {
				head = "**" + mdEscape(e.Date) + "**"
			}
			if len(e.Participants) > 0 {
				names := make([]string, len(e.Participants))
				for i, p := range e.Participants {
					names[i] = mdEscape(p)
				}
				if head != "" {
					head += " — "
				}
				head += strings.Join(names, ", ")
			}
			if head != "" {
				head += "\n\n"
			}
			parts = append(parts, head+mdRich(e.Text))
		}
		return strings.Join(parts, "\n\n")
	case "stamp":
		d := decodeData[stampData](blk.Data)
		return "**[" + mdEscape(d.Text) + "]**"
	case "memo":
		d := decodeData[memoData](blk.Data)
		title := map[string]string{"memo": "Меморандум", "order": "Приказ", "letter": "Письмо"}[d.Kind]
		if title == "" {
			title = "Документ"
		}
		if d.Number != "" {
			title += " № " + d.Number
		}
		lines := []string{"**" + mdEscape(title) + "**"}
		add := func(label, val string) {
			if val != "" {
				lines = append(lines, label+": "+mdEscape(val))
			}
		}
		add("Дата", d.Date)
		add("От", d.From)
		if len(d.To) > 0 {
			add("Кому", strings.Join(d.To, ", "))
		}
		add("Тема", d.Subject)
		out := mdQuote(strings.Join(lines, "  \n"))
		for _, p := range d.Body {
			out += "\n>\n" + mdQuote(mdRich(p))
		}
		if d.Signature != "" {
			out += "\n>\n> " + mdEscape(d.Signature)
		}
		return out
	case "clipping":
		d := decodeData[clippingData](blk.Data)
		kind := map[string]string{"newspaper": "Газетная вырезка", "handwritten": "Рукописная запись", "transcript": "Расшифровка записи"}[d.Kind]
		head := "**" + kind + "**"
		if d.Title != "" {
			head += " «" + mdEscape(d.Title) + "»"
		}
		if d.Source != "" {
			head += ", " + mdEscape(d.Source)
		}
		if d.Date != "" {
			head += ", " + mdEscape(d.Date)
		}
		out := mdQuote(head)
		if d.Kind == "transcript" {
			for _, l := range d.Lines {
				line := mdRich(l.Text)
				if l.Speaker != "" {
					line = "**" + mdEscape(l.Speaker) + ":** " + line
				}
				out += "\n>\n" + mdQuote(line)
			}
		} else {
			for _, p := range d.Paragraphs {
				out += "\n>\n" + mdQuote(mdRich(p))
			}
		}
		return out
	case "table":
		d := decodeData[tableData](blk.Data)
		var lines []string
		if d.Caption != "" {
			lines = append(lines, "*"+mdEscape(d.Caption)+"*", "")
		}
		cell := func(s string) string { return strings.ReplaceAll(strings.ReplaceAll(s, "|", `\|`), "  \n", " ") }
		head := make([]string, len(d.Columns))
		rule := make([]string, len(d.Columns))
		for i, c := range d.Columns {
			head[i] = cell(mdEscape(c))
			rule[i] = "---"
		}
		lines = append(lines, "| "+strings.Join(head, " | ")+" |", "| "+strings.Join(rule, " | ")+" |")
		for _, row := range d.Rows {
			cells := make([]string, len(row))
			for i, c := range row {
				cells[i] = cell(mdRich(c))
			}
			lines = append(lines, "| "+strings.Join(cells, " | ")+" |")
		}
		return strings.Join(lines, "\n")
	case "doc_link":
		d := decodeData[docLinkData](blk.Data)
		out := "→ " + mdEscape(d.Code)
		if d.Note != "" {
			out += " — " + mdEscape(d.Note)
		}
		return out
	case "divider":
		if decodeData[dividerData](blk.Data).Style == "stars" {
			return "\\* \\* \\*"
		}
		return "---"
	case "page":
		if n := decodeData[pageData](blk.Data).Number; n != "" {
			return "*— страница " + mdEscape(n) + " —*"
		}
		return "*— новая страница —*"
	case "footnote":
		d := decodeData[footnoteData](blk.Data)
		return "[^" + strings.NewReplacer("]", "", "[", "", " ", "-").Replace(d.Mark) + "]: " + mdRich(d.Text)
	case "appendix":
		d := decodeData[appendixData](blk.Data)
		title := "Приложение"
		if d.Number != "" {
			title += " " + d.Number
		}
		return "## " + mdEscape(title+". "+d.Title)
	default:
		// Неизвестный тип (появится в редакторе позже) не теряется молча.
		return "<!-- блок «" + strings.ReplaceAll(blk.Type, "-->", "") + "»: в Markdown не переносится, см. JSON -->"
	}
}

// mdRich — форматированный текст: жирный и курсив — как в Markdown, закрытый фрагмент — [текст]{допуск=N}.
func mdRich(r Rich) string {
	var b strings.Builder
	for _, run := range r {
		text := mdEscape(run.Text)
		text = strings.ReplaceAll(text, "\n", "  \n")
		switch {
		case run.Bold && run.Italic:
			text = "***" + text + "***"
		case run.Bold:
			text = "**" + text + "**"
		case run.Italic:
			text = "*" + text + "*"
		}
		if run.Level > 0 {
			text = "[" + text + "]{допуск=" + strconv.Itoa(run.Level) + "}"
		}
		b.WriteString(text)
	}
	return b.String()
}

// mdQuote ставит «> » перед каждой строкой.
func mdQuote(s string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight("> "+l, " ")
	}
	return strings.Join(lines, "\n")
}

var mdEscaper = strings.NewReplacer(`\`, `\\`, "`", "\\`", "*", `\*`, "_", `\_`, "[", `\[`, "]", `\]`, "<", `\<`, ">", `\>`)

// mdEscape прячет знаки разметки: текст документа не должен превращаться в форматирование сам.
func mdEscape(s string) string {
	s = mdEscaper.Replace(s)
	// «#», «-», «+» и «1.» в начале строки — заголовок, список, нумерованный список
	if s != "" && strings.ContainsRune("#-+", rune(s[0])) {
		s = `\` + s
	}
	return s
}
