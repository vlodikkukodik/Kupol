package httpapi_test

import "testing"

func TestSiteSettingsOverHTTP(t *testing.T) {
	_, actors := teamStackWithActors(t)
	const path = "/api/team/site"

	// публично: пока контакта нет — пусто
	pub := actors["guest"].client.do("GET", "/api/site", nil)
	if pub.Code != 200 || pub.Header().Get("Cache-Control") != "no-store" || pub.json()["site"].(map[string]any)["contact"] != "" {
		t.Fatalf("публичные настройки: %d %s", pub.Code, pub.Body)
	}

	// читают члены команды, правит только Директорат
	for name, want := range map[string]int{"guest": 401, "plain": 403, "author": 200, "moderator": 200, "editor": 200, "archivist": 200, "director": 200} {
		if got := actors[name].client.do("GET", path, nil).Code; got != want {
			t.Errorf("%s: чтение = %d, ожидалось %d", name, got, want)
		}
	}
	for name, want := range map[string]int{"guest": 401, "plain": 403, "author": 403, "editor": 403, "archivist": 403, "director": 200} {
		if got := actors[name].client.do("PUT", path, map[string]any{"contact": "контакт от " + name}).Code; got != want {
			t.Errorf("%s: правка = %d, ожидалось %d", name, got, want)
		}
	}
	if r := actors["editor"].client.do("GET", path, nil); r.json()["site"].(map[string]any)["can_edit"] != false || r.json()["site"].(map[string]any)["contact"] != "контакт от director" {
		t.Errorf("Редактор: %s", r.Body)
	}
	if r := actors["director"].client.do("GET", path, nil); r.json()["site"].(map[string]any)["can_edit"] != true || r.json()["site"].(map[string]any)["updated_by"] != "user_director" {
		t.Errorf("Директорат: %s", r.Body)
	}
	// теперь контакт виден всем, в том числе гостю
	if r := actors["guest"].client.do("GET", "/api/site", nil); r.json()["site"].(map[string]any)["contact"] != "контакт от director" {
		t.Errorf("публичный контакт: %s", r.Body)
	}
	// замечания — 422 с полем; неизвестные поля — 400
	long := make([]rune, 1001)
	for i := range long {
		long[i] = 'я'
	}
	if r := actors["director"].client.do("PUT", path, map[string]any{"contact": string(long)}); r.Code != 422 {
		t.Errorf("длинный контакт: %d %s", r.Code, r.Body)
	}
	if r := actors["director"].client.do("PUT", path, map[string]any{"contact": "х", "role": "admin"}); r.Code != 400 {
		t.Errorf("лишнее поле: %d", r.Code)
	}
	// очистка убирает контакт
	if r := actors["director"].client.do("PUT", path, map[string]any{"contact": ""}); r.Code != 200 {
		t.Fatalf("очистка: %d", r.Code)
	}
	if r := actors["guest"].client.do("GET", "/api/site", nil); r.json()["site"].(map[string]any)["contact"] != "" {
		t.Errorf("контакт остался: %s", r.Body)
	}
}
