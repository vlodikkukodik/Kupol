package httpapi_test

import (
	"strconv"
	"testing"
)

func itoa64(n int64) string { return strconv.FormatInt(n, 10) }

func TestInboxOverHTTP(t *testing.T) {
	_, actors := teamStackWithActors(t)
	director, editor, guest := actors["director"], actors["editor"], actors["guest"]

	if r := guest.client.do("GET", "/api/me/inbox", nil); r.Code != 401 {
		t.Errorf("гость: %d", r.Code)
	}
	body := map[string]any{"title": "Объявление", "body": "Всем сотрудникам архива."}
	if r := editor.client.do("POST", "/api/team/inbox/send", body); r.Code != 403 {
		t.Errorf("Редактор шлёт записку: %d", r.Code)
	}
	if r := director.client.do("POST", "/api/team/inbox/send", map[string]any{"title": "", "body": "x"}); r.Code != 422 {
		t.Errorf("пустой заголовок: %d", r.Code)
	}
	if r := director.client.do("POST", "/api/team/inbox/send", map[string]any{"login": "нет_такого", "title": "a", "body": "b"}); r.Code != 404 {
		t.Errorf("неизвестный логин: %d", r.Code)
	}
	send := director.client.do("POST", "/api/team/inbox/send", body)
	if send.Code != 200 || send.json()["recipients"].(float64) < 2 {
		t.Fatalf("рассылка: %d %s", send.Code, send.Body)
	}

	un := editor.client.do("GET", "/api/me/inbox/unread", nil)
	if un.Code != 200 || un.json()["unread"].(float64) != 1 {
		t.Fatalf("непрочитанные: %d %s", un.Code, un.Body)
	}
	list := editor.client.do("GET", "/api/me/inbox", nil).json()
	item := list["items"].([]any)[0].(map[string]any)
	if item["title"] != "Объявление" || item["read"] != false {
		t.Errorf("записка: %v", item)
	}
	id := int64(item["id"].(float64))
	if r := editor.client.do("POST", "/api/me/inbox/9999999/read", nil); r.Code != 404 {
		t.Errorf("чужая/несуществующая: %d", r.Code)
	}
	if r := editor.client.do("POST", "/api/me/inbox/"+itoa64(id)+"/read", nil); r.Code != 204 {
		t.Fatalf("прочитано: %d", r.Code)
	}
	if n := editor.client.do("GET", "/api/me/inbox/unread", nil).json()["unread"].(float64); n != 0 {
		t.Errorf("после прочтения: %v", n)
	}
	// записка Директората уходит и ему самому; чужие записки читать нельзя
	if r := actors["author"].client.do("POST", "/api/me/inbox/"+itoa64(id)+"/read", nil); r.Code != 404 {
		t.Errorf("чужая записка: %d", r.Code)
	}
}
