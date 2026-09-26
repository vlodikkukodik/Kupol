package httpapi_test

import (
	"fmt"
	"strings"
	"testing"
)

func TestTimelineOverHTTP(t *testing.T) {
	_, actors := teamStackWithActors(t)
	const path = "/api/team/timeline"
	body := func(year int, title string, level int) map[string]any {
		return map[string]any{"year": year, "title": title, "body": "Текст события.", "level": level, "document_code": ""}
	}

	// ведут Редактор, Архивариус и Директорат; Автор, Модератор, посторонний и гость — нет
	for name, want := range map[string]int{"guest": 401, "plain": 403, "author": 403, "moderator": 403, "editor": 201, "archivist": 201, "director": 201} {
		if got := actors[name].client.do("POST", path, body(1974, "Событие от "+name, 0)).Code; got != want {
			t.Errorf("%s: создание = %d, ожидалось %d", name, got, want)
		}
	}
	secret := actors["archivist"].client.do("POST", path, body(1990, "Тайное событие секретхрон", 5))
	if secret.Code != 201 {
		t.Fatalf("закрытое событие: %d %s", secret.Code, secret.Body)
	}
	id := int64(secret.json()["event"].(map[string]any)["id"].(float64))

	// читают члены команды — все события; гость и посторонний команды не читают
	for name, want := range map[string]int{"guest": 401, "plain": 403, "author": 200, "moderator": 200, "editor": 200} {
		if got := actors[name].client.do("GET", path, nil).Code; got != want {
			t.Errorf("%s: чтение хронологии команды = %d, ожидалось %d", name, got, want)
		}
	}
	list := actors["author"].client.do("GET", path, nil).json()["items"].([]any)
	if len(list) != 4 || list[0].(map[string]any)["can_edit"] != false {
		t.Errorf("список для Автора: %v", list)
	}

	// правка и удаление — только с правом; несуществующее и мусор — 404
	put := func(a *teamActor, target string) response {
		return a.client.do("PUT", target, body(1991, "Изменено", 5))
	}
	if r := put(actors["author"], fmt.Sprintf("%s/%d", path, id)); r.Code != 403 {
		t.Errorf("Автор изменил событие: %d", r.Code)
	}
	if r := put(actors["editor"], fmt.Sprintf("%s/%d", path, id)); r.Code != 200 || r.json()["event"].(map[string]any)["year"].(float64) != 1991 {
		t.Errorf("правка Редактором: %d %s", r.Code, r.Body)
	}
	if r := put(actors["editor"], path+"/999999"); r.Code != 404 {
		t.Errorf("правка несуществующего: %d", r.Code)
	}
	if r := actors["editor"].client.do("DELETE", path+"/abc", nil); r.Code != 404 {
		t.Errorf("мусор в номере: %d", r.Code)
	}
	if r := actors["editor"].client.do("POST", path, map[string]any{"year": 1800, "title": "", "level": 9}); r.Code != 422 || len(r.json()["error"].(map[string]any)["problems"].([]any)) < 3 {
		t.Errorf("замечания: %d %s", r.Code, r.Body)
	}

	// публичная шкала: гостю — только открытое, без закрытого события и без его слов
	pub := actors["guest"].client.do("GET", "/api/timeline", nil)
	if pub.Code != 200 || pub.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("публичная шкала: %d %v", pub.Code, pub.Header())
	}
	if strings.Contains(pub.Body.String(), "секретхрон") || strings.Contains(pub.Body.String(), "Тайное") {
		t.Errorf("гость видит закрытое событие: %s", pub.Body)
	}
	if n := len(pub.json()["items"].([]any)); n != 3 {
		t.Errorf("гость видит %d событий, ожидалось 3", n)
	}
	// читатель с допуском 5 видит закрытое; Директорат — тоже
	if r := actors["director"].client.do("GET", "/api/timeline", nil); len(r.json()["items"].([]any)) != 4 {
		t.Errorf("Директорат: %s", r.Body)
	}

	if r := actors["director"].client.do("DELETE", fmt.Sprintf("%s/%d", path, id), nil); r.Code != 204 {
		t.Errorf("удаление: %d", r.Code)
	}
}
