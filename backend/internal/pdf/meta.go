package pdf

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"kupol/internal/documents"
	"kupol/internal/i18n"
)

// Реалистичность архива (спецификация §7) — те же функции, что в frontend/src/lib/realism.ts:
// гриф по уровню, архивный шифр «Фонд · Опись · Дело · Листов», номер экземпляра.

// classification — гриф секретности по уровню допуска (верхний и нижний колонтитул листа).
func classification(l i18n.Lang, level int) string {
	levels := [...]string{
		"Несекретно",
		"Для служебного пользования",
		"Конфиденциально",
		"Секретно",
		"Совершенно секретно",
		"Особой важности",
		"Особой важности · Особый Совет",
		"Особой важности · только Директорат",
	}
	if level < 0 {
		level = 0
	}
	if level > 7 {
		level = 7
	}
	return l.Translate(levels[level])
}

// fonds — фонд по типу документа: как расложены дела в архиве (FONDS в realism.ts).
var fonds = map[string]int{
	"object": 1, "order": 2, "incident": 3, "personnel": 4,
	"unit": 5, "protocol": 6, "testimony": 7, "memo": 8,
}

const (
	// charsPerSheet — знаков текста на условном «листе машинописи».
	charsPerSheet = 2800
	// redactedBlockChars — закрытый блок занимает примерно строку-другую: его объём читателю неизвестен.
	redactedBlockChars = 300
)

// sheetCount — число листов дела: только то, что читатель видит, плюс явные разрывы страниц.
func sheetCount(blocks []documents.OutBlock) int {
	chars, breaks := 0, 0
	for _, b := range blocks {
		switch b.Type {
		case "redacted":
			chars += redactedBlockChars
		case "page":
			breaks++
		default:
			chars += textLength(b.Data)
		}
	}
	return 1 + breaks + chars/charsPerSheet
}

// textLength — длина видимого текста данных блока: строки суммируются, числа и признаки не считаются
// (как textLength в realism.ts; длина по рунам вместо UTF-16 — расхождение только на суррогатных парах).
func textLength(v any) int {
	switch t := v.(type) {
	case string:
		return len([]rune(t))
	case []any:
		n := 0
		for _, e := range t {
			n += textLength(e)
		}
		return n
	case map[string]any:
		n := 0
		for _, e := range t {
			n += textLength(e)
		}
		return n
	default:
		return 0
	}
}

var codeNumbers = regexp.MustCompile(`\d+`)

// archiveMark — архивный шифр: фонд по типу, опись — год составления, дело — номер в шифре,
// листов — по видимому объёму.
func archiveMark(l i18n.Lang, d *documents.OutDocument) string {
	fond := fonds[d.Type]
	if fond == 0 {
		fond = 9
	}
	file := 0
	if nums := codeNumbers.FindAllString(d.Code, -1); len(nums) > 0 {
		file, _ = strconv.Atoi(nums[len(nums)-1])
	}
	return l.T("Фонд %d · Опись %d · Дело %d · Листов %d", fond, d.Composed.Year, file, sheetCount(d.Blocks))
}

// copyText — «экз. № 0042»; у Гражданина номера нет (экз. б/н).
func copyText(l i18n.Lang, copy string) string {
	if copy == "" {
		return l.T("экз. б/н")
	}
	return l.T("экз. № %s", copy)
}

// composedText — дата составления: «14 сентября 1978 г.», «сентябрь 1978 г.» или «1978 г.».
func composedText(l i18n.Lang, c documents.Composed) string {
	if c.Year == 0 {
		return ""
	}
	monthsGen := [12]string{"января", "февраля", "марта", "апреля", "мая", "июня", "июля", "августа", "сентября", "октября", "ноября", "декабря"}
	monthsNom := [12]string{"январь", "февраль", "март", "апрель", "май", "июнь", "июль", "август", "сентябрь", "октябрь", "ноябрь", "декабрь"}
	monthsIT := [12]string{"gennaio", "febbraio", "marzo", "aprile", "maggio", "giugno", "luglio", "agosto", "settembre", "ottobre", "novembre", "dicembre"}
	month := 0
	if c.Month != nil {
		month = *c.Month
	}
	switch {
	case month < 1 || month > 12:
		return l.T("%d г.", c.Year)
	case c.Day != nil && *c.Day > 0:
		if l == "ru" {
			return l.T("%d %s %d г.", *c.Day, monthsGen[month-1], c.Year)
		}
		return fmt.Sprintf("%d %s %d", *c.Day, monthsIT[month-1], c.Year)
	default:
		if l == "ru" {
			return l.T("%s %d г.", monthsNom[month-1], c.Year)
		}
		return fmt.Sprintf("%s %d", monthsIT[month-1], c.Year)
	}
}

// stampLine — штамп-оттиск в заданном размере (шапка досье, статус у названия).
func stampLine(text, color string, tilt float64, size float64) string {
	return "#rotate(" + fmt.Sprintf("%.1f", tilt) +
		"deg, box(inset: (x: 7pt, y: 2.5pt), stroke: 1.8pt + " + color + ", radius: 4pt)[#text(fill: " + color +
		", font: \"Oswald\", weight: \"bold\", size: " + fmt.Sprintf("%.0f", size) + "pt, tracking: 0.14em)[" +
		escape(strings.ToUpper(text)) + "]])"
}

// dossierHeader — шапка досье: гриф-штамп и сведения о документе (строится из свойств, как на сайте).
func (s *sheet) dossierHeader() string {
	d := s.doc
	var fields [][2]string
	add := func(label, val string) {
		if val != "" {
			fields = append(fields, [2]string{label, val})
		}
	}
	add(s.t("Шифр"), d.Code)
	add(s.t("Тип"), d.TypeName)
	if composed := composedText(s.lang, d.Composed); composed != "" {
		add(s.t("Дата составления"), composed)
	}
	if d.DangerClass != nil {
		add(s.t("Класс опасности"), strconv.Itoa(*d.DangerClass))
	}
	if d.DeviationPoints != nil {
		add(s.t("Пункты отклонения"), s.t("%d п.о.", *d.DeviationPoints))
	}
	add(s.t("Категория"), d.CategoryName)
	add(s.t("Статус содержания"), d.ContainmentName)
	if d.Department != nil {
		add(s.t("Отдел"), *d.Department)
	}
	if d.DiscoveryPlace != nil {
		add(s.t("Место обнаружения"), *d.DiscoveryPlace)
	}
	access := s.t("открытый")
	if d.Level > 0 {
		access = s.t("уровень %d — %s", d.Level, documents.LevelNameIn(s.lang, d.Level))
	}
	fields = append(fields, [2]string{s.t("Допуск"), access})

	var b strings.Builder
	b.WriteString("#block(width: 100%, stroke: 1.5pt + black, inset: (x: 10pt, y: 8pt), breakable: true)[\n")
	b.WriteString("#align(right)[" + stampLine(d.Grif, "black", -1.5, 10.5) + "]\n")
	b.WriteString(gridRows(fields) + "\n")
	b.WriteString("]")
	return b.String()
}
