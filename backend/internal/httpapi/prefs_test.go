package httpapi_test

import "testing"

// Настройки писем-уведомлений и язык писем: читатель выбирает сам, сервер запоминает выбор.
func TestEmailPrefsAndLangOverHTTP(t *testing.T) {
	st := newStack(t, nil)
	anon := st.newClient(t)
	want(t, anon.do("GET", "/api/me/email-prefs", nil), 401, "unauthenticated")
	want(t, anon.do("PUT", "/api/me/email-prefs", map[string]any{"prefs": map[string]any{"enabled": true}}), 401, "unauthenticated")
	want(t, anon.do("PUT", "/api/me/lang", map[string]any{"lang": "it"}), 401, "unauthenticated")

	c := st.newClient(t)
	c.register("mailreader", "правильный пароль")

	// по умолчанию включено всё: когда Директорат включит почту, известные события сразу доходят
	r := c.do("GET", "/api/me/email-prefs", nil)
	want(t, r, 200, "")
	prefs := r.json()["prefs"].(map[string]any)
	for _, k := range []string{"enabled", "note", "level_up", "achievement", "suggestion", "remark_reply", "petition", "invitation", "invitation_answer", "sanction"} {
		if prefs[k] != true {
			t.Errorf("по умолчанию %s = %v", k, prefs[k])
		}
	}

	// сохранить один выключенный вид; тело — объект целиком, как он пришёл
	off := map[string]any{"enabled": true, "note": false, "level_up": true, "achievement": true, "suggestion": true, "remark_reply": true, "petition": true, "invitation": true, "invitation_answer": true, "sanction": true}
	want(t, c.do("PUT", "/api/me/email-prefs", map[string]any{"prefs": off}), 204, "")
	got := c.do("GET", "/api/me/email-prefs", nil).json()["prefs"].(map[string]any)
	if got["note"] != false || got["enabled"] != true {
		t.Errorf("после сохранения: %v", got)
	}
	// лишнее поле — 400 (форма строгая); неполный объект допустим и выключает не названные виды —
	// так и задумано: интерфейс присылает объект целиком (см. components/NotifyPanel.vue)
	want(t, c.do("PUT", "/api/me/email-prefs", map[string]any{"prefs": off, "all": true}), 400, "bad_request")
	want(t, c.do("PUT", "/api/me/email-prefs", map[string]any{"prefs": map[string]any{"enabled": true}}), 204, "")
	if p := c.do("GET", "/api/me/email-prefs", nil).json()["prefs"].(map[string]any); p["level_up"] != false || p["enabled"] != true {
		t.Errorf("неполный объект: %v", p)
	}
	want(t, c.do("PUT", "/api/me/email-prefs", map[string]any{"prefs": off}), 204, "")

	// язык писем: сервер запоминает выбор интерфейса и присылает его в сессии
	want(t, c.do("PUT", "/api/me/lang", map[string]any{"lang": "it"}), 204, "")
	user := c.do("GET", "/api/auth/session", nil).json()["user"].(map[string]any)
	if user["lang"] != "it" {
		t.Errorf("язык в сессии: %v", user["lang"])
	}
	want(t, c.do("PUT", "/api/me/lang", map[string]any{"lang": "de"}), 422, "validation")
	want(t, c.do("PUT", "/api/me/lang", map[string]any{"lang": "русский"}), 422, "validation")
	want(t, c.do("PUT", "/api/me/lang", map[string]any{"lang": "ru"}), 204, "")

	// настройки писем переживают вход: другой браузер видит тот же выбор
	phone := st.newClient(t)
	want(t, phone.do("POST", "/api/auth/login", map[string]any{"login": "mailreader", "password": "правильный пароль"}), 200, "")
	if p := phone.do("GET", "/api/me/email-prefs", nil).json()["prefs"].(map[string]any); p["note"] != false {
		t.Errorf("настройки не сохранились: %v", p)
	}
}
