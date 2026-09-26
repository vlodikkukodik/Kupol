package documents

import (
	"testing"

	"kupol/internal/i18n"
)

// TestTranslationShownByViewerLanguage — итальянская версия показывается читателю с it, русская — с ru.
func TestTranslationShownByViewerLanguage(t *testing.T) {
	e := newEnv(t)
	e.imp(`{"code":"О-1","type":"object","title":"Гамма","status":"published","level":0,"composed":{"year":1981},
		"props":{"danger_class":2,"category":"entity","containment_status":"contained"},
		"blocks":[{"id":"p1","type":"paragraph","data":{"text":"Русский текст"}}],
		"it":{"title":"Gamma","blocks":[{"id":"p1","type":"paragraph","data":{"text":"Testo italiano"}}]}}`)

	ru, err := e.svc.Get(ctx, Viewer{Lang: i18n.RU}, "О-1")
	if err != nil {
		t.Fatal(err)
	}
	if ru.Title != "Гамма" || !ru.HasTranslation {
		t.Fatalf("ru: заголовок %q, has_translation %v", ru.Title, ru.HasTranslation)
	}

	it, err := e.svc.Get(ctx, Viewer{Lang: i18n.IT}, "О-1")
	if err != nil {
		t.Fatal(err)
	}
	if it.Title != "Gamma" || !it.HasTranslation {
		t.Fatalf("it: заголовок %q, has_translation %v", it.Title, it.HasTranslation)
	}
}

// TestTranslationFallsBackWhenMissing — нет перевода — читатель с it видит русскую версию.
func TestTranslationFallsBackWhenMissing(t *testing.T) {
	e := newEnv(t)
	e.imp(`{"code":"О-1","type":"object","title":"Гамма","status":"published","level":0,"composed":{"year":1981},
		"props":{"danger_class":2,"category":"entity","containment_status":"contained"},
		"blocks":[{"id":"p1","type":"paragraph","data":{"text":"Русский текст"}}]}`)

	it, err := e.svc.Get(ctx, Viewer{Lang: i18n.IT}, "О-1")
	if err != nil {
		t.Fatal(err)
	}
	if it.Title != "Гамма" || it.HasTranslation {
		t.Fatalf("it fallback: заголовок %q, has_translation %v", it.Title, it.HasTranslation)
	}
}

// TestTranslationIsSearchableRegardlessOfQueryLanguage — документ с итальянским переводом находится
// и по русскому, и по итальянскому тексту, независимо от языка читателя.
func TestTranslationIsSearchableRegardlessOfQueryLanguage(t *testing.T) {
	e := newEnv(t)
	e.imp(`{"code":"О-1","type":"object","title":"Гамма","status":"published","level":0,"composed":{"year":1981},
		"props":{"danger_class":2,"category":"entity","containment_status":"contained"},
		"blocks":[{"id":"p1","type":"paragraph","data":{"text":"Русский текст"}}],
		"it":{"title":"Gamma","blocks":[{"id":"p1","type":"paragraph","data":{"text":"Testo italiano unico"}}]}}`)

	if got := codesOf(e.search(Guest, "Gamma")); len(got) != 1 {
		t.Errorf("поиск по it-заголовку: %v", got)
	}
	if got := codesOf(e.search(Viewer{Lang: i18n.IT}, "Гамма")); len(got) != 1 {
		t.Errorf("поиск по ru-заголовку читателем с it: %v", got)
	}
	if got := codesOf(e.search(Guest, "unico")); len(got) != 1 {
		t.Errorf("поиск по it-тексту блока: %v", got)
	}
}

// TestTranslationHiddenFromCatalogAndSearchWithoutTranslation — в каталоге, ленте и поиске
// читатель с it видит только дела с итальянским переводом; по прямой ссылке (Get) — откат на
// русский, как раньше.
func TestTranslationHiddenFromCatalogAndSearchWithoutTranslation(t *testing.T) {
	e := newEnv(t)
	e.imp(`{"code":"О-1","type":"object","title":"Без перевода","status":"published","level":0,"composed":{"year":1981},
		"props":{"danger_class":2,"category":"entity","containment_status":"contained"},
		"blocks":[{"id":"p1","type":"paragraph","data":{"text":"Только по-русски"}}]}`)
	e.imp(`{"code":"О-2","type":"object","title":"С переводом","status":"published","level":0,"composed":{"year":1981},
		"props":{"danger_class":2,"category":"entity","containment_status":"contained"},
		"blocks":[{"id":"p1","type":"paragraph","data":{"text":"Текст"}}],
		"it":{"title":"Con traduzione","blocks":[{"id":"p1","type":"paragraph","data":{"text":"Testo"}}]}}`)

	itViewer := Viewer{Lang: i18n.IT}
	ruViewer := Viewer{Lang: i18n.RU}

	// каталог
	if got := codes(e.list(itViewer, ListQuery{}).Items); got != "О-002" {
		t.Errorf("каталог (it): %q, ожидалось только О-002", got)
	}
	if got := codes(e.list(ruViewer, ListQuery{}).Items); got != "О-001,О-002" {
		t.Errorf("каталог (ru): %q, ожидались оба", got)
	}

	// лента
	recentIT, err := e.svc.Recent(ctx, itViewer, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(recentIT) != 1 || recentIT[0].Code != "О-002" {
		t.Errorf("лента (it): %+v", recentIT)
	}

	// поиск: слово, которое встречается только в русской версии дела без перевода
	if got := codesOf(e.search(itViewer, "русски")); len(got) != 0 {
		t.Errorf("поиск (it) нашёл дело без перевода: %v", got)
	}
	if got := codesOf(e.search(ruViewer, "русски")); len(got) != 1 {
		t.Errorf("поиск (ru) не нашёл дело без перевода: %v", got)
	}
	if got := codesOf(e.search(itViewer, "Con")); len(got) != 1 {
		t.Errorf("поиск (it) не нашёл дело с переводом: %v", got)
	}

	// прямая ссылка — откат на русский, как и раньше
	direct, err := e.svc.Get(ctx, itViewer, "О-1")
	if err != nil {
		t.Fatal(err)
	}
	if direct.Title != "Без перевода" {
		t.Errorf("прямая ссылка (it) на дело без перевода: заголовок %q", direct.Title)
	}
}

// TestTranslationValidation — заголовок и блоки перевода либо оба пусты, либо оба заполнены.
func TestTranslationValidation(t *testing.T) {
	e := newEnv(t)
	err := e.impErr(`{"code":"О-1","type":"object","title":"Гамма","status":"published","level":0,"composed":{"year":1981},
		"props":{"danger_class":2,"category":"entity","containment_status":"contained"},
		"blocks":[{"id":"p1","type":"paragraph","data":{"text":"Русский текст"}}],
		"it":{"title":"","blocks":[{"id":"p1","type":"paragraph","data":{"text":"Testo"}}]}}`)
	if paths := problemPaths(err); len(paths) != 1 || paths[0] != "it.title" {
		t.Fatalf("problemPaths = %v", paths)
	}
}

// TestTranslationRoundTripsThroughTeamEditor — редактор сохраняет и возвращает перевод как есть.
func TestTranslationRoundTripsThroughTeamEditor(t *testing.T) {
	e := newEnv(t)
	e.imp(`{"code":"О-1","type":"object","title":"Гамма","status":"published","level":0,"composed":{"year":1981},
		"props":{"danger_class":2,"category":"entity","containment_status":"contained"},
		"blocks":[{"id":"p1","type":"paragraph","data":{"text":"Русский текст"}}]}`)
	var id int64
	if err := e.db.Raw(`SELECT id FROM documents WHERE code = ?`, "О-001").Scan(&id).Error; err != nil {
		t.Fatal(err)
	}

	a := Actor{UserID: e.user("director"), Directorate: true, CanWrite: true, CanReview: true, CanEditPublished: true}
	tdoc, err := e.svc.TeamGet(ctx, a, id)
	if err != nil {
		t.Fatalf("id=%d err=%v", id, err)
	}
	c := tdoc.Content
	c.IT = &Translation{Title: "Gamma", Blocks: []InputBlock{paragraph("p1", "Testo italiano")}}

	if _, err := e.svc.TeamSave(ctx, a, id, tdoc.Revision, c); err != nil {
		t.Fatal(err)
	}

	tdoc2, err := e.svc.TeamGet(ctx, a, id)
	if err != nil {
		t.Fatal(err)
	}
	if tdoc2.Content.IT == nil || tdoc2.Content.IT.Title != "Gamma" {
		t.Fatalf("content.it = %+v", tdoc2.Content.IT)
	}

	it, err := e.svc.Get(ctx, Viewer{Lang: i18n.IT}, "О-1")
	if err != nil {
		t.Fatal(err)
	}
	if it.Title != "Gamma" {
		t.Fatalf("Get(it).Title = %q", it.Title)
	}
}
