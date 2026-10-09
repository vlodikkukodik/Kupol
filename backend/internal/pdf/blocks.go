package pdf

import (
	"encoding/json"
	"fmt"
	"strings"

	"kupol/internal/documents"
	"kupol/internal/i18n"
)

// sheet — лист одного документа: свойства, язык и всё, что понадобится при сборке разметки.
type sheet struct {
	doc    *documents.OutDocument
	lang   i18n.Lang
	origin string // https://… — для ссылок на документы
	level  int    // допуск, с которым собрано (для картинок и подписей)
	images map[string]string
	open   func(key string) (string, error)
}

func (s *sheet) t(msg string, args ...any) string { return s.lang.T(msg, args...) }

// blocks — все блоки документа через одиницу разделения.
func (s *sheet) blocks() (string, error) {
	var b strings.Builder
	for i, blk := range s.doc.Blocks {
		src, err := s.block(blk)
		if err != nil {
			return "", err
		}
		if i > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(src)
	}
	return b.String(), nil
}

// block — один блок ответа читателю в разметку Typst. Структура подписей повторяет выгрузку в Markdown
// (exchange.go) и страницу на сайте (components/document/blocks/*): PDF — тот же документ, что видит читатель.
func (s *sheet) block(b documents.OutBlock) (string, error) {
	switch b.Type {
	case "redacted":
		var d struct {
			Level int `json:"level"`
		}
		if err := unmarshalData(b.Data, &d); err != nil {
			return "", fmt.Errorf("блок redacted: %w", err)
		}
		return s.redactedPlate(d.Level), nil
	case "heading":
		d, err := as[documents.OutHeading](b)
		if err != nil {
			return "", err
		}
		depth := d.Depth
		if depth < 1 || depth > 3 {
			depth = 2
		}
		return strings.Repeat("=", depth) + " " + escape(d.Text), nil
	case "paragraph":
		d, err := as[documents.OutParagraph](b)
		if err != nil {
			return "", err
		}
		return rich(s.doc.Code, d.Text), nil
	case "list":
		d, err := as[documents.OutList](b)
		if err != nil {
			return "", err
		}
		marker := "#list"
		if d.Ordered {
			marker = "#enum"
		}
		items := make([]string, len(d.Items))
		for i, it := range d.Items {
			items[i] = "[" + rich(s.doc.Code, it) + "]"
		}
		return marker + "(\n  " + strings.Join(items, ",\n  ") + ",\n)", nil
	case "quote":
		d, err := as[documents.OutQuote](b)
		if err != nil {
			return "", err
		}
		open := "#quote(block: true"
		if d.Source != "" {
			open += ", attribution: [" + escape(d.Source) + "]"
		}
		return open + ")[\n" + rich(s.doc.Code, d.Text) + "\n]", nil
	case "dossier_header":
		return s.dossierHeader(), nil
	case "experiment_log":
		d, err := as[documents.OutExperimentLog](b)
		if err != nil {
			return "", err
		}
		return s.experimentLog(d), nil
	case "stamp":
		d, err := as[documents.OutStamp](b)
		if err != nil {
			return "", err
		}
		return s.stampBlock(d), nil
	case "memo":
		d, err := as[documents.OutMemo](b)
		if err != nil {
			return "", err
		}
		return s.memo(d), nil
	case "clipping":
		d, err := as[documents.OutClipping](b)
		if err != nil {
			return "", err
		}
		return s.clipping(d), nil
	case "table":
		d, err := as[documents.OutTable](b)
		if err != nil {
			return "", err
		}
		return s.table(d), nil
	case "doc_link":
		d, err := as[documents.OutDocLink](b)
		if err != nil {
			return "", err
		}
		return s.docLink(d), nil
	case "divider":
		d, err := as[documents.OutDivider](b)
		if err != nil {
			return "", err
		}
		if d.Style == "stars" {
			return "#align(center)[#text(fill: luma(90), tracking: 0.6em)[* * *]]", nil
		}
		return "#line(length: 100%, stroke: 1.5pt + black)", nil
	case "page":
		d, err := as[documents.OutPage](b)
		if err != nil {
			return "", err
		}
		return s.pageMark(d), nil
	case "footnote":
		d, err := as[documents.OutFootnote](b)
		if err != nil {
			return "", err
		}
		return "#block(above: 0.5em, below: 0.5em, inset: (left: 1em), stroke: (left: 1pt + luma(120)))[#text(size: 9pt)[#strong[" +
			escape(d.Mark) + ".] " + rich(s.doc.Code, d.Text) + "]]", nil
	case "appendix":
		d, err := as[documents.OutAppendix](b)
		if err != nil {
			return "", err
		}
		mark := s.t("Приложение")
		if d.Number != "" {
			mark = s.t("Приложение № %s", d.Number)
		}
		return "#block(above: 1.6em, below: 1em, width: 100%, stroke: (top: 2.5pt + black, bottom: 0.75pt + black), inset: (top: 5pt, bottom: 5pt))[\n" +
			"#lab[" + escape(mark) + "]\n#linebreak()\n#text(font: \"Oswald\", size: 13pt, weight: \"semibold\")[ " + escape(d.Title) + "]\n]", nil
	case "containment_procedure":
		d, err := as[documents.OutContainmentProcedure](b)
		if err != nil {
			return "", err
		}
		return s.containment(d), nil
	case "directive":
		d, err := as[documents.OutDirective](b)
		if err != nil {
			return "", err
		}
		return s.directive(d), nil
	case "incident_timeline":
		d, err := as[documents.OutIncidentTimeline](b)
		if err != nil {
			return "", err
		}
		items := make([]string, len(d.Entries))
		for i, e := range d.Entries {
			line := rich(s.doc.Code, e.Event)
			if e.Time != "" {
				line = "**" + escape(e.Time) + "** — " + line
			}
			items[i] = "[" + line + "]"
		}
		return "#list(\n  " + strings.Join(items, ",\n  ") + ",\n)", nil
	case "personnel_record":
		d, err := as[documents.OutPersonnelRecord](b)
		if err != nil {
			return "", err
		}
		return s.personnel(d), nil
	case "roster":
		d, err := as[documents.OutRoster](b)
		if err != nil {
			return "", err
		}
		items := make([]string, len(d.Entries))
		for i, e := range d.Entries {
			line := "**" + escape(e.Name) + "**"
			if e.Position != "" {
				line += " — " + escape(e.Position)
			}
			items[i] = "[" + line + "]"
		}
		return "#list(\n  " + strings.Join(items, ",\n  ") + ",\n)", nil
	case "hypothesis":
		d, err := as[documents.OutHypothesis](b)
		if err != nil {
			return "", err
		}
		return s.hypothesis(d), nil
	case "qa":
		d, err := as[documents.OutQA](b)
		if err != nil {
			return "", err
		}
		return s.qa(d), nil
	case "routing":
		d, err := as[documents.OutRouting](b)
		if err != nil {
			return "", err
		}
		return s.routing(d), nil
	case "image":
		d, err := as[documents.OutImage](b)
		if err != nil {
			return "", err
		}
		return s.image(d), nil
	case "audio":
		d, err := as[documents.OutAudio](b)
		if err != nil {
			return "", err
		}
		return s.audio(d), nil
	default:
		return "", fmt.Errorf("блок %s: неизвестный тип %q", b.ID, b.Type)
	}
}

// as — данные блока известного вида; расхождение с контрактом — ошибка, а не пустой блок.
func as[T any](b documents.OutBlock) (T, error) {
	var zero T
	d, ok := b.Data.(T)
	if !ok {
		return zero, fmt.Errorf("блок %s (%s): данные %T, ожидался другой тип", b.ID, b.Type, b.Data)
	}
	return d, nil
}

// unmarshalData — данные блока в структуру (закрытый блок приходит без типизированного вида).
func unmarshalData(v any, dst any) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, dst)
}

// fileExt — расширение файла изображения для ссылки в разметке.
func fileExt(path string) string {
	if i := strings.LastIndexByte(path, '.'); i >= 0 {
		return path[i:]
	}
	return ""
}

// redactedPlate — закрытый блок: сплошная чёрная плашка с названием и нужным допуском (как на сайте).
func (s *sheet) redactedPlate(level int) string {
	accessText := s.t("допуск не ниже уровня %d (%s)", level, documents.LevelNameIn(s.lang, level))
	if level >= documents.MaxLevel {
		accessText = s.t("только Директорат")
	}
	return "#block(above: 1em, below: 1em, width: 100%, fill: black, inset: (x: 10pt, y: 9pt), radius: 1pt)[\n" +
		"#grid(columns: (1fr, auto), align: (left + horizon, right + horizon))[\n" +
		"#text(fill: white, font: \"Oswald\", weight: \"bold\", size: 11.5pt, tracking: 0.1em)[" + escape(s.t("Данные удалены")) + "],\n" +
		"#text(fill: luma(215), size: 8pt, tracking: 0.06em)[" + escape(accessText) + "],\n" +
		"]\n]"
}

// experimentLog — журнал: заголовок (или название по умолчанию) и записи «дата — участники» с текстом.
func (s *sheet) experimentLog(d documents.OutExperimentLog) string {
	title := d.Title
	if title == "" {
		title = s.t("Журнал эксперимента")
	}
	var b strings.Builder
	b.WriteString("#block(width: 100%, inset: (x: 10pt, y: 8pt), stroke: 0.75pt + luma(60), breakable: true)[\n")
	b.WriteString("#text(font: \"Oswald\", weight: \"bold\", size: 11pt, tracking: 0.08em)[" + escape(title) + "]\n")
	for _, e := range d.Entries {
		b.WriteString("\n\n")
		head := ""
		if e.Date != "" {
			head = "**" + escape(e.Date) + "**"
		}
		if len(e.Participants) > 0 {
			names := make([]string, len(e.Participants))
			for i, p := range e.Participants {
				names[i] = escape(p)
			}
			if head != "" {
				head += " — "
			}
			head += s.t("Участники: %s", strings.Join(names, ", "))
		}
		if head != "" {
			b.WriteString(head + "\n\n")
		}
		b.WriteString("#pad(left: 1em)[" + rich(s.doc.Code, e.Text) + "]")
	}
	b.WriteString("]")
	return b.String()
}

// stampBlock — штамп-оттиск: рамка и надпись красной или чёрной краской под наклоном.
func (s *sheet) stampBlock(d documents.OutStamp) string {
	color := "rgb(176, 24, 24)"
	if d.Tone == "ink" {
		color = "black"
	}
	tilt := float64(d.Tilt)
	if tilt == 0 {
		tilt = -6
	}
	return "#block(above: 0.8em, below: 0.8em)[#rotate(" + fmt.Sprintf("%.1f", tilt) +
		"deg, box(inset: (x: 9pt, y: 4pt), stroke: 2.2pt + " + color + ", radius: 5pt)[#text(fill: " + color +
		", font: \"Oswald\", weight: \"bold\", size: 15pt, tracking: 0.14em)[" + escape(strings.ToUpper(d.Text)) + "]])]"
}

// memo — бланк: заголовок «Вид № N», поля и текст, подпись справа.
func (s *sheet) memo(d documents.OutMemo) string {
	kinds := map[string]string{"memo": "Служебная записка", "order": "Приказ", "letter": "Письмо"}
	title := s.t(kinds[d.Kind])
	if title == "" {
		title = s.t("Документ")
	}
	if d.Number != "" {
		title = s.t("%s № %s", title, d.Number)
	}
	var fields [][2]string
	if d.Date != "" {
		fields = append(fields, [2]string{s.t("Дата"), d.Date})
	}
	if d.From != "" {
		fields = append(fields, [2]string{s.t("От"), d.From})
	}
	if len(d.To) > 0 {
		fields = append(fields, [2]string{s.t("Кому"), strings.Join(d.To, ", ")})
	}
	if d.Subject != "" {
		fields = append(fields, [2]string{s.t("Тема"), d.Subject})
	}
	var b strings.Builder
	b.WriteString("#block(width: 100%, inset: 10pt, stroke: 1pt + black, breakable: true)[\n")
	b.WriteString("#text(font: \"Oswald\", weight: \"bold\", size: 12pt, tracking: 0.08em)[" + escape(title) + "]\n")
	if len(fields) > 0 {
		b.WriteString("\n\n" + gridRows(fields))
	}
	for _, p := range d.Body {
		b.WriteString("\n\n" + rich(s.doc.Code, p))
	}
	if d.Signature != "" {
		b.WriteString("\n\n#align(right)[#emph[" + escape(d.Signature) + "]]")
	}
	b.WriteString("]")
	return b.String()
}

// clipping — вырезка: вид, заголовок, источник и дата шапкой, затем абзацы или реплики расшифровки.
func (s *sheet) clipping(d documents.OutClipping) string {
	kinds := map[string]string{
		"newspaper": "Газетная вырезка", "handwritten": "Рукописная заметка",
		"transcript": "Расшифровка записи", "other": "Материал",
	}
	kind := s.t(kinds[d.Kind])
	if kind == "" {
		kind = s.t("Материал")
	}
	var head strings.Builder
	head.WriteString("**" + escape(kind) + "**")
	if d.Title != "" {
		head.WriteString(" «" + escape(d.Title) + "»")
	}
	if d.Source != "" {
		head.WriteString(", " + escape(d.Source))
	}
	if d.Date != "" {
		head.WriteString(", " + escape(d.Date))
	}
	var b strings.Builder
	b.WriteString("#block(above: 0.7em, below: 0.7em, inset: (left: 10pt), stroke: (left: 2.5pt + luma(60)), breakable: true)[\n")
	b.WriteString(head.String() + "\n")
	if d.Kind == "transcript" {
		for _, l := range d.Lines {
			line := rich(s.doc.Code, l.Text)
			if l.Speaker != "" {
				line = "**" + escape(l.Speaker) + ":** " + line
			}
			b.WriteString("\n\n" + line)
		}
	} else {
		for _, p := range d.Paragraphs {
			b.WriteString("\n\n" + rich(s.doc.Code, p))
		}
	}
	b.WriteString("]")
	return b.String()
}

// table — таблица с подписью и жирной шапкой.
func (s *sheet) table(d documents.OutTable) string {
	var b strings.Builder
	if d.Caption != "" {
		b.WriteString("#text(font: \"Oswald\", size: 9.5pt, tracking: 0.08em)[" + escape(d.Caption) + "]\n#v(3pt)\n")
	}
	cols := len(d.Columns)
	if cols == 0 {
		cols = 1
	}
	b.WriteString("#table(columns: " + fmt.Sprint(cols) + ", align: left + top, stroke: 0.5pt + black, inset: 4.5pt")
	for _, c := range d.Columns {
		b.WriteString(", [#strong[" + escape(c) + "]]")
	}
	for _, row := range d.Rows {
		for i := 0; i < cols; i++ {
			cell := ""
			if i < len(row) {
				cell = rich(s.doc.Code, row[i])
			}
			b.WriteString(", [" + cell + "]")
		}
	}
	b.WriteString(")")
	return b.String()
}

// docLink — ссылка на другой документ: доступная цель в рамке, закрытая — «Засекречен».
func (s *sheet) docLink(d documents.OutDocLink) string {
	if !d.Available || d.Slug == "" {
		return "#block(above: 0.7em, below: 0.7em, width: 100%, inset: (x: 10pt, y: 6pt), stroke: 1.5pt + black)[\n" +
			"#lab[" + escape(s.t("Связанный документ")) + "]\n#linebreak()\n#text(weight: \"bold\")[" +
			escape(s.t("Засекречен")) + "]\n]"
	}
	main := "**" + escape(d.Code) + "** " + escape(d.Title)
	var b strings.Builder
	b.WriteString("#block(above: 0.7em, below: 0.7em, width: 100%, inset: (x: 10pt, y: 6pt), stroke: 1.5pt + black)[\n")
	if d.TypeName != "" {
		b.WriteString("#lab[" + escape(d.TypeName) + "]\n#linebreak()\n")
	}
	b.WriteString("#link(\"" + escapeString(s.origin+"/doc/"+d.Slug) + "\")[" + main + "]\n")
	if d.Note != "" {
		b.WriteString("\n#text(size: 9pt, fill: luma(70))[" + escape(d.Note) + "]")
	}
	b.WriteString("\n]")
	return b.String()
}

// pageMark — метка ручного разрыва страницы, как на сайте: пунктир и «— стр. N —».
func (s *sheet) pageMark(d documents.OutPage) string {
	label := s.t("— стр. —")
	if d.Number != "" {
		label = s.t("— стр. %s —", d.Number)
	}
	return "#block(above: 1em, below: 1em, width: 100%)[\n" +
		"#line(length: 100%, stroke: (paint: luma(140), thickness: 0.75pt, dash: \"dashed\"))\n" +
		"\n#align(center)[#text(size: 8.5pt, fill: luma(100), tracking: 0.12em)[" + escape(label) + "]]\n]"
}

// containment — процедура содержания: заголовок (или название по умолчанию) и пронумерованные шаги.
func (s *sheet) containment(d documents.OutContainmentProcedure) string {
	title := d.Title
	if title == "" {
		title = s.t("Процедура содержания")
	}
	steps := make([]string, len(d.Steps))
	for i, st := range d.Steps {
		steps[i] = "[" + rich(s.doc.Code, st) + "]"
	}
	return "#text(font: \"Oswald\", weight: \"bold\", size: 11pt, tracking: 0.06em)[" + escape(title) + "]\n" +
		"#enum(\n  " + strings.Join(steps, ",\n  ") + ",\n)"
}

// directive — пункт приказа: адресат, предписание и срок.
func (s *sheet) directive(d documents.OutDirective) string {
	line := "**" + escape(d.Recipient) + "** — " + rich(s.doc.Code, d.Order)
	if d.Deadline != "" {
		line += " (" + s.t("Срок: %s", escape(d.Deadline)) + ")"
	}
	return "#block(inset: (left: 1em), stroke: (left: 2pt + black))[" + line + "]"
}

// personnel — личные данные: звание, допуск, статус подписями.
func (s *sheet) personnel(d documents.OutPersonnelRecord) string {
	statuses := map[string]string{
		"active": "На службе", "transferred": "Переведён(а)", "deceased": "Погиб(ла)",
		"missing": "Пропал(а) без вести", "unknown": "Неизвестно",
	}
	var fields [][2]string
	if d.Rank != "" {
		fields = append(fields, [2]string{s.t("Звание"), d.Rank})
	}
	if d.Clearance != "" {
		fields = append(fields, [2]string{s.t("Допуск"), d.Clearance})
	}
	if d.Status != "" {
		status := s.t(statuses[d.Status])
		if status == "" {
			status = d.Status
		}
		fields = append(fields, [2]string{s.t("Статус"), status})
	}
	if len(fields) == 0 {
		return ""
	}
	return gridRows(fields)
}

// hypothesis — гипотеза, результат и вердикт.
func (s *sheet) hypothesis(d documents.OutHypothesis) string {
	var b strings.Builder
	b.WriteString("**" + s.t("Гипотеза") + ":** " + rich(s.doc.Code, d.Text) + "\\\n")
	b.WriteString("**" + s.t("Результат") + ":** " + rich(s.doc.Code, d.Result))
	if d.Confirmed != nil {
		verdict := s.t("Не подтверждено")
		if *d.Confirmed {
			verdict = s.t("Подтверждено")
		}
		b.WriteString("\n\n#text(font: \"Oswald\", size: 9.5pt, tracking: 0.1em)[" + escape(strings.ToUpper(verdict)) + "]")
	}
	return b.String()
}

// qa — вопросы и ответы.
func (s *sheet) qa(d documents.OutQA) string {
	parts := make([]string, len(d.Entries))
	for i, e := range d.Entries {
		parts[i] = "**" + s.t("Вопрос") + ":** " + rich(s.doc.Code, e.Question) + "\\\n" +
			"**" + s.t("Ответ") + ":** " + rich(s.doc.Code, e.Answer)
	}
	return strings.Join(parts, "\n#v(4pt)\n")
}

// routing — маршрут согласования: кто и какое решение (справа, как на сайте).
func (s *sheet) routing(d documents.OutRouting) string {
	decisions := map[string]string{
		"approved": "Одобрено", "rejected": "Отклонено",
		"noted": "Принято к сведению", "pending": "На рассмотрении",
	}
	var b strings.Builder
	b.WriteString("#grid(columns: (1fr, auto), column-gutter: 12pt, row-gutter: 3pt")
	for _, e := range d.Entries {
		decision := s.t(decisions[e.Decision])
		if decision == "" {
			decision = s.t(decisions["pending"])
		}
		b.WriteString(", [" + escape(e.Who) + "], [#text(font: \"Oswald\", size: 9pt, tracking: 0.08em, weight: \"bold\")[" +
			escape(strings.ToUpper(decision)) + "]]")
	}
	b.WriteString(")")
	return b.String()
}

// image — иллюстрация: файл читателя (или заглушка, если его нет), подпись под ней.
func (s *sheet) image(d documents.OutImage) string {
	path := ""
	if s.open != nil {
		if p, err := s.open(d.Upload); err == nil {
			path = p
		}
	}
	var inner string
	if path == "" {
		inner = "block(width: 100%, inset: 8pt, stroke: (paint: luma(150), thickness: 0.75pt, dash: \"dashed\"))[" +
			"#text(fill: luma(110), size: 9.5pt)[" + escape(s.t("Изображение недоступно.")) + "]]"
	} else {
		ref := "img/" + d.Upload + fileExt(path)
		s.images[ref] = path
		img := "image(\"" + escapeString(ref) + "\", width: 86%)"
		if d.Sticker == "frame" {
			img = "box(stroke: 0.5pt + luma(80), inset: 6pt, radius: 2pt)[" + img + "]"
		}
		inner = img
		if d.Sticker == "stamp" {
			inner = "box(width: 100%)[" + img +
				" #place(bottom + right, dx: -6pt, dy: -6pt, circle(radius: 30pt, stroke: 3pt + rgb(176, 24, 24)))]"
		}
	}
	caption := ""
	if d.Caption != "" {
		caption = ", caption: [" + escape(d.Caption) + "]"
	}
	return "#figure(" + inner + caption + ")"
}

// audio — аудиозапись: в PDF нет звука, остаются название и расшифровка.
func (s *sheet) audio(d documents.OutAudio) string {
	var b strings.Builder
	b.WriteString("#block(above: 0.7em, below: 0.7em, inset: 8pt, stroke: 0.5pt + luma(140), breakable: true)[\n")
	if d.Title != "" {
		b.WriteString("#strong[" + escape(d.Title) + "]\n#linebreak()\n")
	}
	b.WriteString("#text(font: \"Oswald\", size: 9pt, tracking: 0.1em)[" + escape(s.t("Аудиозапись")) + "]")
	if len(d.Transcript) > 0 {
		b.WriteString("\n\n#text(fill: luma(90), size: 9pt)[" + escape(s.t("Расшифровка")) + "]\n")
		b.WriteString(rich(s.doc.Code, d.Transcript))
	}
	b.WriteString("\n]")
	return b.String()
}

// gridRows — пары «подпись — значение» в две колонки (шапка досье, личные данные, поля бланка).
func gridRows(fields [][2]string) string {
	var b strings.Builder
	b.WriteString("#grid(columns: (9.5em, 1fr), column-gutter: 10pt, row-gutter: 3pt")
	for _, f := range fields {
		b.WriteString(", lab[" + escape(f[0]) + "], text(weight: \"bold\")[" + escape(f[1]) + "]")
	}
	b.WriteString(")")
	return b.String()
}
