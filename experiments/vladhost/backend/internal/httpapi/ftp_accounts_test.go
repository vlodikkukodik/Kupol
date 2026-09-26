package httpapi_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type acctBody struct {
	Account struct {
		ID       int64  `json:"id"`
		Name     string `json:"name"`
		Username string `json:"username"`
		Dir      string `json:"dir"`
		ReadOnly bool   `json:"read_only"`
		Enabled  bool   `json:"enabled"`
	} `json:"account"`
	Password string `json:"password"`
}

type siteAccounts struct {
	Sites []struct {
		FTP struct {
			Accounts []struct {
				ID       int64  `json:"id"`
				Username string `json:"username"`
				Dir      string `json:"dir"`
				ReadOnly bool   `json:"read_only"`
				Enabled  bool   `json:"enabled"`
			} `json:"accounts"`
			Limit int `json:"accounts_limit"`
		} `json:"ftp"`
	} `json:"sites"`
}

func TestFTPAccountEndpoints(t *testing.T) {
	e := newEnv(t)
	e.withFTP()
	adm, _ := e.admin()
	john, mary := e.user(adm, "john"), e.user(adm, "mary")
	id, host := e.createSite(john, "blog")
	base := "/api/sites/" + itoa(id) + "/ftp/accounts"

	// Создание: пароль виден только в этом ответе, папка создаётся на диске.
	w := e.do("POST", base, map[string]any{"name": "Deploy", "dir": "/app/dist/", "read_only": false}, john)
	if w.Code != 201 || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("создание: %d %s %v", w.Code, w.Body, w.Header())
	}
	c := decode[acctBody](t, w)
	if c.Account.Name != "deploy" || c.Account.Username != "deploy.blog.john" || c.Account.Dir != "app/dist" || !c.Account.Enabled || len(c.Password) != 20 {
		t.Fatalf("аккаунт: %+v", c)
	}
	if fi, err := os.Stat(filepath.Join(e.root, host, "public", "app", "dist")); err != nil || !fi.IsDir() {
		t.Fatalf("папка не создана: %v", err)
	}
	aid := itoa(c.Account.ID)

	// В списке сайтов аккаунт есть, а пароля нет нигде.
	w = e.do("GET", "/api/sites", nil, john)
	if strings.Contains(w.Body.String(), c.Password) || strings.Contains(w.Body.String(), "password_hash") {
		t.Fatalf("секрет попал в список сайтов: %s", w.Body)
	}
	list := decode[siteAccounts](t, w).Sites[0].FTP
	if len(list.Accounts) != 1 || list.Accounts[0].Username != "deploy.blog.john" || list.Limit != 5 {
		t.Fatalf("список: %+v", list)
	}

	// Изменение: отключение, режим, папка. Пароль в ответе не возвращается.
	w = e.do("PATCH", base+"/"+aid, map[string]any{"enabled": false, "read_only": true, "dir": "docs"}, john)
	up := decode[acctBody](t, w)
	if w.Code != 200 || up.Account.Enabled || !up.Account.ReadOnly || up.Account.Dir != "docs" || up.Password != "" {
		t.Fatalf("изменение: %d %+v", w.Code, up)
	}
	// Пустое тело ничего не меняет и не ломает.
	if w := e.do("PATCH", base+"/"+aid, map[string]any{}, john); w.Code != 200 {
		t.Fatalf("пустое изменение: %d %s", w.Code, w.Body)
	}

	// Сброс пароля выдаёт новый.
	w = e.do("POST", base+"/"+aid+"/password", nil, john)
	rp := decode[acctBody](t, w)
	if w.Code != 200 || len(rp.Password) != 20 || rp.Password == c.Password {
		t.Fatalf("сброс: %d %+v", w.Code, rp)
	}

	// Ошибки проверки: коды и поля.
	for _, tc := range []struct {
		body        map[string]any
		code, field string
		status      int
	}{
		{map[string]any{"name": "a.b"}, "validation.ftp_name", "name", 422},
		{map[string]any{"name": ""}, "validation.ftp_name", "name", 422},
		{map[string]any{"name": "ok", "dir": "../x"}, "validation.ftp_dir", "dir", 422},
		{map[string]any{"name": "ok", "dir": ".git"}, "validation.ftp_dir", "dir", 422},
		{map[string]any{"name": "deploy"}, "ftp_name_taken", "name", 409},
	} {
		w := e.do("POST", base, tc.body, john)
		got := decode[errBody](t, w).Error
		if w.Code != tc.status || got.Code != tc.code || got.Field != tc.field {
			t.Errorf("%v → %d %+v", tc.body, w.Code, got)
		}
	}
	if w := e.do("PATCH", base+"/"+aid, map[string]any{"dir": ".."}, john); w.Code != 422 {
		t.Errorf("изменение папки на недопустимую: %d", w.Code)
	}
	if w := e.do("POST", base, "not-json-object", john); w.Code != 400 {
		t.Errorf("не объект: %d", w.Code)
	}

	// Лимит на сайт.
	for _, n := range []string{"b", "c", "d", "e"} {
		if w := e.do("POST", base, map[string]any{"name": n}, john); w.Code != 201 {
			t.Fatalf("аккаунт %s: %d %s", n, w.Code, w.Body)
		}
	}
	if w := e.do("POST", base, map[string]any{"name": "f"}, john); w.Code != 403 || decode[errBody](t, w).Error.Code != "ftp_account_limit" {
		t.Errorf("лимит: %d %s", w.Code, w.Body)
	}

	// Чужой пользователь: ничего не видит и не меняет (404), без токена — 401.
	for _, req := range [][2]string{{"POST", base}, {"PATCH", base + "/" + aid}, {"POST", base + "/" + aid + "/password"}, {"DELETE", base + "/" + aid}} {
		if w := e.do(req[0], req[1], map[string]any{"name": "x"}, mary); w.Code != 404 {
			t.Errorf("%s %s чужим: %d", req[0], req[1], w.Code)
		}
		if w := e.do(req[0], req[1], nil, ""); w.Code != 401 {
			t.Errorf("%s %s без токена: %d", req[0], req[1], w.Code)
		}
	}
	if w := e.do("PATCH", base+"/999999", map[string]any{"enabled": true}, john); w.Code != 404 {
		t.Errorf("несуществующий: %d", w.Code)
	}
	if w := e.do("PATCH", base+"/abc", map[string]any{"enabled": true}, john); w.Code != 404 {
		t.Errorf("мусорный id: %d", w.Code)
	}

	// Удаление.
	if w := e.do("DELETE", base+"/"+aid, nil, john); w.Code != 204 {
		t.Fatalf("удаление: %d %s", w.Code, w.Body)
	}
	if w := e.do("DELETE", base+"/"+aid, nil, john); w.Code != 404 {
		t.Errorf("повторное удаление: %d", w.Code)
	}
}

func TestFTPAccountsRequireFTPOnServer(t *testing.T) {
	e := newEnv(t) // FTP на сервере не включён
	adm, _ := e.admin()
	john := e.user(adm, "john")
	id, _ := e.createSite(john, "blog")
	w := e.do("POST", "/api/sites/"+itoa(id)+"/ftp/accounts", map[string]any{"name": "x"}, john)
	if w.Code != 409 || decode[errBody](t, w).Error.Code != "ftp_unavailable" {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
}

func TestFTPAccountsListIsEmptyArrayNotNull(t *testing.T) {
	e := newEnv(t)
	e.withFTP()
	adm, _ := e.admin()
	john := e.user(adm, "john")
	e.createSite(john, "blog")
	if body := e.do("GET", "/api/sites", nil, john).Body.String(); !strings.Contains(body, `"accounts":[]`) {
		t.Fatalf("список аккаунтов должен быть [], а не null (фронтенд разбирает ответ строго): %s", body)
	}
}

func TestFTPAccountsDeletedWithSite(t *testing.T) {
	e := newEnv(t)
	e.withFTP()
	adm, _ := e.admin()
	john := e.user(adm, "john")
	id, _ := e.createSite(john, "blog")
	e.do("POST", "/api/sites/"+itoa(id)+"/ftp/accounts", map[string]any{"name": "x"}, john)
	if w := e.do("DELETE", "/api/sites/"+itoa(id), nil, john); w.Code != 204 {
		t.Fatalf("удаление сайта: %d", w.Code)
	}
	var n int64
	if err := e.db.Table("ftp_accounts").Count(&n).Error; err != nil || n != 0 {
		t.Fatalf("аккаунты остались после удаления сайта: %d %v", n, err)
	}
}
