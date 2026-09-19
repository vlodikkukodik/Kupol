package httpapi_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"kupol/internal/documents"
)

const docsPath = "/api/team/documents"

func memoBody(code, title string) map[string]any {
	return map[string]any{
		"type": "memo", "code": code, "title": title, "composed": map[string]any{"year": 1979},
		"blocks": []any{map[string]any{"id": "b1", "type": "paragraph", "data": map[string]any{"text": "Первый абзац."}}},
	}
}

func contentBody(title string, paragraphs ...string) map[string]any {
	blocks := []any{}
	for i, p := range paragraphs {
		blocks = append(blocks, map[string]any{"id": fmt.Sprintf("b%d", i+1), "type": "paragraph", "data": map[string]any{"text": p}})
	}
	return map[string]any{"title": title, "composed": map[string]any{"year": 1979}, "blocks": blocks}
}

func docID(r response) int64 {
	return int64(r.json()["document"].(map[string]any)["id"].(float64))
}

func (a *teamActor) createMemo(t *testing.T, code, title string) int64 {
	t.Helper()
	r := a.client.do("POST", docsPath, memoBody(code, title))
	if r.Code != http.StatusCreated {
		t.Fatalf("создание %s: %d %s", code, r.Code, r.Body)
	}
	return docID(r)
}

func TestTeamDocumentsPermissionMatrixOverHTTP(t *testing.T) {
	_, actors := teamStackWithActors(t)
	id := actors["author"].createMemo(t, "МЕМО-1", "Черновик автора")

	// кто что получает при обращении к чужому черновику и к общим маршрутам
	type want struct{ list, create, get, save, autosave, lock, versions, restore int }
	matrix := map[string]want{
		"guest":     {401, 401, 401, 401, 401, 401, 401, 401},
		"plain":     {403, 403, 403, 403, 403, 403, 403, 403}, // не в команде
		"moderator": {200, 403, 404, 404, 404, 404, 404, 404}, // в команде, но чужого черновика не видит; писать нельзя
		"editor":    {200, 201, 404, 404, 404, 404, 404, 404}, // чужой черновик Редактору не виден
		"author":    {200, 201, 200, 200, 200, 200, 200, 200},
		"director":  {200, 201, 200, 200, 200, 200, 200, 200},
	}
	// порядок фиксирован: замок, взятый одним, мешает следующему, поэтому после каждого его снимает Директорат
	for i, name := range []string{"guest", "plain", "moderator", "editor", "author", "director"} {
		w := matrix[name]
		c := actors[name].client
		vid := int64(1)
		if name == "author" || name == "director" {
			vid = firstVersion(t, actors["author"], id)
		}
		codeFor := fmt.Sprintf("МЕМО-%d", 100+i)
		checks := []struct {
			what   string
			got    int
			expect int
		}{
			{"список", c.do("GET", docsPath, nil).Code, w.list},
			{"создание", c.do("POST", docsPath, memoBody(codeFor, "Создан "+name)).Code, w.create},
			{"чтение", c.do("GET", fmt.Sprintf("%s/%d", docsPath, id), nil).Code, w.get},
			{"сохранение", c.do("PUT", fmt.Sprintf("%s/%d", docsPath, id), map[string]any{"base_revision": currentRevision(t, actors["director"], id), "content": contentBody("x"+name, "y")}).Code, w.save},
			{"автосохранение", c.do("PUT", fmt.Sprintf("%s/%d/draft", docsPath, id), contentBody("x", "y")).Code, w.autosave},
			{"замок", c.do("POST", fmt.Sprintf("%s/%d/lock", docsPath, id), nil).Code, w.lock},
			{"история", c.do("GET", fmt.Sprintf("%s/%d/versions", docsPath, id), nil).Code, w.versions},
			{"откат", c.do("POST", fmt.Sprintf("%s/%d/versions/%d/restore", docsPath, id, vid), nil).Code, w.restore},
		}
		for _, ch := range checks {
			if ch.got != ch.expect {
				t.Errorf("%s: %s = %d, ожидалось %d", name, ch.what, ch.got, ch.expect)
			}
		}
		actors["director"].client.do("DELETE", fmt.Sprintf("%s/%d/lock", docsPath, id), nil)
	}
}

func currentRevision(t *testing.T, a *teamActor, docID int64) int {
	t.Helper()
	r := a.client.do("GET", fmt.Sprintf("%s/%d", docsPath, docID), nil)
	return int(r.json()["document"].(map[string]any)["revision"].(float64))
}

func firstVersion(t *testing.T, a *teamActor, docID int64) int64 {
	t.Helper()
	r := a.client.do("GET", fmt.Sprintf("%s/%d/versions?per_page=100", docsPath, docID), nil)
	items := r.json()["items"].([]any)
	return int64(items[len(items)-1].(map[string]any)["id"].(float64))
}

func TestTeamDocumentLifecycleOverHTTP(t *testing.T) {
	_, actors := teamStackWithActors(t)
	author, director := actors["author"], actors["director"]

	// создание
	r := author.client.do("POST", docsPath, memoBody("МЕМО-7", "Меморандум"))
	if r.Code != 201 {
		t.Fatalf("создание: %d %s", r.Code, r.Body)
	}
	doc := r.json()["document"].(map[string]any)
	id := int64(doc["id"].(float64))
	path := fmt.Sprintf("%s/%d", docsPath, id)
	if doc["status"] != "draft" || doc["revision"].(float64) != 1 || doc["author"] != author.login || doc["can_edit"] != true || doc["code"] != "МЕМО-7" {
		t.Errorf("новый документ: %v", doc)
	}
	if doc["lock"] != nil {
		t.Errorf("новый документ уже взят в работу: %v", doc["lock"])
	}

	// читатели черновика не видят
	if got := actors["guest"].client.do("GET", "/api/documents/MEMO-7", nil).Code; got != 404 {
		t.Errorf("черновик виден читателю: %d", got)
	}

	// сохранение: редакция растёт, замок взят
	save := author.client.do("PUT", path, map[string]any{"base_revision": 1, "content": contentBody("Меморандум, правка", "Абзац 1", "Абзац 2")})
	if save.Code != 200 || save.json()["changed"] != true {
		t.Fatalf("сохранение: %d %s", save.Code, save.Body)
	}
	saved := save.json()["document"].(map[string]any)
	if saved["revision"].(float64) != 2 || saved["content"].(map[string]any)["title"] != "Меморандум, правка" || saved["lock"].(map[string]any)["mine"] != true {
		t.Errorf("после сохранения: %v", saved)
	}

	// устаревшая редакция — конфликт с подсказкой
	conf := author.client.do("PUT", path, map[string]any{"base_revision": 1, "content": contentBody("Устаревшее", "x")})
	if conf.Code != 409 || conf.errCode() != "conflict" || conf.json()["error"].(map[string]any)["current_revision"].(float64) != 2 {
		t.Errorf("конфликт: %d %s", conf.Code, conf.Body)
	}

	// автосохранение неполного содержимого: 200, документ не тронут
	half := map[string]any{"title": "Ещё пишу", "composed": map[string]any{"year": 1979}, "blocks": []any{map[string]any{"id": "b1", "type": "paragraph", "data": map[string]any{"text": ""}}}}
	auto := author.client.do("PUT", path+"/draft", half)
	if auto.Code != 200 || auto.json()["saved"] != true {
		t.Fatalf("автосохранение: %d %s", auto.Code, auto.Body)
	}
	got := author.client.do("GET", path, nil).json()["document"].(map[string]any)
	if got["revision"].(float64) != 2 || got["content"].(map[string]any)["title"] != "Меморандум, правка" {
		t.Errorf("автосохранение изменило документ: %v", got)
	}
	if d := got["draft"].(map[string]any); d["content"].(map[string]any)["title"] != "Ещё пишу" || d["author"] != author.login {
		t.Errorf("несохранённые правки: %v", got["draft"])
	}
	// то же ещё раз — saved=false
	if again := author.client.do("PUT", path+"/draft", half); again.json()["saved"] != false {
		t.Errorf("повтор автосохранения: %s", again.Body)
	}

	// история и снимок
	list := author.client.do("GET", path+"/versions", nil).json()
	items := list["items"].([]any)
	if int(list["total"].(float64)) != 3 || items[0].(map[string]any)["kind"] != "autosave" || items[2].(map[string]any)["kind"] != "create" {
		t.Errorf("история: %v", list)
	}
	createID := int64(items[2].(map[string]any)["id"].(float64))
	v := author.client.do("GET", fmt.Sprintf("%s/versions/%d", path, createID), nil).json()["version"].(map[string]any)
	if v["content"].(map[string]any)["title"] != "Меморандум" {
		t.Errorf("снимок создания: %v", v)
	}
	// разница снимка с текущим
	d := author.client.do("GET", fmt.Sprintf("%s/versions/%d/diff?against=live", path, createID), nil)
	diff := d.json()["diff"].(map[string]any)
	// изменились название, первый абзац и добавлен второй
	if d.Code != 200 || diff["same"] != false || len(diff["fields"].([]any)) != 1 || len(diff["blocks"].([]any)) != 2 {
		t.Errorf("разница: %d %s", d.Code, d.Body)
	}
	if bad := author.client.do("GET", fmt.Sprintf("%s/versions/%d/diff?against=abc", path, createID), nil); bad.Code != 400 {
		t.Errorf("against=abc: %d", bad.Code)
	}

	// замок (его взяло сохранение Автора): другой человек видит, кто правит; править не может
	dirGet := director.client.do("GET", path, nil).json()["document"].(map[string]any)
	if lock := dirGet["lock"].(map[string]any); lock["holder"] != author.login || lock["mine"] != false {
		t.Errorf("замок глазами Директората: %v", lock)
	}
	locked := director.client.do("PUT", path, map[string]any{"base_revision": 2, "content": contentBody("Директорат правит", "x")})
	if locked.Code != 409 || locked.errCode() != "locked" {
		t.Fatalf("сохранение при чужом замке: %d %s", locked.Code, locked.Body)
	}
	if lk := locked.json()["error"].(map[string]any)["lock"].(map[string]any); lk["holder"] != author.login || lk["expires_at"] == "" {
		t.Errorf("подробности замка: %v", lk)
	}
	if take := director.client.do("POST", path+"/lock", nil); take.Code != 409 {
		t.Errorf("взять чужое: %d", take.Code)
	}
	// Директорат снимает чужой замок и берёт документ
	if rel := director.client.do("DELETE", path+"/lock", nil); rel.Code != 204 {
		t.Fatalf("снять чужой замок: %d %s", rel.Code, rel.Body)
	}
	if take := director.client.do("POST", path+"/lock", nil); take.Code != 200 || take.json()["lock"].(map[string]any)["mine"] != true {
		t.Errorf("взять свободное: %d %s", take.Code, take.Body)
	}
	// теперь Автор — чужой; Автор без права снять чужой замок — 403
	if rel := author.client.do("DELETE", path+"/lock", nil); rel.Code != 403 {
		t.Errorf("Автор снимает чужой замок: %d", rel.Code)
	}

	// откат к созданию (Директорат держит замок)
	rest := director.client.do("POST", fmt.Sprintf("%s/versions/%d/restore", path, createID), nil)
	if rest.Code != 200 || rest.json()["changed"] != true || rest.json()["document"].(map[string]any)["revision"].(float64) != 3 {
		t.Fatalf("откат: %d %s", rest.Code, rest.Body)
	}
	if rest.json()["document"].(map[string]any)["content"].(map[string]any)["title"] != "Меморандум" {
		t.Errorf("после отката: %s", rest.Body)
	}
}

func TestTeamDocumentValidationAndBadRequests(t *testing.T) {
	_, actors := teamStackWithActors(t)
	author := actors["author"]
	id := author.createMemo(t, "МЕМО-8", "Меморандум")
	path := fmt.Sprintf("%s/%d", docsPath, id)

	// замечания к содержимому: 422 со списком путей
	bad := map[string]any{"base_revision": 1, "content": map[string]any{
		"title": "", "composed": map[string]any{"year": 1979},
		"blocks": []any{map[string]any{"id": "b1", "type": "heading", "data": map[string]any{"depth": 9, "text": ""}}},
	}}
	r := author.client.do("PUT", path, bad)
	if r.Code != 422 || r.errCode() != "validation" {
		t.Fatalf("%d %s", r.Code, r.Body)
	}
	paths := map[string]bool{}
	for _, p := range r.json()["error"].(map[string]any)["problems"].([]any) {
		paths[p.(map[string]any)["path"].(string)] = true
	}
	for _, want := range []string{"title", "blocks[0].data.depth", "blocks[0].data.text"} {
		if !paths[want] {
			t.Errorf("нет замечания к %q: %v", want, paths)
		}
	}
	if got := author.client.do("GET", path, nil).json()["document"].(map[string]any); got["revision"].(float64) != 1 {
		t.Errorf("отклонённое сохранение изменило документ: %v", got["revision"])
	}

	// создание: занятый шифр, чужие свойства, без шифра
	if dup := author.client.do("POST", docsPath, memoBody("МЕМО-8", "Дубль")); dup.Code != 409 || dup.errCode() != "code_taken" || dup.fields()["code"] == nil {
		t.Errorf("занятый шифр: %d %s", dup.Code, dup.Body)
	}
	noCode := memoBody("", "Без шифра")
	if r := author.client.do("POST", docsPath, noCode); r.Code != 422 {
		t.Errorf("приказ без шифра: %d %s", r.Code, r.Body)
	}
	obj := map[string]any{"type": "object", "title": "Объект без шифра", "composed": map[string]any{"year": 1979}, "blocks": []any{}}
	if r := author.client.do("POST", docsPath, obj); r.Code != 201 || r.json()["document"].(map[string]any)["code"] != nil {
		t.Errorf("объект без шифра: %d %s", r.Code, r.Body)
	}

	// строгий разбор тела: неизвестные поля, лишнее, не тот Content-Type, слишком большое
	if r := author.client.do("PUT", path, map[string]any{"base_revision": 1, "content": contentBody("x", "y"), "status": "published"}); r.Code != 400 {
		t.Errorf("неизвестное поле (status): %d", r.Code)
	}
	if r := author.client.do("PUT", path, map[string]any{"base_revision": 1, "content": map[string]any{"title": "x", "code": "О-1", "composed": map[string]any{"year": 1979}, "blocks": []any{}}}); r.Code != 400 {
		t.Errorf("шифр внутри содержимого должен отвергаться: %d", r.Code)
	}
	raw := func(method, target, ctype, body string) response {
		req := httptest.NewRequest(method, target, strings.NewReader(body))
		req.RemoteAddr = author.client.ip + ":4000"
		req.Header.Set("Origin", testOrigin)
		if ctype != "" {
			req.Header.Set("Content-Type", ctype)
		}
		req.AddCookie(&http.Cookie{Name: "kupol_session", Value: author.client.cookie.Value})
		w := httptest.NewRecorder()
		author.client.st.r.ServeHTTP(w, req)
		return response{w}
	}
	if r := raw("PUT", path, "text/plain", `{}`); r.Code != 415 {
		t.Errorf("Content-Type: %d", r.Code)
	}
	if r := raw("PUT", path+"/draft", "application/json", `{"title":"a"} {"b":1}`); r.Code != 422 {
		t.Errorf("лишнее после JSON в автосохранении: %d %s", r.Code, r.Body)
	}
	if r := raw("PUT", path+"/draft", "application/json", `[1,2]`); r.Code != 422 {
		t.Errorf("массив вместо содержимого: %d", r.Code)
	}
	huge := `{"title":"` + strings.Repeat("я", 600_000) + `"}`
	if r := raw("PUT", path+"/draft", "application/json", huge); r.Code != 413 && r.Code != 422 {
		t.Errorf("слишком большое тело: %d", r.Code)
	}

	// мусор в адресе и в параметрах — 404 или 400, а не 500
	for _, p := range []string{docsPath + "/abc", docsPath + "/0", docsPath + "/-3", docsPath + "/99999999999999999999", docsPath + "/1.5"} {
		if r := author.client.do("GET", p, nil); r.Code != 404 {
			t.Errorf("GET %s = %d", p, r.Code)
		}
	}
	for _, q := range []string{"status=deleted", "type=cat", "mine=2", "page=-1", "page=x", "per_page=101", "q=" + strings.Repeat("я", 101)} {
		if r := author.client.do("GET", docsPath+"?"+q, nil); r.Code != 400 {
			t.Errorf("список ?%s = %d", q, r.Code)
		}
	}
}

func TestTeamDocumentCSRFAndMethods(t *testing.T) {
	_, actors := teamStackWithActors(t)
	author := actors["author"]
	id := author.createMemo(t, "МЕМО-9", "Меморандум")
	path := fmt.Sprintf("%s/%d", docsPath, id)

	evil := *author.client
	evil.origin = "https://evil.example"
	noOrigin := *author.client
	noOrigin.origin = ""
	for name, c := range map[string]*client{"чужой Origin": &evil, "без Origin": &noOrigin} {
		for _, call := range []struct{ method, path string }{
			{"POST", docsPath}, {"PUT", path}, {"PUT", path + "/draft"}, {"POST", path + "/lock"}, {"DELETE", path + "/lock"},
			{"POST", path + "/versions/1/restore"},
		} {
			if r := c.do(call.method, call.path, memoBody("МЕМО-666", "Взлом")); r.Code != 403 || r.errCode() != "forbidden_origin" {
				t.Errorf("%s: %s %s = %d %s", name, call.method, call.path, r.Code, r.errCode())
			}
		}
	}
	if got := author.client.do("GET", path, nil).json()["document"].(map[string]any); got["revision"].(float64) != 1 || got["lock"] != nil {
		t.Errorf("запросы с чужого сайта что-то изменили: %v", got)
	}
	for _, call := range []struct{ method, path string }{
		{"DELETE", path}, {"PATCH", path}, {"POST", path}, {"GET", path + "/lock"}, {"POST", path + "/versions"},
	} {
		if r := author.client.do(call.method, call.path, nil); r.Code != 405 && r.Code != 404 {
			t.Errorf("%s %s: %d", call.method, call.path, r.Code)
		}
	}
}

func TestEditorEditsPublishedInPlaceAndReadersSeeItAtOnce(t *testing.T) {
	st, actors := teamStackWithActors(t)
	author, editor := actors["author"], actors["editor"]
	id := author.createMemo(t, "МЕМО-10", "Опубликованный")
	if _, err := st.docs.SetStatus(context.Background(), "МЕМО-10", documents.StatusPublished); err != nil {
		t.Fatal(err)
	}
	path := fmt.Sprintf("%s/%d", docsPath, id)

	// Автор видит своё, но править опубликованное не может
	if r := author.client.do("GET", path, nil); r.Code != 200 || r.json()["document"].(map[string]any)["can_edit"] != false {
		t.Errorf("Автор читает опубликованное: %d %s", r.Code, r.Body)
	}
	if r := author.client.do("PUT", path, map[string]any{"base_revision": 1, "content": contentBody("Автор правит", "x")}); r.Code != 403 {
		t.Errorf("Автор правит опубликованное: %d", r.Code)
	}
	if r := author.client.do("POST", path+"/lock", nil); r.Code != 403 {
		t.Errorf("Автор берёт опубликованное: %d", r.Code)
	}

	// Редактор правит на месте — читатель видит сразу
	r := editor.client.do("PUT", path, map[string]any{"base_revision": 1, "content": contentBody("Опубликованный, исправлено", "Исправленный текст")})
	if r.Code != 200 || r.json()["changed"] != true {
		t.Fatalf("правка Редактора: %d %s", r.Code, r.Body)
	}
	seen := actors["guest"].client.do("GET", "/api/documents/MEMO-10", nil)
	if seen.Code != 200 || !strings.Contains(seen.Body.String(), "Исправленный текст") || !strings.Contains(seen.Body.String(), "Опубликованный, исправлено") {
		t.Errorf("читатель после правки: %d %s", seen.Code, seen.Body)
	}
	kinds := []string{}
	for _, it := range editor.client.do("GET", path+"/versions", nil).json()["items"].([]any) {
		kinds = append(kinds, it.(map[string]any)["kind"].(string))
	}
	if strings.Join(kinds, ",") != "edit,status,create" {
		t.Errorf("история: %v", kinds)
	}
	// автосохранения на опубликованное читателям не видны
	if r := editor.client.do("PUT", path+"/draft", contentBody("Черновик правки", "Ещё не сохранено")); r.Code != 200 {
		t.Fatalf("автосохранение: %d %s", r.Code, r.Body)
	}
	again := actors["guest"].client.do("GET", "/api/documents/MEMO-10", nil)
	if strings.Contains(again.Body.String(), "Ещё не сохранено") || strings.Contains(again.Body.String(), "Черновик правки") {
		t.Errorf("читатель видит автосохранение: %s", again.Body)
	}
}

func TestDocumentTypesReference(t *testing.T) {
	_, actors := teamStackWithActors(t)
	if r := actors["plain"].client.do("GET", "/api/team/document-types", nil); r.Code != 403 {
		t.Errorf("не член команды: %d", r.Code)
	}
	r := actors["author"].client.do("GET", "/api/team/document-types", nil)
	if r.Code != 200 {
		t.Fatalf("%d %s", r.Code, r.Body)
	}
	m := r.json()
	types := m["types"].([]any)
	if len(types) != len(documents.Types) {
		t.Errorf("типов %d", len(types))
	}
	for _, ti := range types {
		tt := ti.(map[string]any)
		if tt["name"] == "" || tt["code_example"] == "" {
			t.Errorf("тип без названия или образца: %v", tt)
		}
		if (tt["id"] == "object") != (tt["code_optional"] == true) {
			t.Errorf("шифр необязателен только у объекта: %v", tt)
		}
	}
	kinds := m["block_kinds"].([]any)
	if len(kinds) != len(documents.KindNames()) {
		t.Errorf("типов блоков %d", len(kinds))
	}
	for _, k := range kinds {
		if km := k.(map[string]any); km["name"] == km["id"] || km["name"] == "" {
			t.Errorf("у типа блока нет русского названия: %v", km)
		}
	}
	if len(m["statuses"].([]any)) != 4 || len(m["categories"].([]any)) != 3 || len(m["containment"].([]any)) != 4 ||
		len(m["direct_links"].([]any)) != 2 || m["max_level"].(float64) != 7 || m["default_grif"] != "Форма КУПОЛ-1" {
		t.Errorf("справочник: %v", m)
	}
}

func TestTeamDocumentsNeverLeakToOutsiders(t *testing.T) {
	_, actors := teamStackWithActors(t)
	author := actors["author"]
	id := author.createMemo(t, "МЕМО-11", "СЕКРЕТНОЕ-НАЗВАНИЕ-ЧЕРНОВИКА")
	path := fmt.Sprintf("%s/%d", docsPath, id)
	author.client.do("PUT", path+"/draft", contentBody("СЕКРЕТНОЕ-АВТОСОХРАНЕНИЕ", "текст"))

	for _, who := range []string{"guest", "plain", "moderator", "editor", "archivist"} {
		c := actors[who].client
		for _, p := range []string{docsPath, path, path + "/versions", path + "/versions/1", path + "/versions/1/diff"} {
			r := c.do("GET", p, nil)
			if strings.Contains(r.Body.String(), "СЕКРЕТНОЕ") || strings.Contains(r.Body.String(), "МЕМО-11") {
				t.Errorf("%s: %s раскрывает чужой черновик: %s", who, p, r.Body)
			}
		}
	}
}
