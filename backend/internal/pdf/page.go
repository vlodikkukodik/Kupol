package pdf

import (
	"fmt"
	"strings"

	"kupol/internal/documents"
)

// preamble — общий набор листа: шрифты, абзацы, списки, заголовки, таблицы и подписи.
// Разметка блоков ссылается на lab(...) отсюда; всё остальное — правила для всего документа.
const preamble = `#set text(font: ("IBM Plex Mono"), size: 10pt, lang: %[1]q, region: %[1]q)
#set par(spacing: 0.55em, leading: 0.85em, justify: false)
#set list(indent: 1.5em, body-indent: 0.4em, marker: ([•], [–], [◦]))
#set enum(indent: 1.5em, body-indent: 0.4em, numbering: "1.")
#show heading: it => {
  let sizes = (15.5pt, 13pt, 11.5pt)
  block(above: 1.7em, below: 0.7em)[
    #text(font: "Oswald", weight: "semibold", size: sizes.at(calc.min(it.level, 3) - 1), tracking: 0.05em, it.body)
  ]
}
#show figure.caption: set text(size: 8.5pt, fill: luma(75))
#show figure: set block(breakable: false)
#show link: set text(fill: rgb(24, 56, 120))
#let lab(body) = text(font: "Oswald", size: 8.5pt, tracking: 0.12em, fill: luma(75), upper(body))
`

// setup — метаданные PDF и колонтитулы. Гриф, классификация и шифр — в верхнем колонтитуле листа
// (как полоса на сайте), классификация, номер экземпляра и гриф — в нижнем; счётчик страниц по центру.
func (s *sheet) setup(title, author string) string {
	lang := "ru"
	if s.lang == "it" {
		lang = "it"
	}
	p := fmt.Sprintf(preamble, lang)

	grif := escape(s.doc.Grif)
	class := escape(classification(s.lang, s.doc.Level))
	code := escape(s.doc.Code)
	copyNo := escape(copyText(s.lang, s.doc.CopyNumber))

	var b strings.Builder
	b.WriteString("#set document(title: " + fmt.Sprintf("%q", title))
	if author != "" {
		b.WriteString(", author: " + fmt.Sprintf("%q", author))
	}
	b.WriteString(")\n")
	b.WriteString("#set page(\n")
	b.WriteString("  paper: \"a4\",\n")
	b.WriteString("  margin: (x: 19mm, top: 25mm, bottom: 23mm),\n")
	b.WriteString("  header-ascent: 11mm,\n")
	b.WriteString("  footer-descent: 11mm,\n")
	b.WriteString("  header: context [\n")
	b.WriteString("    #set text(font: \"Oswald\", size: 7.5pt, tracking: 0.14em)\n")
	b.WriteString("    #grid(columns: (1fr, auto, 1fr),\n")
	b.WriteString("      align(left + horizon)[#upper[" + grif + "]],\n")
	b.WriteString("      align(center + horizon)[#text(fill: rgb(150, 18, 18), weight: \"bold\")[#upper[" + class + "]]],\n")
	b.WriteString("      align(right + horizon)[#upper[" + code + "]])\n")
	b.WriteString("    #v(3pt)\n")
	b.WriteString("    #line(length: 100%, stroke: 1.5pt + black)\n")
	b.WriteString("    #v(1.2pt)\n")
	b.WriteString("    #line(length: 100%, stroke: 0.5pt + black)\n")
	b.WriteString("  ],\n")
	b.WriteString("  footer: context [\n")
	b.WriteString("    #line(length: 100%, stroke: 1.5pt + black)\n")
	b.WriteString("    #v(1.2pt)\n")
	b.WriteString("    #line(length: 100%, stroke: 0.5pt + black)\n")
	b.WriteString("    #v(4pt)\n")
	b.WriteString("    #set text(font: \"Oswald\", size: 7pt, tracking: 0.1em)\n")
	b.WriteString("    #grid(columns: (1fr, auto, 1fr),\n")
	b.WriteString("      align(left)[#upper[" + class + "]],\n")
	b.WriteString("      align(center)[" + copyNo + "],\n")
	b.WriteString("      align(right)[#upper[" + grif + "]])\n")
	b.WriteString("    #v(2pt)\n")
	b.WriteString("    #align(center)[#text(size: 6.5pt, fill: luma(90))[#counter(page).display(\"1\") / #counter(page).final().first()]]\n")
	b.WriteString("  ],\n")
	b.WriteString(")\n")
	return p + b.String()
}

// titleBlock — начало листа: тип и шифр, архивный шифр с номером экземпляра, название и статус-штамп.
func (s *sheet) titleBlock() string {
	d := s.doc
	var b strings.Builder
	kicker := strings.TrimSpace(d.TypeName + " · " + d.Code)
	if strings.TrimSpace(d.TypeName) != "" {
		b.WriteString("#lab[" + escape(kicker) + "]\n\n")
	}
	mark := archiveMark(s.lang, d)
	if d.CopyNumber != "" || mark != "" {
		line := mark
		if cn := copyText(s.lang, d.CopyNumber); cn != "" {
			if line != "" {
				line += " · "
			}
			line += cn
		}
		b.WriteString("#text(size: 8pt, fill: luma(85), tracking: 0.05em)[" + escape(line) + "]\n\n")
	}
	b.WriteString("#text(font: \"Oswald\", size: 21pt, weight: \"semibold\", tracking: 0.01em)[" + escape(d.Title) + "]")
	if d.Status != "" && d.Status != "published" {
		name := documents.Status(d.Status).NameIn(s.lang)
		b.WriteString("\n\n" + stampLine(name, "black", -2, 10))
	}
	return b.String()
}

// mentionsBlock — документы, ссылающиеся на этот (список ссылок), и подпись «Составил(а)».
func (s *sheet) mentionsBlock() string {
	if len(s.doc.MentionedIn) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("#v(1em)\n#lab[" + escape(s.t("Упоминается в")) + "]\n")
	for _, m := range s.doc.MentionedIn {
		main := "**" + escape(m.Code) + "** " + escape(m.Title)
		if m.Slug != "" {
			main = "#link(\"" + escapeString(s.origin+"/doc/"+m.Slug) + "\")[" + main + "]"
		}
		line := main
		if m.TypeName != "" {
			line += " #text(size: 8.5pt, fill: luma(85))[" + escape(m.TypeName) + "]"
		}
		b.WriteString("#v(1.5pt)\n" + line + "\n")
	}
	return b.String()
}

// authorFooter — подпись «Составил(а): …» внизу листа.
func (s *sheet) authorFooter() string {
	if s.doc.Author == nil || *s.doc.Author == "" {
		return ""
	}
	return "#v(1.2em)\n#text(size: 9pt, fill: luma(70))[" + escape(s.t("Составил(а): %s", *s.doc.Author)) + "]"
}
