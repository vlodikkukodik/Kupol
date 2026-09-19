package documents

import "fmt"

// Данные блоков. Каждая структура — полный перечень полей блока; в ответ читателю попадают только они.

type headingData struct {
	Depth int    `json:"depth"`
	Text  string `json:"text"`
}

type paragraphData struct {
	Text Rich `json:"text"`
}

type listData struct {
	Ordered bool   `json:"ordered,omitempty"`
	Items   []Rich `json:"items"`
}

type quoteData struct {
	Text   Rich   `json:"text"`
	Source string `json:"source,omitempty"`
}

type dossierHeaderData struct{}

type logEntry struct {
	Date         string   `json:"date,omitempty"`
	Participants []string `json:"participants,omitempty"`
	Text         Rich     `json:"text"`
}

type experimentLogData struct {
	Title   string     `json:"title,omitempty"`
	Entries []logEntry `json:"entries"`
}

type stampData struct {
	Text string `json:"text"`
	Tone string `json:"tone,omitempty"`
	Tilt int    `json:"tilt,omitempty"`
}

type memoData struct {
	Kind      string   `json:"kind"`
	Number    string   `json:"number,omitempty"`
	Date      string   `json:"date,omitempty"`
	From      string   `json:"from,omitempty"`
	To        []string `json:"to,omitempty"`
	Subject   string   `json:"subject,omitempty"`
	Body      []Rich   `json:"body"`
	Signature string   `json:"signature,omitempty"`
}

type transcriptLine struct {
	Speaker string `json:"speaker,omitempty"`
	Text    Rich   `json:"text"`
}

type clippingData struct {
	Kind       string           `json:"kind"`
	Title      string           `json:"title,omitempty"`
	Source     string           `json:"source,omitempty"`
	Date       string           `json:"date,omitempty"`
	Paragraphs []Rich           `json:"paragraphs,omitempty"`
	Lines      []transcriptLine `json:"lines,omitempty"`
}

type tableData struct {
	Caption string   `json:"caption,omitempty"`
	Columns []string `json:"columns"`
	Rows    [][]Rich `json:"rows"`
}

type docLinkData struct {
	Code string `json:"code"`
	Note string `json:"note,omitempty"`
}

type dividerData struct {
	Style string `json:"style,omitempty"`
}

type pageData struct {
	Number string `json:"number,omitempty"`
}

type footnoteData struct {
	Mark string `json:"mark"`
	Text Rich   `json:"text"`
}

type appendixData struct {
	Number string `json:"number,omitempty"`
	Title  string `json:"title"`
}

// Ответы читателю: собираются явно, поле за полем.

type outHeading struct {
	Depth int    `json:"depth"`
	Text  string `json:"text"`
}

type outParagraph struct {
	Text []OutRun `json:"text"`
}

type outList struct {
	Ordered bool       `json:"ordered"`
	Items   [][]OutRun `json:"items"`
}

type outQuote struct {
	Text   []OutRun `json:"text"`
	Source string   `json:"source,omitempty"`
}

type outLogEntry struct {
	Date         string   `json:"date,omitempty"`
	Participants []string `json:"participants,omitempty"`
	Text         []OutRun `json:"text"`
}

type outExperimentLog struct {
	Title   string        `json:"title,omitempty"`
	Entries []outLogEntry `json:"entries"`
}

type outStamp struct {
	Text string `json:"text"`
	Tone string `json:"tone"`
	Tilt int    `json:"tilt"`
}

type outMemo struct {
	Kind      string     `json:"kind"`
	Number    string     `json:"number,omitempty"`
	Date      string     `json:"date,omitempty"`
	From      string     `json:"from,omitempty"`
	To        []string   `json:"to,omitempty"`
	Subject   string     `json:"subject,omitempty"`
	Body      [][]OutRun `json:"body"`
	Signature string     `json:"signature,omitempty"`
}

type outTranscriptLine struct {
	Speaker string   `json:"speaker,omitempty"`
	Text    []OutRun `json:"text"`
}

type outClipping struct {
	Kind       string              `json:"kind"`
	Title      string              `json:"title,omitempty"`
	Source     string              `json:"source,omitempty"`
	Date       string              `json:"date,omitempty"`
	Paragraphs [][]OutRun          `json:"paragraphs,omitempty"`
	Lines      []outTranscriptLine `json:"lines,omitempty"`
}

type outTable struct {
	Caption string       `json:"caption,omitempty"`
	Columns []string     `json:"columns"`
	Rows    [][][]OutRun `json:"rows"`
}

// outDocLink: для недоступной цели (нет такого документа или он закрыт от читателя) — только
// «недоступно»: ни шифр, ни название, ни примечание автора не раскрываются.
type outDocLink struct {
	Available bool   `json:"available"`
	Code      string `json:"code,omitempty"`
	Slug      string `json:"slug,omitempty"`
	Title     string `json:"title,omitempty"`
	Type      string `json:"type,omitempty"`
	TypeName  string `json:"type_name,omitempty"`
	Note      string `json:"note,omitempty"`
}

type outDivider struct {
	Style string `json:"style"`
}

type outPage struct {
	Number string `json:"number,omitempty"`
}

type outFootnote struct {
	Mark string   `json:"mark"`
	Text []OutRun `json:"text"`
}

type outAppendix struct {
	Number string `json:"number,omitempty"`
	Title  string `json:"title"`
}

func renderRichList(items []Rich, viewer int) [][]OutRun {
	out := make([][]OutRun, len(items))
	for i, it := range items {
		out[i] = it.render(viewer)
	}
	return out
}

// checkRichList проверяет список текстов: от min до max штук.
func checkRichList(items []Rich, p *Problems, path string, min, max int, cellsMayBeEmpty bool) {
	if len(items) < min {
		p.Add(path, "нужен хотя бы %d элемент(ов)", min)
	}
	if len(items) > max {
		p.Add(path, "слишком много элементов (%d, не больше %d)", len(items), max)
		return
	}
	for i, it := range items {
		ip := fmt.Sprintf("%s[%d]", path, i)
		if cellsMayBeEmpty && len(it) == 1 && it[0].Text == "" && it[0].Level == 0 {
			continue
		}
		it.check(p, ip, true)
	}
}

func init() {
	register("heading",
		func(d *headingData, p *Problems, path string) {
			if d.Depth == 0 {
				d.Depth = 2
			}
			if d.Depth < 1 || d.Depth > 3 {
				p.Add(path+".depth", "глубина заголовка — от 1 до 3")
			}
			p.text(path+".text", d.Text, 1, 300)
		},
		func(d *headingData, _ *renderer) any { return outHeading{Depth: d.Depth, Text: d.Text} })

	register("paragraph",
		func(d *paragraphData, p *Problems, path string) { d.Text.check(p, path+".text", true) },
		func(d *paragraphData, r *renderer) any { return outParagraph{Text: d.Text.render(r.viewer)} })

	register("list",
		func(d *listData, p *Problems, path string) { checkRichList(d.Items, p, path+".items", 1, 200, false) },
		func(d *listData, r *renderer) any {
			return outList{Ordered: d.Ordered, Items: renderRichList(d.Items, r.viewer)}
		})

	register("quote",
		func(d *quoteData, p *Problems, path string) {
			d.Text.check(p, path+".text", true)
			p.text(path+".source", d.Source, 0, 200)
		},
		func(d *quoteData, r *renderer) any { return outQuote{Text: d.Text.render(r.viewer), Source: d.Source} })

	register("dossier_header",
		func(*dossierHeaderData, *Problems, string) {},
		func(*dossierHeaderData, *renderer) any { return struct{}{} })

	register("experiment_log",
		func(d *experimentLogData, p *Problems, path string) {
			p.text(path+".title", d.Title, 0, 200)
			if len(d.Entries) == 0 || len(d.Entries) > 200 {
				p.Add(path+".entries", "записей должно быть от 1 до 200")
				return
			}
			for i, e := range d.Entries {
				ep := fmt.Sprintf("%s.entries[%d]", path, i)
				p.text(ep+".date", e.Date, 0, 40)
				if len(e.Participants) > 20 {
					p.Add(ep+".participants", "слишком много участников (%d, не больше 20)", len(e.Participants))
				}
				for j, name := range e.Participants {
					p.text(fmt.Sprintf("%s.participants[%d]", ep, j), name, 1, 100)
				}
				e.Text.check(p, ep+".text", true)
			}
		},
		func(d *experimentLogData, r *renderer) any {
			out := outExperimentLog{Title: d.Title, Entries: make([]outLogEntry, len(d.Entries))}
			for i, e := range d.Entries {
				out.Entries[i] = outLogEntry{Date: e.Date, Participants: e.Participants, Text: e.Text.render(r.viewer)}
			}
			return out
		})

	register("stamp",
		func(d *stampData, p *Problems, path string) {
			p.text(path+".text", d.Text, 1, 40)
			if d.Tone == "" {
				d.Tone = "red"
			}
			p.oneOf(path+".tone", d.Tone, "red", "ink")
			if d.Tilt < -15 || d.Tilt > 15 {
				p.Add(path+".tilt", "наклон — от -15 до 15 градусов")
			}
		},
		func(d *stampData, _ *renderer) any { return outStamp{Text: d.Text, Tone: d.Tone, Tilt: d.Tilt} })

	register("memo",
		func(d *memoData, p *Problems, path string) {
			p.oneOf(path+".kind", d.Kind, "memo", "order", "letter")
			p.text(path+".number", d.Number, 0, 40)
			p.text(path+".date", d.Date, 0, 40)
			p.text(path+".from", d.From, 0, 200)
			if len(d.To) > 20 {
				p.Add(path+".to", "слишком много адресатов (%d, не больше 20)", len(d.To))
			}
			for i, to := range d.To {
				p.text(fmt.Sprintf("%s.to[%d]", path, i), to, 1, 200)
			}
			p.text(path+".subject", d.Subject, 0, 300)
			checkRichList(d.Body, p, path+".body", 1, 50, false)
			p.text(path+".signature", d.Signature, 0, 200)
		},
		func(d *memoData, r *renderer) any {
			return outMemo{
				Kind: d.Kind, Number: d.Number, Date: d.Date, From: d.From, To: d.To, Subject: d.Subject,
				Body: renderRichList(d.Body, r.viewer), Signature: d.Signature,
			}
		})

	register("clipping",
		func(d *clippingData, p *Problems, path string) {
			p.oneOf(path+".kind", d.Kind, "newspaper", "handwritten", "transcript")
			p.text(path+".title", d.Title, 0, 300)
			p.text(path+".source", d.Source, 0, 200)
			p.text(path+".date", d.Date, 0, 40)
			if d.Kind == "transcript" {
				if len(d.Paragraphs) > 0 {
					p.Add(path+".paragraphs", "у расшифровки записи вместо абзацев — реплики (lines)")
				}
				if len(d.Lines) == 0 || len(d.Lines) > 500 {
					p.Add(path+".lines", "реплик должно быть от 1 до 500")
				}
				for i, l := range d.Lines {
					lp := fmt.Sprintf("%s.lines[%d]", path, i)
					p.text(lp+".speaker", l.Speaker, 0, 100)
					l.Text.check(p, lp+".text", true)
				}
				return
			}
			if len(d.Lines) > 0 {
				p.Add(path+".lines", "реплики (lines) бывают только у расшифровки записи (kind: transcript)")
			}
			checkRichList(d.Paragraphs, p, path+".paragraphs", 1, 100, false)
		},
		func(d *clippingData, r *renderer) any {
			out := outClipping{Kind: d.Kind, Title: d.Title, Source: d.Source, Date: d.Date}
			if d.Kind == "transcript" {
				out.Lines = make([]outTranscriptLine, len(d.Lines))
				for i, l := range d.Lines {
					out.Lines[i] = outTranscriptLine{Speaker: l.Speaker, Text: l.Text.render(r.viewer)}
				}
			} else {
				out.Paragraphs = renderRichList(d.Paragraphs, r.viewer)
			}
			return out
		})

	register("table",
		func(d *tableData, p *Problems, path string) {
			p.text(path+".caption", d.Caption, 0, 300)
			if len(d.Columns) == 0 || len(d.Columns) > 12 {
				p.Add(path+".columns", "столбцов должно быть от 1 до 12")
				return
			}
			for i, c := range d.Columns {
				p.text(fmt.Sprintf("%s.columns[%d]", path, i), c, 1, 100)
			}
			if len(d.Rows) == 0 || len(d.Rows) > 200 {
				p.Add(path+".rows", "строк должно быть от 1 до 200")
				return
			}
			for i, row := range d.Rows {
				rp := fmt.Sprintf("%s.rows[%d]", path, i)
				if len(row) != len(d.Columns) {
					p.Add(rp, "в строке %d ячеек, а столбцов %d", len(row), len(d.Columns))
					continue
				}
				checkRichList(row, p, rp, len(d.Columns), len(d.Columns), true)
			}
		},
		func(d *tableData, r *renderer) any {
			out := outTable{Caption: d.Caption, Columns: d.Columns, Rows: make([][][]OutRun, len(d.Rows))}
			for i, row := range d.Rows {
				out.Rows[i] = renderRichList(row, r.viewer)
			}
			return out
		})

	kinds["doc_link"] = docLinkSpec()

	register("divider",
		func(d *dividerData, p *Problems, path string) {
			if d.Style == "" {
				d.Style = "line"
			}
			p.oneOf(path+".style", d.Style, "line", "stars")
		},
		func(d *dividerData, _ *renderer) any { return outDivider{Style: d.Style} })

	register("page",
		func(d *pageData, p *Problems, path string) { p.text(path+".number", d.Number, 0, 20) },
		func(d *pageData, _ *renderer) any { return outPage{Number: d.Number} })

	register("footnote",
		func(d *footnoteData, p *Problems, path string) {
			p.text(path+".mark", d.Mark, 1, 8)
			d.Text.check(p, path+".text", true)
		},
		func(d *footnoteData, r *renderer) any {
			return outFootnote{Mark: d.Mark, Text: d.Text.render(r.viewer)}
		})

	register("appendix",
		func(d *appendixData, p *Problems, path string) {
			p.text(path+".number", d.Number, 0, 20)
			p.text(path+".title", d.Title, 1, 200)
		},
		func(d *appendixData, _ *renderer) any { return outAppendix{Number: d.Number, Title: d.Title} })
}

// docLinkSpec — блок-ссылка на другой документ: разрешается при чтении с учётом допуска читателя.
func docLinkSpec() kindSpec {
	spec := newSpec(
		func(d *docLinkData, p *Problems, path string) {
			c, err := ParseCode(d.Code)
			if err != nil {
				p.Add(path+".code", "%q — не шифр документа (ожидается, например, О-041 или ПРИКАЗ-1978-12)", d.Code)
			} else {
				d.Code = c.Canonical
			}
			p.text(path+".note", d.Note, 0, 300)
		},
		func(d *docLinkData, r *renderer) any {
			t := (*LinkTarget)(nil)
			if r.resolve != nil {
				t = r.resolve(d.Code)
			}
			if t == nil {
				return outDocLink{Available: false}
			}
			return outDocLink{
				Available: true, Code: t.Code, Slug: t.Slug, Title: t.Title,
				Type: string(t.Type), TypeName: t.Type.Name(), Note: d.Note,
			}
		})
	spec.links = func(v any) []string { return []string{v.(*docLinkData).Code} }
	return spec
}
