package documents

import (
	"fmt"
	"regexp"
)

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

// Восемь видов, по одному на тип дела (не привязаны к типу принудительно — как и остальные блоки).

type containmentProcedureData struct {
	Title string `json:"title,omitempty"`
	Steps []Rich `json:"steps"`
}

type directiveData struct {
	Recipient string `json:"recipient"`
	Order     Rich   `json:"order"`
	Deadline  string `json:"deadline,omitempty"`
}

type timelineEntry struct {
	Time  string `json:"time,omitempty"`
	Event Rich   `json:"event"`
}

type incidentTimelineData struct {
	Entries []timelineEntry `json:"entries"`
}

type personnelRecordData struct {
	Rank      string `json:"rank,omitempty"`
	Clearance string `json:"clearance,omitempty"`
	Status    string `json:"status,omitempty"`
}

type rosterEntry struct {
	Name     string `json:"name"`
	Position string `json:"position,omitempty"`
}

type rosterData struct {
	Entries []rosterEntry `json:"entries"`
}

type hypothesisData struct {
	Text      Rich  `json:"text"`
	Result    Rich  `json:"result"`
	Confirmed *bool `json:"confirmed,omitempty"`
}

type qaEntry struct {
	Question Rich `json:"question"`
	Answer   Rich `json:"answer"`
}

type qaData struct {
	Entries []qaEntry `json:"entries"`
}

type routingEntry struct {
	Who      string `json:"who"`
	Decision string `json:"decision"`
}

// uploadKeyRe — ключ загрузки (internal/uploads): 16 случайных байт в hex.
var uploadKeyRe = regexp.MustCompile(`^[0-9a-f]{32}$`)

type imageData struct {
	Upload  string `json:"upload"`
	Caption string `json:"caption,omitempty"`
	Sticker string `json:"sticker,omitempty"`
}

type audioData struct {
	Upload     string `json:"upload"`
	Title      string `json:"title,omitempty"`
	Transcript Rich   `json:"transcript,omitempty"`
}

type routingData struct {
	Entries []routingEntry `json:"entries"`
}

// Ответы читателю: собираются явно, поле за полем. Типы экспортируются, чтобы tygo включил их в контракт
// фронтенда: OutBlock.Data — одна из этих форм в зависимости от Type.

type OutHeading struct {
	Depth int    `json:"depth"`
	Text  string `json:"text"`
}

type OutParagraph struct {
	Text []OutRun `json:"text"`
}

type OutList struct {
	Ordered bool       `json:"ordered"`
	Items   [][]OutRun `json:"items"`
}

type OutQuote struct {
	Text   []OutRun `json:"text"`
	Source string   `json:"source,omitempty"`
}

type OutLogEntry struct {
	Date         string   `json:"date,omitempty"`
	Participants []string `json:"participants,omitempty"`
	Text         []OutRun `json:"text"`
}

type OutExperimentLog struct {
	Title   string        `json:"title,omitempty"`
	Entries []OutLogEntry `json:"entries"`
}

type OutStamp struct {
	Text string `json:"text"`
	Tone string `json:"tone"`
	Tilt int    `json:"tilt"`
}

type OutMemo struct {
	Kind      string     `json:"kind"`
	Number    string     `json:"number,omitempty"`
	Date      string     `json:"date,omitempty"`
	From      string     `json:"from,omitempty"`
	To        []string   `json:"to,omitempty"`
	Subject   string     `json:"subject,omitempty"`
	Body      [][]OutRun `json:"body"`
	Signature string     `json:"signature,omitempty"`
}

type OutTranscriptLine struct {
	Speaker string   `json:"speaker,omitempty"`
	Text    []OutRun `json:"text"`
}

type OutClipping struct {
	Kind       string              `json:"kind"`
	Title      string              `json:"title,omitempty"`
	Source     string              `json:"source,omitempty"`
	Date       string              `json:"date,omitempty"`
	Paragraphs [][]OutRun          `json:"paragraphs,omitempty"`
	Lines      []OutTranscriptLine `json:"lines,omitempty"`
}

type OutTable struct {
	Caption string       `json:"caption,omitempty"`
	Columns []string     `json:"columns"`
	Rows    [][][]OutRun `json:"rows"`
}

// OutDocLink: для недоступной цели (нет такого документа или он закрыт от читателя) — только
// «недоступно»: ни шифр, ни название, ни примечание автора не раскрываются.
type OutDocLink struct {
	Available bool   `json:"available"`
	Code      string `json:"code,omitempty"`
	Slug      string `json:"slug,omitempty"`
	Title     string `json:"title,omitempty"`
	Type      string `json:"type,omitempty"`
	TypeName  string `json:"type_name,omitempty"`
	Note      string `json:"note,omitempty"`
}

type OutDivider struct {
	Style string `json:"style"`
}

type OutPage struct {
	Number string `json:"number,omitempty"`
}

type OutFootnote struct {
	Mark string   `json:"mark"`
	Text []OutRun `json:"text"`
}

type OutAppendix struct {
	Number string `json:"number,omitempty"`
	Title  string `json:"title"`
}

type OutContainmentProcedure struct {
	Title string     `json:"title,omitempty"`
	Steps [][]OutRun `json:"steps"`
}

type OutDirective struct {
	Recipient string   `json:"recipient"`
	Order     []OutRun `json:"order"`
	Deadline  string   `json:"deadline,omitempty"`
}

type OutTimelineEntry struct {
	Time  string   `json:"time,omitempty"`
	Event []OutRun `json:"event"`
}

type OutIncidentTimeline struct {
	Entries []OutTimelineEntry `json:"entries"`
}

type OutPersonnelRecord struct {
	Rank      string `json:"rank,omitempty"`
	Clearance string `json:"clearance,omitempty"`
	Status    string `json:"status,omitempty"`
}

type OutRosterEntry struct {
	Name     string `json:"name"`
	Position string `json:"position,omitempty"`
}

type OutRoster struct {
	Entries []OutRosterEntry `json:"entries"`
}

type OutHypothesis struct {
	Text      []OutRun `json:"text"`
	Result    []OutRun `json:"result"`
	Confirmed *bool    `json:"confirmed,omitempty"`
}

type OutQAEntry struct {
	Question []OutRun `json:"question"`
	Answer   []OutRun `json:"answer"`
}

type OutQA struct {
	Entries []OutQAEntry `json:"entries"`
}

type OutRoutingEntry struct {
	Who      string `json:"who"`
	Decision string `json:"decision"`
}

type OutImage struct {
	Upload  string `json:"upload"`
	Caption string `json:"caption,omitempty"`
	Sticker string `json:"sticker"`
}

type OutAudio struct {
	Upload     string   `json:"upload"`
	Title      string   `json:"title,omitempty"`
	Transcript []OutRun `json:"transcript,omitempty"`
}

type OutRouting struct {
	Entries []OutRoutingEntry `json:"entries"`
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
		func(d *headingData, _ *renderer) any { return OutHeading{Depth: d.Depth, Text: d.Text} })

	register("paragraph",
		func(d *paragraphData, p *Problems, path string) { d.Text.check(p, path+".text", true) },
		func(d *paragraphData, r *renderer) any { return OutParagraph{Text: d.Text.render(r.viewer)} })

	register("list",
		func(d *listData, p *Problems, path string) { checkRichList(d.Items, p, path+".items", 1, 200, false) },
		func(d *listData, r *renderer) any {
			return OutList{Ordered: d.Ordered, Items: renderRichList(d.Items, r.viewer)}
		})

	register("quote",
		func(d *quoteData, p *Problems, path string) {
			d.Text.check(p, path+".text", true)
			p.text(path+".source", d.Source, 0, 200)
		},
		func(d *quoteData, r *renderer) any { return OutQuote{Text: d.Text.render(r.viewer), Source: d.Source} })

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
			out := OutExperimentLog{Title: d.Title, Entries: make([]OutLogEntry, len(d.Entries))}
			for i, e := range d.Entries {
				out.Entries[i] = OutLogEntry{Date: e.Date, Participants: e.Participants, Text: e.Text.render(r.viewer)}
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
		func(d *stampData, _ *renderer) any { return OutStamp{Text: d.Text, Tone: d.Tone, Tilt: d.Tilt} })

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
			return OutMemo{
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
			out := OutClipping{Kind: d.Kind, Title: d.Title, Source: d.Source, Date: d.Date}
			if d.Kind == "transcript" {
				out.Lines = make([]OutTranscriptLine, len(d.Lines))
				for i, l := range d.Lines {
					out.Lines[i] = OutTranscriptLine{Speaker: l.Speaker, Text: l.Text.render(r.viewer)}
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
			out := OutTable{Caption: d.Caption, Columns: d.Columns, Rows: make([][][]OutRun, len(d.Rows))}
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
		func(d *dividerData, _ *renderer) any { return OutDivider{Style: d.Style} })

	register("page",
		func(d *pageData, p *Problems, path string) { p.text(path+".number", d.Number, 0, 20) },
		func(d *pageData, _ *renderer) any { return OutPage{Number: d.Number} })

	register("footnote",
		func(d *footnoteData, p *Problems, path string) {
			p.text(path+".mark", d.Mark, 1, 8)
			d.Text.check(p, path+".text", true)
		},
		func(d *footnoteData, r *renderer) any {
			return OutFootnote{Mark: d.Mark, Text: d.Text.render(r.viewer)}
		})

	register("appendix",
		func(d *appendixData, p *Problems, path string) {
			p.text(path+".number", d.Number, 0, 20)
			p.text(path+".title", d.Title, 1, 200)
		},
		func(d *appendixData, _ *renderer) any { return OutAppendix{Number: d.Number, Title: d.Title} })

	register("containment_procedure",
		func(d *containmentProcedureData, p *Problems, path string) {
			p.text(path+".title", d.Title, 0, 200)
			checkRichList(d.Steps, p, path+".steps", 1, 100, false)
		},
		func(d *containmentProcedureData, r *renderer) any {
			return OutContainmentProcedure{Title: d.Title, Steps: renderRichList(d.Steps, r.viewer)}
		})

	register("directive",
		func(d *directiveData, p *Problems, path string) {
			p.text(path+".recipient", d.Recipient, 1, 200)
			d.Order.check(p, path+".order", true)
			p.text(path+".deadline", d.Deadline, 0, 40)
		},
		func(d *directiveData, r *renderer) any {
			return OutDirective{Recipient: d.Recipient, Order: d.Order.render(r.viewer), Deadline: d.Deadline}
		})

	register("incident_timeline",
		func(d *incidentTimelineData, p *Problems, path string) {
			if len(d.Entries) == 0 || len(d.Entries) > 200 {
				p.Add(path+".entries", "записей должно быть от 1 до 200")
				return
			}
			for i, e := range d.Entries {
				ep := fmt.Sprintf("%s.entries[%d]", path, i)
				p.text(ep+".time", e.Time, 0, 40)
				e.Event.check(p, ep+".event", true)
			}
		},
		func(d *incidentTimelineData, r *renderer) any {
			out := OutIncidentTimeline{Entries: make([]OutTimelineEntry, len(d.Entries))}
			for i, e := range d.Entries {
				out.Entries[i] = OutTimelineEntry{Time: e.Time, Event: e.Event.render(r.viewer)}
			}
			return out
		})

	register("personnel_record",
		func(d *personnelRecordData, p *Problems, path string) {
			p.text(path+".rank", d.Rank, 0, 100)
			p.text(path+".clearance", d.Clearance, 0, 100)
			if d.Status == "" {
				d.Status = "active"
			}
			p.oneOf(path+".status", d.Status, "active", "transferred", "deceased", "missing", "unknown")
		},
		func(d *personnelRecordData, _ *renderer) any {
			return OutPersonnelRecord{Rank: d.Rank, Clearance: d.Clearance, Status: d.Status}
		})

	register("roster",
		func(d *rosterData, p *Problems, path string) {
			if len(d.Entries) == 0 || len(d.Entries) > 200 {
				p.Add(path+".entries", "записей должно быть от 1 до 200")
				return
			}
			for i, e := range d.Entries {
				ep := fmt.Sprintf("%s.entries[%d]", path, i)
				p.text(ep+".name", e.Name, 1, 200)
				p.text(ep+".position", e.Position, 0, 200)
			}
		},
		func(d *rosterData, _ *renderer) any {
			out := OutRoster{Entries: make([]OutRosterEntry, len(d.Entries))}
			for i, e := range d.Entries {
				out.Entries[i] = OutRosterEntry{Name: e.Name, Position: e.Position}
			}
			return out
		})

	register("hypothesis",
		func(d *hypothesisData, p *Problems, path string) {
			d.Text.check(p, path+".text", true)
			d.Result.check(p, path+".result", true)
		},
		func(d *hypothesisData, r *renderer) any {
			return OutHypothesis{Text: d.Text.render(r.viewer), Result: d.Result.render(r.viewer), Confirmed: d.Confirmed}
		})

	register("qa",
		func(d *qaData, p *Problems, path string) {
			if len(d.Entries) == 0 || len(d.Entries) > 200 {
				p.Add(path+".entries", "записей должно быть от 1 до 200")
				return
			}
			for i, e := range d.Entries {
				ep := fmt.Sprintf("%s.entries[%d]", path, i)
				e.Question.check(p, ep+".question", true)
				e.Answer.check(p, ep+".answer", true)
			}
		},
		func(d *qaData, r *renderer) any {
			out := OutQA{Entries: make([]OutQAEntry, len(d.Entries))}
			for i, e := range d.Entries {
				out.Entries[i] = OutQAEntry{Question: e.Question.render(r.viewer), Answer: e.Answer.render(r.viewer)}
			}
			return out
		})

	register("image",
		func(d *imageData, p *Problems, path string) {
			if !uploadKeyRe.MatchString(d.Upload) {
				p.Add(path+".upload", "ключ загруженного файла: 32 знака 0–9 и a–f (его выдаёт загрузка)")
			}
			p.text(path+".caption", d.Caption, 0, 300)
			if d.Sticker == "" {
				d.Sticker = "none"
			}
			p.oneOf(path+".sticker", d.Sticker, "none", "frame", "clip", "stamp")
		},
		func(d *imageData, _ *renderer) any {
			return OutImage{Upload: d.Upload, Caption: d.Caption, Sticker: d.Sticker}
		})

	register("audio",
		func(d *audioData, p *Problems, path string) {
			if !uploadKeyRe.MatchString(d.Upload) {
				p.Add(path+".upload", "ключ загруженного файла: 32 знака 0–9 и a–f (его выдаёт загрузка)")
			}
			p.text(path+".title", d.Title, 0, 200)
			if len(d.Transcript) > 0 {
				d.Transcript.check(p, path+".transcript", true)
			}
		},
		func(d *audioData, r *renderer) any {
			out := OutAudio{Upload: d.Upload, Title: d.Title}
			if len(d.Transcript) > 0 {
				out.Transcript = d.Transcript.render(r.viewer)
			}
			return out
		})

	register("routing",
		func(d *routingData, p *Problems, path string) {
			if len(d.Entries) == 0 || len(d.Entries) > 100 {
				p.Add(path+".entries", "записей должно быть от 1 до 100")
				return
			}
			for i, e := range d.Entries {
				ep := fmt.Sprintf("%s.entries[%d]", path, i)
				p.text(ep+".who", e.Who, 1, 200)
				p.oneOf(ep+".decision", e.Decision, "approved", "rejected", "noted", "pending")
			}
		},
		func(d *routingData, _ *renderer) any {
			out := OutRouting{Entries: make([]OutRoutingEntry, len(d.Entries))}
			for i, e := range d.Entries {
				out.Entries[i] = OutRoutingEntry{Who: e.Who, Decision: e.Decision}
			}
			return out
		})
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
				return OutDocLink{Available: false}
			}
			typeName := t.TypeName
			if typeName == "" {
				typeName = t.Type.Name()
			}
			return OutDocLink{
				Available: true, Code: t.Code, Slug: t.Slug, Title: t.Title,
				Type: string(t.Type), TypeName: typeName, Note: d.Note,
			}
		})
	spec.links = func(v any) []string { return []string{v.(*docLinkData).Code} }
	return spec
}
