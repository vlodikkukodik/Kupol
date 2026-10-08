package httpapi_test

import "testing"

func TestAchievementsOverHTTP(t *testing.T) {
	st := newStack(t, nil)
	const pw = "правильный пароль"
	c := st.newClient(t)
	reg := c.register("hunter", pw)
	if reg.Code != 201 {
		t.Fatalf("регистрация: %d %s", reg.Code, reg.Body)
	}

	guest := st.newClient(t)
	if r := guest.do("GET", "/api/me/achievements", nil); r.Code != 401 {
		t.Errorf("гость: %d", r.Code)
	}

	list := c.do("GET", "/api/me/achievements", nil)
	if list.Code != 200 {
		t.Fatalf("список: %d %s", list.Code, list.Body)
	}
	if items := list.json()["items"].([]any); len(items) != 0 {
		t.Errorf("свежий аккаунт без грамот: %v", items)
	}

	// вход по-итальянски: у нового аккаунта грамот пока нет, но ответ на вход уже несёт поле new_achievements (не мешает)
	c.lang = "it"
	login := c.do("POST", "/api/auth/login", map[string]any{"login": "hunter", "password": pw})
	if login.Code != 200 {
		t.Fatalf("вход: %d %s", login.Code, login.Body)
	}
	if _, ok := login.json()["new_achievements"]; ok {
		t.Errorf("new_achievements не должно быть в ответе, когда грамот нет (omitempty): %s", login.Body)
	}

	// грамота, выданная напрямую (как погашение скрытого кода), видна в списке — с переводом по языку запроса
	var uid int64
	if err := st.db.Raw(`SELECT id FROM users WHERE login = ?`, "hunter").Scan(&uid).Error; err != nil {
		t.Fatal(err)
	}
	if err := st.db.Exec(`INSERT INTO achievements (user_id, kind, awarded_at) VALUES (?, 'secret_finder', now())`, uid).Error; err != nil {
		t.Fatal(err)
	}
	listIT := c.do("GET", "/api/me/achievements", nil)
	items := listIT.json()["items"].([]any)
	if len(items) != 1 || items[0].(map[string]any)["kind"] != "secret_finder" || items[0].(map[string]any)["name"] != "Ha trovato un codice segreto" {
		t.Errorf("список по-итальянски: %v", items)
	}
}
