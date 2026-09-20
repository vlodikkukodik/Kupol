package httpapi_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestExportAndImportOverHTTP(t *testing.T) {
	_, actors := teamStackWithActors(t)
	author, other := actors["author"], actors["editor"]

	made := author.client.do("POST", "/api/team/documents", map[string]any{
		"type": "memo", "code": "МЕМО-901", "title": "Для выгрузки *важно*", "composed": map[string]any{"year": 1979},
		"blocks": []any{map[string]any{"id": "p", "type": "paragraph", "data": map[string]any{"text": "Текст."}}},
	})
	if made.Code != 201 {
		t.Fatalf("создание: %d %s", made.Code, made.Body)
	}
	id := int64(made.json()["document"].(map[string]any)["id"].(float64))
	path := fmt.Sprintf("/api/team/documents/%d/export", id)

	// файл: заголовки скачивания, JSON разбирается, Markdown читаем
	r := author.client.do("GET", path+"?format=json", nil)
	if r.Code != 200 || r.Header().Get("Content-Disposition") != `attachment; filename="MEMO-901.json"` || !strings.HasPrefix(r.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("JSON: %d %v", r.Code, r.Header())
	}
	var file map[string]any
	if err := json.Unmarshal(r.Body.Bytes(), &file); err != nil || file["code"] != "МЕМО-901" || file["status"] != "draft" {
		t.Errorf("файл JSON: %v %v", err, file)
	}
	if d := author.client.do("GET", path, nil); d.Code != 200 || !strings.HasSuffix(d.Header().Get("Content-Disposition"), `.json"`) {
		t.Errorf("формат по умолчанию — JSON: %d %v", d.Code, d.Header())
	}
	md := author.client.do("GET", path+"?format=md", nil)
	if md.Code != 200 || md.Header().Get("Content-Disposition") != `attachment; filename="MEMO-901.md"` || !strings.HasPrefix(md.Header().Get("Content-Type"), "text/markdown") ||
		!strings.Contains(md.Body.String(), `# Для выгрузки \*важно\*`) || !strings.Contains(md.Body.String(), "Текст.") {
		t.Errorf("Markdown: %d %v\n%s", md.Code, md.Header(), md.Body)
	}
	if md.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Errorf("скачиваемый файл без nosniff: %v", md.Header())
	}

	// доступ: гость 401, не-член 403, чужой черновик 404, неверный формат 400, мусор в номере 404
	for name, want := range map[string]int{"guest": 401, "plain": 403, "editor": 404, "moderator": 404, "author": 200, "director": 200} {
		if got := actors[name].client.do("GET", path+"?format=json", nil).Code; got != want {
			t.Errorf("%s: экспорт = %d, ожидалось %d", name, got, want)
		}
	}
	if r := author.client.do("GET", path+"?format=pdf", nil); r.Code != 400 || r.fields()["format"] == nil {
		t.Errorf("формат pdf: %d %s", r.Code, r.Body)
	}
	if r := author.client.do("GET", "/api/team/documents/abc/export", nil); r.Code != 404 {
		t.Errorf("мусор вместо номера: %d", r.Code)
	}

	// импорт: тело — сам файл. Проверка ничего не создаёт, загрузка создаёт черновик за загрузившим
	file["code"] = "МЕМО-902"
	file["status"] = "published"
	imp := "/api/team/documents/import"
	dry := other.client.do("POST", imp+"?dry_run=1", file)
	if dry.Code != 200 || dry.json()["dry_run"] != true || dry.json()["document"] != nil || dry.json()["status_ignored"] != "published" || dry.json()["type_name"] != "Меморандум" || dry.json()["code"] != "МЕМО-902" {
		t.Fatalf("проверка: %d %s", dry.Code, dry.Body)
	}
	if list := other.client.do("GET", "/api/team/documents?q=902", nil).json()["items"].([]any); len(list) != 0 {
		t.Fatalf("проверка создала документ: %v", list)
	}
	real := other.client.do("POST", imp, file)
	if real.Code != 201 {
		t.Fatalf("загрузка: %d %s", real.Code, real.Body)
	}
	doc := real.json()["document"].(map[string]any)
	if doc["status"] != "draft" || doc["author"] != other.login || doc["code"] != "МЕМО-902" {
		t.Errorf("загруженный документ: %v", doc)
	}

	// повторно тот же шифр — 409 с полем; проверка — тоже
	for _, q := range []string{"", "?dry_run=1"} {
		if r := other.client.do("POST", imp+q, file); r.Code != 409 || r.errCode() != "code_taken" || r.fields()["code"] == nil {
			t.Errorf("занятый шифр%s: %d %s", q, r.Code, r.Body)
		}
	}
	// замечания — с путями; пакет отклоняется
	bad := map[string]any{"type": "memo", "code": "МЕМО-903", "title": "", "composed": map[string]any{"year": 1979}, "blocks": []any{}}
	if r := other.client.do("POST", imp, bad); r.Code != 422 {
		t.Errorf("замечания: %d %s", r.Code, r.Body)
	}
	if r := other.client.do("POST", imp, map[string]any{"documents": []any{file}}); r.Code != 422 || !strings.Contains(r.Body.String(), "пакет") {
		t.Errorf("пакет: %d %s", r.Code, r.Body)
	}
	if r := other.client.do("POST", imp+"?dry_run=2", file); r.Code != 400 {
		t.Errorf("dry_run=2: %d", r.Code)
	}
	// права: гость 401, не-член 403, член команды без права писать 403
	for name, want := range map[string]int{"guest": 401, "plain": 403, "moderator": 403} {
		if got := actors[name].client.do("POST", imp, file).Code; got != want {
			t.Errorf("%s: импорт = %d, ожидалось %d", name, got, want)
		}
	}
}
