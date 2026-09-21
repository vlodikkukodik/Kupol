package documents

import (
	"context"
	"strings"
	"unicode/utf8"
)

// Поиск по документам (этап 4). Индекс и его правила — search_index.go.
//
// Читатель видит в выдаче только то, что вправе видеть при чтении: документы не выше его допуска (у неопубликованных —
// только Директорат) и строки индекса не выше его допуска. Слово из закрытого фрагмента не находит документ, не попадает в
// сниппет и не влияет на число найденных: счёт ведётся по тем же условиям, что и выдача.

const (
	maxSearchRunes     = 200
	minSearchRunes     = 2
	defaultSearchPage  = 20
	maxSearchPerPage   = 50
	snippetsPerHit     = 3
	highlightOpen      = "\x01"
	highlightClose     = "\x02"
	headlineOptionsSQL = "'StartSel=' || chr(1) || ', StopSel=' || chr(2) || ', MaxWords=28, MinWords=10, MaxFragments=1, ShortWord=2'"
)

// SearchQuery — параметры поиска. Отбор по типу, классу, отделу и периоду — те же, что у каталога.
type SearchQuery struct {
	// Text — запрос человека: слова, «фраза в кавычках», -исключённое слово, or.
	Text string
	ListQuery
}

// SnippetPart — кусок сниппета; Match — слово запроса. Разметки нет: подсветку рисует интерфейс по этим кускам.
type SnippetPart struct {
	Text  string `json:"text"`
	Match bool   `json:"match,omitempty"`
}

// Snippet — выдержка из одного места документа.
type Snippet struct {
	// Kind — title (название и шифр), meta (сведения досье), block (текст блока).
	Kind string `json:"kind"`
	// BlockID — блок, в котором найдено (для kind=block); по нему интерфейс ведёт к месту в документе.
	BlockID string        `json:"block_id,omitempty"`
	Parts   []SnippetPart `json:"parts"`
}

// SearchHit — документ в выдаче и его выдержки.
type SearchHit struct {
	Item     Item      `json:"item"`
	Snippets []Snippet `json:"snippets"`
}

// SearchResult — страница выдачи.
type SearchResult struct {
	Items   []SearchHit `json:"items"`
	Total   int64       `json:"total"`
	Page    int         `json:"page"`
	PerPage int         `json:"per_page"`
	Pages   int         `json:"pages"`
	// Query — запрос после очистки пробелов.
	Query string `json:"query"`
	// Ignored — в запросе только слишком частые слова («и», «в», «на»): искать нечего.
	Ignored bool `json:"ignored,omitempty"`
}

func (q *SearchQuery) normalize(v Viewer) error {
	q.Text = strings.Join(strings.Fields(q.Text), " ")
	n := utf8.RuneCountInString(q.Text)
	switch {
	case n < minSearchRunes:
		return queryError("q", "введите хотя бы %d знака", minSearchRunes)
	case n > maxSearchRunes:
		return queryError("q", "запрос слишком длинный (не больше %d знаков)", maxSearchRunes)
	}
	if q.PerPage == 0 {
		q.PerPage = defaultSearchPage
	}
	if q.PerPage < 0 || q.PerPage > maxSearchPerPage {
		return queryError("per_page", "размер страницы — от 1 до %d", maxSearchPerPage)
	}
	// Сортировка в поиске одна — по совпадению; параметр каталога не используется.
	q.Sort = ""
	return q.ListQuery.normalize(v)
}

// Search ищет по названиям, шифрам и тексту блоков.
func (s *Service) Search(ctx context.Context, v Viewer, q SearchQuery) (*SearchResult, error) {
	if err := q.normalize(v); err != nil {
		return nil, err
	}
	res := &SearchResult{Items: []SearchHit{}, Page: q.Page, PerPage: q.PerPage, Query: q.Text}
	db := s.db.WithContext(ctx)

	// Запрос только из частых слов даёт пустое условие: сообщаем об этом, а не «ничего не найдено».
	var nodes int
	// Запрос понижается в регистре здесь же, где и текст при построении индекса: база с локалью C кириллицу не понижает.
	text := lowerText(q.Text)
	if err := db.Raw("SELECT numnode(websearch_to_tsquery('russian', ?))", text).Scan(&nodes).Error; err != nil {
		return nil, err
	}
	slug := ""
	if c, err := ParseCode(q.Text); err == nil {
		slug = c.Slug
	}
	if nodes == 0 && slug == "" {
		res.Ignored = true
		return res, nil
	}

	// Общее условие: что вообще вправе увидеть читатель, и отбор по признакам документа.
	level := v.Level()
	where := []string{"s.level <= ?", "d.level <= ?", "d.code IS NOT NULL"}
	args := []any{level, level}
	if !v.SeesUnpublished() {
		where = append(where, "d.status = ?")
		args = append(args, string(StatusPublished))
	}
	add := func(cond string, arg any) {
		where = append(where, cond)
		args = append(args, arg)
	}
	if q.Type != "" {
		add("d.type = ?", q.Type)
	}
	if q.Class != nil {
		add("d.danger_class = ?", *q.Class)
	}
	if q.YearFrom != nil {
		add("d.composed_year >= ?", *q.YearFrom)
	}
	if q.YearTo != nil {
		add("d.composed_year <= ?", *q.YearTo)
	}
	if q.Department != "" {
		add("d.department = ?", q.Department)
	}
	if q.Category != "" {
		add("d.category = ?", q.Category)
	}
	if q.Containment != "" {
		add("d.containment_status = ?", q.Containment)
	}
	if q.Status != "" {
		add("d.status = ?", q.Status)
	}
	cond := strings.Join(where, " AND ")

	// Совпадение: слова запроса в строке индекса либо шифр документа, введённый в любой раскладке («о-41» → О-041).
	match := "(s.tsv @@ q.tq OR (s.kind = 'title' AND d.slug = ?))"
	score := "CASE WHEN d.slug = ? THEN 1000 ELSE ts_rank_cd(s.tsv, q.tq) END"
	const withQuery = "WITH q AS (SELECT websearch_to_tsquery('russian', ?) AS tq) "
	from := " FROM document_search s JOIN documents d ON d.id = s.document_id CROSS JOIN q WHERE " + match + " AND " + cond

	var total int64
	err := db.Raw(withQuery+"SELECT count(DISTINCT d.id)"+from, append([]any{text, slug}, args...)...).Scan(&total).Error
	if err != nil {
		return nil, err
	}
	res.Total = total
	res.Pages = int((total + int64(q.PerPage) - 1) / int64(q.PerPage))
	if total == 0 {
		return res, nil
	}

	var ids []int64
	rankSQL := withQuery + "SELECT r.id FROM (SELECT d.id, d.code, MAX(" + score + ") AS score" + from +
		" GROUP BY d.id, d.code) r ORDER BY r.score DESC, r.code COLLATE kupol_natural, r.id LIMIT ? OFFSET ?"
	// Параметры — в порядке появления в запросе: текст, слаг (счёт), слаг (совпадение), условия, страница.
	rankArgs := append([]any{text, slug, slug}, args...)
	rankArgs = append(rankArgs, q.PerPage, (q.Page-1)*q.PerPage)
	if err := db.Raw(rankSQL, rankArgs...).Scan(&ids).Error; err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return res, nil
	}

	// Документы страницы (без блоков: они тяжёлые и выдаче не нужны).
	var docs []Document
	if err := db.Select(listColumns).Where("id IN ?", ids).Find(&docs).Error; err != nil {
		return nil, err
	}
	byID := make(map[int64]*Document, len(docs))
	for i := range docs {
		byID[docs[i].ID] = &docs[i]
	}

	// Выдержки: лучшие совпавшие строки каждого документа — снова только допустимые читателю.
	type snipRow struct {
		DocumentID int64
		Kind       string
		BlockID    string
		Body       string
		Headline   string
	}
	var rows []snipRow
	// Выдержка берётся из строчной копии (norm): по ней построен tsvector. Регистр потом возвращается по исходному тексту.
	snipSQL := withQuery + "SELECT document_id, kind, block_id, body, ts_headline('russian', norm, tq, " + headlineOptionsSQL + ") AS headline FROM (" +
		"SELECT s.document_id, s.kind, s.block_id, s.body, s.norm, s.ord, q.tq, ROW_NUMBER() OVER (PARTITION BY s.document_id ORDER BY ts_rank_cd(s.tsv, q.tq) DESC, s.ord) AS n" +
		" FROM document_search s JOIN documents d ON d.id = s.document_id CROSS JOIN q" +
		" WHERE s.tsv @@ q.tq AND s.document_id IN ? AND " + cond + ") r WHERE n <= ? ORDER BY document_id, n"
	snipArgs := append([]any{text, ids}, args...)
	snipArgs = append(snipArgs, snippetsPerHit)
	if err := db.Raw(snipSQL, snipArgs...).Scan(&rows).Error; err != nil {
		return nil, err
	}
	snippets := map[int64][]Snippet{}
	for _, r := range rows {
		snippets[r.DocumentID] = append(snippets[r.DocumentID], Snippet{Kind: r.Kind, BlockID: r.BlockID, Parts: snippetParts(r.Headline, r.Body)})
	}

	for _, id := range ids {
		d, ok := byID[id]
		if !ok {
			continue
		}
		hit := SearchHit{Item: itemFrom(d, v), Snippets: snippets[id]}
		if hit.Snippets == nil {
			hit.Snippets = []Snippet{}
		}
		res.Items = append(res.Items, hit)
	}
	return res, nil
}

// snippetParts превращает выдержку ts_headline (по строчной копии, с метками \x01…\x02) в куски исходного текста:
// выдержка находится в копии, те же позиции берутся из исходного текста, и метки расставляются заново. Так в сниппете
// сохраняются заглавные буквы. lowerText не меняет число знаков, поэтому позиции совпадают; если выдержка вдруг не нашлась
// (не должно случаться), возвращается как есть.
func snippetParts(headline, body string) []SnippetPart {
	var plain strings.Builder
	type span struct{ from, to int } // в знаках выдержки
	var spans []span
	runes := 0
	openAt := -1
	for _, r := range headline {
		switch string(r) {
		case highlightOpen:
			openAt = runes
		case highlightClose:
			if openAt >= 0 {
				spans = append(spans, span{openAt, runes})
				openAt = -1
			}
		default:
			plain.WriteRune(r)
			runes++
		}
	}
	if openAt >= 0 {
		spans = append(spans, span{openAt, runes})
	}
	norm := lowerText(body)
	idx := strings.Index(norm, plain.String())
	if idx < 0 {
		return splitHighlight(headline)
	}
	start := utf8.RuneCountInString(norm[:idx])
	orig := []rune(body)
	if start+runes > len(orig) {
		return splitHighlight(headline)
	}
	frag := orig[start : start+runes]
	var parts []SnippetPart
	pos := 0
	for _, sp := range spans {
		if sp.from > pos {
			parts = append(parts, SnippetPart{Text: string(frag[pos:sp.from])})
		}
		parts = append(parts, SnippetPart{Text: string(frag[sp.from:sp.to]), Match: true})
		pos = sp.to
	}
	if pos < len(frag) {
		parts = append(parts, SnippetPart{Text: string(frag[pos:])})
	}
	return parts
}

// splitHighlight разбирает результат ts_headline с метками \x01…\x02 на куски (без возврата регистра).
func splitHighlight(s string) []SnippetPart {
	var parts []SnippetPart
	for s != "" {
		open := strings.Index(s, highlightOpen)
		if open < 0 {
			parts = append(parts, SnippetPart{Text: s})
			break
		}
		if open > 0 {
			parts = append(parts, SnippetPart{Text: s[:open]})
		}
		s = s[open+len(highlightOpen):]
		end := strings.Index(s, highlightClose)
		if end < 0 {
			parts = append(parts, SnippetPart{Text: s, Match: true})
			break
		}
		parts = append(parts, SnippetPart{Text: s[:end], Match: true})
		s = s[end+len(highlightClose):]
	}
	return parts
}
