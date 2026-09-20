package documents

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// ---------------------------------------------------------------- помощники

func viewerAt(level int) Viewer {
	switch level {
	case 0:
		return Guest
	case 7:
		return Viewer{UserID: 1000, Directorate: true}
	}
	return Viewer{UserID: int64(1000 + level), UserLevel: level}
}

func (e *env) search(v Viewer, q string, mutate ...func(*SearchQuery)) *SearchResult {
	e.t.Helper()
	sq := SearchQuery{Text: q}
	for _, m := range mutate {
		m(&sq)
	}
	res, err := e.svc.Search(ctx, v, sq)
	if err != nil {
		e.t.Fatalf("Search(%q): %v", q, err)
	}
	return res
}

func codesOf(r *SearchResult) []string {
	out := make([]string, len(r.Items))
	for i, h := range r.Items {
		out[i] = h.Item.Code
	}
	return out
}

func snippetText(h SearchHit) string {
	var b strings.Builder
	for _, s := range h.Snippets {
		for _, p := range s.Parts {
			b.WriteString(p.Text)
		}
		b.WriteByte('\n')
	}
	return b.String()
}

func para(id string, level *int, runs ...string) string {
	// runs: «текст» или «текст@N» — фрагмент с уровнем N
	var parts []string
	for _, r := range runs {
		text, lvl, ok := strings.Cut(r, "@")
		if ok {
			parts = append(parts, fmt.Sprintf(`{"text":%q,"level":%s}`, text, lvl))
		} else {
			parts = append(parts, fmt.Sprintf(`{"text":%q}`, text))
		}
	}
	l := ""
	if level != nil {
		l = fmt.Sprintf(`,"level":%d`, *level)
	}
	return fmt.Sprintf(`{"id":%q,"type":"paragraph"%s,"data":{"text":[%s]}}`, id, l, strings.Join(parts, ","))
}

func blocks(bs ...string) string { return `,"blocks":[` + strings.Join(bs, ",") + `]` }

// ---------------------------------------------------------------- извлечение текста

func TestSearchRowsSplitTextByLevel(t *testing.T) {
	lvl2 := 2
	d := &Document{ID: 1, Title: "Название", Code: strPtr("О-041"), Level: 0, Blocks: BlockList{
		mustBlock(t, para("p1", nil, "открытое начало ", "ТАЙНА@4", " открытый конец")),
		mustBlock(t, para("p2", &lvl2, "блок второго уровня ", "ТАЙНА@5")),
		mustBlock(t, `{"id":"l","type":"doc_link","data":{"code":"О-9","note":"примечание автора"}}`),
	}}
	rows := searchRows(d)
	got := map[string]string{}
	for _, r := range rows {
		got[fmt.Sprintf("%s/%s/%d", r.Kind, r.BlockID, r.Level)] = r.Body
	}
	want := map[string]string{
		"title//0":   "Название О-041",
		"block/p1/0": "открытое начало открытый конец", // соседние открытые фрагменты через закрытый — с пробелом
		"block/p1/4": "ТАЙНА",
		"block/p2/2": "блок второго уровня",
		"block/p2/5": "ТАЙНА",
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("строка %s: %q, ожидалось %q", k, got[k], v)
		}
	}
	if len(got) != len(want) {
		t.Errorf("лишние строки индекса: %v", got)
	}
	for _, r := range rows {
		if strings.Contains(r.Body, "примечание") {
			t.Error("примечание ссылки попало в индекс")
		}
	}
}

func strPtr(s string) *string { return &s }

func mustBlock(t *testing.T, raw string) Block {
	t.Helper()
	var b Block
	if err := json.Unmarshal([]byte(raw), &b); err != nil {
		t.Fatal(err)
	}
	return b
}

// Каждый вид блока либо даёт текст в индекс, либо явно исключён с причиной: новый вид не пройдёт мимо.
func TestSearchIndexCoversEveryBlockKind(t *testing.T) {
	seen := map[string]bool{}
	for _, ib := range fixtureBlocks(t) {
		seen[ib.Type] = true
		b := Block{ID: ib.ID, Type: ib.Type, Level: ib.Level, Data: ib.Data}
		pieces, ok := blockPieces(b)
		_, excluded := searchExcluded[ib.Type]
		switch {
		case excluded && ok:
			t.Errorf("блок «%s» исключён из индекса, но извлекается", ib.Type)
		case !excluded && !ok:
			t.Errorf("блок «%s» не индексируется и не в списке исключённых", ib.Type)
		case ok:
			empty := true
			for _, sb := range pieces.byLevel {
				if strings.TrimSpace(sb.String()) != "" {
					empty = false
				}
			}
			if empty {
				t.Errorf("блок «%s» из образца не дал ни одного слова", ib.Type)
			}
		}
	}
	for kind := range kinds {
		if !seen[kind] {
			t.Errorf("в образце нет блока «%s»", kind)
		}
	}
	for kind := range searchExcluded {
		if _, ok := kinds[kind]; !ok {
			t.Errorf("в списке исключённых неизвестный вид «%s»", kind)
		}
	}
}

func TestCleanForIndexRemovesControlAndHighlightMarkers(t *testing.T) {
	if got := cleanForIndex("а\x01б\x02в\tг\n\nд   е"); got != "а б в г д е" {
		t.Errorf("cleanForIndex: %q", got)
	}
}

func TestSnippetPartsRestoreOriginalCase(t *testing.T) {
	body := "Дежурный ВИДЕЛ Тень у Станции Шесть"
	norm := lowerText(body)
	// ts_headline по строчной копии: выдержка «…видел тень у станции…» с метками вокруг «видел» и «станции»
	headline := "\x01видел\x02 тень у \x01станции\x02"
	if !strings.Contains(norm, "видел тень у станции") {
		t.Fatal("подготовка")
	}
	parts := snippetParts(headline, body)
	want := []SnippetPart{{Text: "ВИДЕЛ", Match: true}, {Text: " Тень у "}, {Text: "Станции", Match: true}}
	if fmt.Sprint(parts) != fmt.Sprint(want) {
		t.Errorf("куски: %+v", parts)
	}
	// выдержка не нашлась — возвращается как есть, а не теряется
	if p := snippetParts("\x01чужое\x02", body); len(p) != 1 || p[0].Text != "чужое" || !p[0].Match {
		t.Errorf("запасной путь: %+v", p)
	}
	// метка без закрытия захватывает остаток
	if p := snippetParts("тень \x01у станции", body); len(p) != 2 || !p[1].Match || p[1].Text != "у Станции" {
		t.Errorf("метка без закрытия: %+v", p)
	}
	// нижний регистр не меняет число знаков (от этого зависят позиции)
	for _, s := range []string{"İstanbul", "ЁЖИК", "Ǆ", "ẞ"} {
		if len([]rune(lowerText(s))) != len([]rune(s)) {
			t.Errorf("lowerText(%q) меняет число знаков", s)
		}
	}
}

func TestSplitHighlight(t *testing.T) {
	parts := splitHighlight("до \x01слово\x02 и \x01ещё\x02")
	want := []SnippetPart{{Text: "до "}, {Text: "слово", Match: true}, {Text: " и "}, {Text: "ещё", Match: true}}
	if fmt.Sprint(parts) != fmt.Sprint(want) {
		t.Errorf("куски: %v", parts)
	}
	if len(splitHighlight("")) != 0 {
		t.Error("пустая строка даёт куски")
	}
	if p := splitHighlight("хвост \x01без конца"); len(p) != 2 || !p[1].Match || p[1].Text != "без конца" {
		t.Errorf("метка без закрытия: %v", p)
	}
}

// ---------------------------------------------------------------- безопасность

// Слово с меткой уровня стоит в блоке этого уровня и во фрагменте этого уровня. Читатель уровня L находит ровно то, что ≤ L,
// и ни один сниппет не содержит метки выше его допуска.
func TestSearchNeverRevealsAboveViewerLevel(t *testing.T) {
	e := newEnv(t)
	lvl := func(n int) *int { return &n }

	// документ на каждый уровень допуска; внутри — блоки и фрагменты разных уровней вокруг открытого слова «общеедело»
	for level := 0; level <= 6; level++ {
		var bs []string
		for bl := 0; bl <= 7; bl++ {
			bs = append(bs, para(fmt.Sprintf("b%d", bl), lvl(bl), fmt.Sprintf("общеедело blk%dmark", bl)))
		}
		for rl := 0; rl <= 7; rl++ {
			bs = append(bs, para(fmt.Sprintf("r%d", rl), nil, "общеедело открытое ", fmt.Sprintf("run%dmark@%d", rl, rl), " хвост"))
		}
		e.imp(obj(fmt.Sprintf("О-%d", 500+level), fmt.Sprintf("Документ уровня %d doc%dmark", level, level), "published", level, blocks(bs...)))
	}
	// неопубликованное видит только Директорат
	e.imp(obj("О-600", "Черновик draftmark", "draft", 0, blocks(para("d", nil, "общеедело draftbody"))))
	// в примечании ссылки и в шифре ссылки слов для поиска нет
	e.imp(obj("О-601", "Со ссылкой", "published", 0, blocks(`{"id":"l","type":"doc_link","data":{"code":"О-500","note":"linknotemark"}}`)))

	for viewerLevel := 0; viewerLevel <= 7; viewerLevel++ {
		v := viewerAt(viewerLevel)
		visibleDocs := 0
		for level := 0; level <= 6; level++ {
			if level <= v.Level() {
				visibleDocs++
			}
		}

		// открытое слово находит ровно доступные документы; счёт совпадает с выдачей
		general := e.search(v, "общеедело")
		wantDocs := visibleDocs
		if v.Directorate {
			wantDocs++ // черновик
		}
		if int(general.Total) != wantDocs || len(general.Items) != wantDocs {
			t.Errorf("уровень %d: «общеедело» нашло %d (в выдаче %d), ожидалось %d: %v", viewerLevel, general.Total, len(general.Items), wantDocs, codesOf(general))
		}
		// сниппеты и названия ни в одном документе не содержат метки выше допуска
		for _, h := range general.Items {
			text := snippetText(h) + h.Item.Title
			for m := v.Level() + 1; m <= 7; m++ {
				for _, kind := range []string{"blk", "run"} {
					if strings.Contains(text, fmt.Sprintf("%s%dmark", kind, m)) {
						t.Errorf("уровень %d: в выдаче по «%s» метка уровня %d: %q", viewerLevel, h.Item.Code, m, text)
					}
				}
			}
		}

		for mark := 0; mark <= 7; mark++ {
			for _, kind := range []string{"blk", "run"} {
				res := e.search(v, fmt.Sprintf("%s%dmark", kind, mark))
				// метка mark стоит в каждом документе допустимого уровня (кроме черновика): столько документов и ожидается
				want := 0
				if mark <= v.Level() {
					want = visibleDocs
				}
				if int(res.Total) != want || len(res.Items) != want {
					t.Errorf("уровень %d, «%s%dmark»: найдено %d/%d, ожидалось %d", viewerLevel, kind, mark, res.Total, len(res.Items), want)
				}
			}
			// метка в названии документа: видна только тому, кто вправе видеть документ
			res := e.search(v, fmt.Sprintf("doc%dmark", mark))
			want := 0
			if mark <= 6 && mark <= v.Level() {
				want = 1
			}
			if int(res.Total) != want {
				t.Errorf("уровень %d, название doc%dmark: найдено %d, ожидалось %d", viewerLevel, mark, res.Total, want)
			}
		}

		if res := e.search(v, "draftmark"); (res.Total == 1) != v.Directorate {
			t.Errorf("уровень %d: черновик найден=%v, видит неопубликованное=%v", viewerLevel, res.Total == 1, v.Directorate)
		}
		if res := e.search(v, "linknotemark"); res.Total != 0 {
			t.Errorf("уровень %d: примечание ссылки найдено", viewerLevel)
		}
	}
}

// ---------------------------------------------------------------- поведение

func TestSearchMorphologySnippetsAndRanking(t *testing.T) {
	e := newEnv(t)
	e.imp(obj("О-101", "Сотрудники станции", "published", 0, blocks(para("a", nil, "Обычный текст без нужного слова."))))
	e.imp(obj("О-102", "Отчёт", "published", 0, blocks(para("a", nil, "Один из сотрудников видел "), para("b", nil, "тень у станции."))))
	e.imp(obj("О-103", "Прочее", "published", 0, blocks(para("a", nil, "Ничего общего."))))

	res := e.search(Guest, "сотрудник")
	// морфология: «сотрудники» и «сотрудников» находятся по «сотрудник»; название весомее текста
	if got := codesOf(res); len(got) != 2 || got[0] != "О-101" || got[1] != "О-102" {
		t.Fatalf("выдача %v, ожидалось [О-101 О-102]", got)
	}
	first := res.Items[0]
	if len(first.Snippets) == 0 || first.Snippets[0].Kind != "title" {
		t.Errorf("первый сниппет названия: %+v", first.Snippets)
	}
	matched := false
	for _, p := range first.Snippets[0].Parts {
		if p.Match && strings.HasPrefix(p.Text, "Сотрудник") { // регистр исходного текста сохранён
			matched = true
		}
	}
	if !matched {
		t.Errorf("совпавшее слово не отмечено: %+v", first.Snippets[0].Parts)
	}
	second := res.Items[1]
	if len(second.Snippets) != 1 || second.Snippets[0].Kind != "block" || second.Snippets[0].BlockID != "a" || !strings.Contains(snippetText(second), "сотрудников") {
		t.Errorf("сниппет блока: %+v", second.Snippets)
	}
	// в сниппете нет служебных меток
	for _, h := range res.Items {
		if strings.ContainsAny(snippetText(h), "\x01\x02") {
			t.Error("в сниппете остались служебные метки")
		}
	}

	// фраза, исключение, «или»
	if got := codesOf(e.search(Guest, `"видел тень"`)); len(got) != 0 {
		t.Errorf("фраза через границу блоков: %v", got) // «видел» и «тень» в разных блоках
	}
	if got := codesOf(e.search(Guest, `"сотрудников видел"`)); len(got) != 1 || got[0] != "О-102" {
		t.Errorf("фраза: %v", got)
	}
	// слова запроса ищутся в одном месте (блок, название): «станции -тень» исключает блок с «тенью», а не весь документ
	if got := codesOf(e.search(Guest, "станции -тень")); len(got) != 1 || got[0] != "О-101" {
		t.Errorf("исключение: %v", got)
	}
	if got := codesOf(e.search(Guest, "нужного or общего")); len(got) != 2 {
		t.Errorf("or: %v", got)
	}
}

func TestSearchByCodeInAnyLayout(t *testing.T) {
	e := newEnv(t)
	e.imp(obj("О-041", "Нечто", "published", 0, blocks(para("a", nil, "текст"))))
	e.imp(obj("О-042", "Другое", "published", 0, blocks(para("a", nil, "см. О-041 в тексте"))))
	for _, q := range []string{"О-041", "о-41", "O-041", "o-41"} {
		got := codesOf(e.search(Guest, q))
		if len(got) == 0 || got[0] != "О-041" {
			t.Errorf("запрос %q: %v, документ О-041 должен быть первым", q, got)
		}
	}
	// закрытый документ по шифру тоже не находится
	e.imp(obj("О-043", "Закрытое", "published", 4, blocks(para("a", nil, "текст"))))
	if got := codesOf(e.search(Guest, "О-043")); len(got) != 0 {
		t.Errorf("закрытый документ найден по шифру: %v", got)
	}
	if got := codesOf(e.search(viewerAt(4), "О-043")); len(got) != 1 {
		t.Errorf("допущенный читатель не нашёл по шифру: %v", got)
	}
}

func TestSearchFiltersPaginationAndValidation(t *testing.T) {
	e := newEnv(t)
	for i := 1; i <= 7; i++ {
		typ, year := "memo", 1979
		code := fmt.Sprintf("МЕМО-%d", i)
		extra := blocks(para("a", nil, "общее слово фильтра"))
		if i%2 == 0 {
			typ, year, code = "incident", 1982, fmt.Sprintf("ИНЦ-1982-%02d", i)
		}
		e.imp(fmt.Sprintf(`{"code":%q,"type":%q,"title":"Документ %d","status":"published","composed":{"year":%d}%s}`, code, typ, i, year, extra))
	}
	if r := e.search(Guest, "фильтра"); r.Total != 7 || r.Pages != 1 {
		t.Fatalf("всего %d, страниц %d", r.Total, r.Pages)
	}
	if r := e.search(Guest, "фильтра", func(q *SearchQuery) { q.Type = "incident" }); r.Total != 3 {
		t.Errorf("тип: %d", r.Total)
	}
	if r := e.search(Guest, "фильтра", func(q *SearchQuery) { y := 1980; q.YearFrom = &y }); r.Total != 3 {
		t.Errorf("год от: %d", r.Total)
	}
	if r := e.search(Guest, "фильтра", func(q *SearchQuery) { y := 1980; q.YearTo = &y }); r.Total != 4 {
		t.Errorf("год до: %d", r.Total)
	}
	// страницы: 7 документов по 3 — три страницы, без повторов и пропусков
	seen := map[string]bool{}
	for page := 1; page <= 3; page++ {
		r := e.search(Guest, "фильтра", func(q *SearchQuery) { q.PerPage = 3; q.Page = page })
		if r.Pages != 3 || r.Total != 7 {
			t.Fatalf("страница %d: pages=%d total=%d", page, r.Pages, r.Total)
		}
		for _, c := range codesOf(r) {
			if seen[c] {
				t.Errorf("документ %s на двух страницах", c)
			}
			seen[c] = true
		}
	}
	if len(seen) != 7 {
		t.Errorf("по страницам получено %d из 7", len(seen))
	}
	if r := e.search(Guest, "фильтра", func(q *SearchQuery) { q.Page = 9 }); len(r.Items) != 0 || r.Total != 7 {
		t.Errorf("страница за концом: %+v", r)
	}

	// запросы, которые не ищутся
	if r := e.search(Guest, "и в на"); !r.Ignored || r.Total != 0 {
		t.Errorf("только частые слова: %+v", r)
	}
	if r := e.search(Guest, "ничегонетакого"); r.Total != 0 || r.Ignored {
		t.Errorf("нет совпадений: %+v", r)
	}
	for name, q := range map[string]SearchQuery{
		"пусто": {Text: "  "}, "один знак": {Text: "а"}, "длинный": {Text: strings.Repeat("слово ", 50)},
		"страница": {Text: "слово", ListQuery: ListQuery{PerPage: 500}}, "тип": {Text: "слово", ListQuery: ListQuery{Type: "nope"}},
		"статус читателю": {Text: "слово", ListQuery: ListQuery{Status: "draft"}},
	} {
		var qe *QueryError
		if _, err := e.svc.Search(ctx, Guest, q); err == nil || !asQueryError(err, &qe) {
			t.Errorf("%s: ожидалась QueryError, получено %v", name, err)
		}
	}
	// служебные символы запроса не ломают его: «websearch» их допускает
	for _, q := range []string{`"`, `-`, `'; DROP TABLE documents; --`, `слово & | ! ( )`, `:*`} {
		if _, err := e.svc.Search(ctx, Guest, SearchQuery{Text: q + " слово"}); err != nil {
			t.Errorf("запрос %q: %v", q, err)
		}
	}
}

func asQueryError(err error, target **QueryError) bool {
	qe, ok := err.(*QueryError)
	*target = qe
	return ok
}

// ---------------------------------------------------------------- сопровождение индекса

func TestSearchIndexFollowsEveryWrite(t *testing.T) {
	e := newEnv(t)
	a := e.actors()

	// создание в редакторе: индекс есть сразу
	d := e.objectDraft(a.owner, "Черновик индекса", paragraph("b1", "первоеслово"))
	if got := codesOf(e.search(viewerAt(7), "первоеслово")); len(got) != 0 { // без шифра документ в выдачу не идёт
		t.Errorf("документ без шифра в выдаче: %v", got)
	}
	if n := e.count("SELECT count(*) FROM document_search WHERE document_id = ?", d.ID); n < 2 {
		t.Errorf("строк индекса после создания: %d", n)
	}

	// сохранение: старое слово уходит, новое находится
	res := mustSave(t, e, a.owner, d, memoContent("Черновик индекса", paragraph("b1", "второеслово")))
	d = res.Document
	if n := e.count("SELECT count(*) FROM document_search WHERE document_id = ? AND body LIKE '%первоеслово%'", d.ID); n != 0 {
		t.Error("старый текст остался в индексе после сохранения")
	}
	if n := e.count("SELECT count(*) FROM document_search WHERE document_id = ? AND body LIKE '%второеслово%'", d.ID); n != 1 {
		t.Error("новый текст не попал в индекс")
	}

	// публикация присваивает шифр: документ находится по нему
	d = e.submit(a.owner, d)
	d = e.decide(a.editor, d, VerdictApprove, "")
	if d.Code == nil {
		t.Fatal("шифр не присвоен")
	}
	if got := codesOf(e.search(Guest, *d.Code)); len(got) != 1 || got[0] != *d.Code {
		t.Errorf("поиск по присвоенному шифру: %v", got)
	}
	if got := codesOf(e.search(Guest, "второеслово")); len(got) != 1 {
		t.Errorf("поиск по тексту опубликованного: %v", got)
	}

	// архив скрывает документ от читателей, не трогая индекс
	e.setStatus(d, StatusArchived)
	if got := codesOf(e.search(Guest, "второеслово")); len(got) != 0 {
		t.Errorf("архивный документ в выдаче гостя: %v", got)
	}
	if got := codesOf(e.search(viewerAt(7), "второеслово")); len(got) != 1 {
		t.Errorf("Директорат не видит архивный: %v", got)
	}

	// удаление убирает строки
	if err := e.svc.Delete(ctx, *d.Code); err != nil {
		t.Fatal(err)
	}
	if n := e.count("SELECT count(*) FROM document_search WHERE document_id = ?", d.ID); n != 0 {
		t.Errorf("после удаления документа в индексе %d строк", n)
	}
}

func TestEnsureSearchIndexRebuildsOnlyWhenNeeded(t *testing.T) {
	e := newEnv(t)
	e.imp(obj("О-201", "Раз", "published", 0, blocks(para("a", nil, "словоодин"))))
	e.imp(obj("О-202", "Два", "published", 3, blocks(para("a", nil, "открытое ", "словодва@5"))))

	// документ, появившийся «до поиска»: строк нет, версии нет
	if err := e.db.Exec("DELETE FROM document_search").Error; err != nil {
		t.Fatal(err)
	}
	if err := e.db.Exec("DELETE FROM document_search_state").Error; err != nil {
		t.Fatal(err)
	}
	if got := codesOf(e.search(Guest, "словоодин")); len(got) != 0 {
		t.Fatalf("без индекса нашлось: %v", got)
	}
	if err := e.svc.EnsureSearchIndex(ctx); err != nil {
		t.Fatal(err)
	}
	if got := codesOf(e.search(Guest, "словоодин")); len(got) != 1 {
		t.Errorf("после построения: %v", got)
	}
	if got := codesOf(e.search(viewerAt(4), "словодва")); len(got) != 0 { // уровень фрагмента 5 при допуске 4
		t.Errorf("закрытый фрагмент найден после перестройки: %v", got)
	}
	if got := codesOf(e.search(viewerAt(5), "словодва")); len(got) != 1 {
		t.Errorf("допущенный не нашёл после перестройки: %v", got)
	}
	if v := e.count("SELECT version FROM document_search_state"); v != searchIndexVersion {
		t.Errorf("версия %d", v)
	}

	// та же версия — ничего не перестраивается (id строк остаются)
	before := e.count("SELECT min(id) FROM document_search")
	if err := e.svc.EnsureSearchIndex(ctx); err != nil {
		t.Fatal(err)
	}
	if after := e.count("SELECT min(id) FROM document_search"); after != before {
		t.Error("индекс перестроен при той же версии")
	}
	// другая версия — перестраивается
	if err := e.db.Exec("UPDATE document_search_state SET version = 0").Error; err != nil {
		t.Fatal(err)
	}
	if err := e.svc.EnsureSearchIndex(ctx); err != nil {
		t.Fatal(err)
	}
	if after := e.count("SELECT min(id) FROM document_search"); after == before {
		t.Error("индекс не перестроен при другой версии")
	}
	n, err := e.svc.ReindexAll(ctx)
	if err != nil || n != 2 {
		t.Errorf("ReindexAll: %d, %v", n, err)
	}
}
