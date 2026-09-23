package httpapi_test

import (
	"strings"
	"testing"

	"vladhost/internal/config"
	"vladhost/internal/httpapi"
)

type ftpSite struct {
	Site struct {
		ID         int64 `json:"id"`
		FTPEnabled bool  `json:"ftp_enabled"`
		FTP        struct {
			Available bool   `json:"available"`
			Enabled   bool   `json:"enabled"`
			Host      string `json:"host"`
			Port      int    `json:"port"`
			Username  string `json:"username"`
		} `json:"ftp"`
	} `json:"site"`
	Password string `json:"password"`
}

func (e *env) withFTP() {
	e.r = httpapi.New(e.svc, e.sites, config.Config{
		JWTSecret: []byte(strings.Repeat("s", 32)), AuthPerMinute: 10000, AuthBurst: 10000,
		FTP: config.FTPConfig{Addr: ":2121", Host: "ftp.vladinc.ru"},
	})
}

func TestFTPEndpoints(t *testing.T) {
	e := newEnv(t)
	e.withFTP()
	adm, _ := e.admin()
	john, mary := e.user(adm, "john"), e.user(adm, "mary")
	id, _ := e.createSite(john, "blog")
	base := "/api/sites/" + itoa(id) + "/ftp"

	// До выдачи: FTP доступен на сервере, но у сайта не включён.
	w := e.do("GET", "/api/sites", nil, john)
	got := decode[struct {
		Sites []struct {
			FTP struct {
				Available bool   `json:"available"`
				Enabled   bool   `json:"enabled"`
				Username  string `json:"username"`
				Host      string `json:"host"`
				Port      int    `json:"port"`
			} `json:"ftp"`
		} `json:"sites"`
	}](t, w).Sites[0].FTP
	if !got.Available || got.Enabled || got.Username != "blog.john" || got.Host != "ftp.vladinc.ru" || got.Port != 2121 {
		t.Fatalf("до выдачи: %+v", got)
	}

	// Выдача: пароль виден в ответе один раз.
	w = e.do("POST", base, nil, john)
	if w.Code != 200 {
		t.Fatalf("выдача: %d %s", w.Code, w.Body)
	}
	first := decode[ftpSite](t, w)
	if len(first.Password) < 16 || !first.Site.FTP.Enabled || first.Site.FTP.Username != "blog.john" {
		t.Fatalf("выдача: %+v", first)
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("ответ с паролем не должен кэшироваться")
	}
	// Пароль (и его хеш) больше нигде не отдаётся.
	if w := e.do("GET", "/api/sites", nil, john); strings.Contains(w.Body.String(), first.Password) || strings.Contains(w.Body.String(), "$2a$") {
		t.Fatalf("пароль или хеш утекли в список сайтов: %s", w.Body)
	}
	// Повторная выдача = смена пароля.
	second := decode[ftpSite](t, e.do("POST", base, nil, john))
	if second.Password == first.Password {
		t.Fatal("повторная выдача должна менять пароль")
	}

	// Чужой сайт недоступен, аноним тоже.
	if w := e.do("POST", base, nil, mary); w.Code != 404 {
		t.Fatalf("выдача для чужого сайта: %d", w.Code)
	}
	if w := e.do("DELETE", base, nil, mary); w.Code != 404 {
		t.Fatalf("отзыв для чужого сайта: %d", w.Code)
	}
	if w := e.do("POST", base, nil, ""); w.Code != 401 {
		t.Fatalf("аноним: %d", w.Code)
	}

	// Отзыв.
	w = e.do("DELETE", base, nil, john)
	if w.Code != 200 || decode[ftpSite](t, w).Site.FTP.Enabled {
		t.Fatalf("отзыв: %d %s", w.Code, w.Body)
	}
}

func TestFTPUnavailableOnServer(t *testing.T) {
	e := newEnv(t) // FTP в конфиге не задан
	adm, _ := e.admin()
	john := e.user(adm, "john")
	id, _ := e.createSite(john, "blog")

	w := e.do("POST", "/api/sites/"+itoa(id)+"/ftp", nil, john)
	if w.Code != 409 || decode[errBody](t, w).Error.Code != "ftp_unavailable" {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if w := e.do("GET", "/api/sites", nil, john); !strings.Contains(w.Body.String(), `"available":false`) {
		t.Fatalf("список должен сообщать, что FTP недоступен: %s", w.Body)
	}
}
