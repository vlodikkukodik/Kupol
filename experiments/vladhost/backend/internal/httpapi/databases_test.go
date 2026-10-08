package httpapi_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"vladhost/internal/config"
	"vladhost/internal/httpapi"
	"vladhost/internal/userdb"
)

const dbKey = "test-internal-key-0123456789abcdef0123456789"

type dbView struct {
	ID        int64    `json:"id"`
	Engine    string   `json:"engine"`
	Name      string   `json:"name"`
	Status    string   `json:"status"`
	SizeBytes int64    `json:"size_bytes"`
	Addrs     []string `json:"addrs"`
}

type dbCreated struct {
	Database dbView `json:"database"`
	Password string `json:"password"`
}

type dbList struct {
	Databases []dbView `json:"databases"`
	Info      struct {
		Engines   []string       `json:"engines"`
		PerEngine int            `json:"per_engine"`
		SizeLimit int64          `json:"size_limit"`
		Host      string         `json:"host"`
		Ports     map[string]int `json:"ports"`
		WebClient bool           `json:"web_client"`
		MaxAddrs  int            `json:"max_addrs"`
	} `json:"info"`
}

// withDatabases включает раздел: настоящие серверы PostgreSQL и MariaDB из docker-compose.
func (e *env) withDatabases(sizeLimit int64) *userdb.Service {
	e.t.Helper()
	pgURL, addr := os.Getenv("VLADHOST_TEST_DATABASE_URL"), os.Getenv("VLADHOST_TEST_MARIADB_ADDR")
	if pgURL == "" || addr == "" {
		e.t.Fatal("VLADHOST_TEST_DATABASE_URL и VLADHOST_TEST_MARIADB_ADDR не заданы (make db-up, затем make test-back)")
	}
	pg, err := userdb.NewPG(userdb.PGConfig{AdminURL: pgURL, HBADir: e.t.TempDir(), FrozenRolePrefix: "frz" + randSuffix() + "_"})
	if err != nil {
		e.t.Fatal(err)
	}
	my, err := userdb.NewMaria(userdb.MariaConfig{AdminDSN: "root:vladhost@tcp(" + addr + ")/", ExternalAccess: true, LocalHost: "%"})
	if err != nil {
		e.t.Fatal(err)
	}
	e.t.Cleanup(func() { _ = my.Close() })
	svc := userdb.NewService(e.db, map[userdb.Engine]userdb.Backend{userdb.Postgres: pg, userdb.MariaDB: my}, userdb.Config{
		PerEngine: 2, SizeLimit: sizeLimit, Host: "db.example.test", Ports: map[userdb.Engine]int{userdb.Postgres: 5433, userdb.MariaDB: 3306},
		External: map[userdb.Engine]bool{userdb.Postgres: true, userdb.MariaDB: true}, WebURL: "https://db.example.test",
		WebServers: map[userdb.Engine]string{userdb.Postgres: "127.0.0.1:5433", userdb.MariaDB: "localhost"},
	})
	e.r = httpapi.New(e.svc, e.sites, config.Config{JWTSecret: []byte(strings.Repeat("s", 32)), AuthPerMinute: 10000, AuthBurst: 10000},
		httpapi.WithDatabases(svc, dbKey))
	// Базы удаляются с серверов даже если тест упал на полпути.
	e.t.Cleanup(func() {
		rows, err := e.db.Raw("SELECT id, user_id FROM user_databases").Rows()
		if err != nil {
			return
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var id, uid int64
			_ = rows.Scan(&id, &uid)
			_ = svc.Delete(context.Background(), uid, id)
		}
	})
	return svc
}

func randSuffix() string {
	b := make([]byte, 3)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func (e *env) dbCreate(tok, engine, name string) (int, dbCreated, string) {
	e.t.Helper()
	w := e.do("POST", "/api/databases", map[string]string{"engine": engine, "name": name}, tok)
	if w.Code != 201 {
		return w.Code, dbCreated{}, w.Body.String()
	}
	return w.Code, decode[dbCreated](e.t, w), w.Body.String()
}

// connectAs проверяет, что выданный пароль действительно открывает базу на сервере.
func connectAs(t *testing.T, engine, name, pass string) error {
	t.Helper()
	switch engine {
	case "postgres":
		u, _ := url.Parse(os.Getenv("VLADHOST_TEST_DATABASE_URL"))
		cu := url.URL{Scheme: "postgres", User: url.UserPassword(name, pass), Host: u.Host, Path: "/" + name, RawQuery: "sslmode=disable"}
		c, err := pgx.Connect(context.Background(), cu.String())
		if err != nil {
			return err
		}
		return c.Close(context.Background())
	default:
		d, err := sql.Open("mysql", name+":"+pass+"@tcp("+os.Getenv("VLADHOST_TEST_MARIADB_ADDR")+")/"+name)
		if err != nil {
			return err
		}
		defer func() { _ = d.Close() }()
		return d.Ping()
	}
}

func TestDatabasesSectionIsHiddenWhenDisabled(t *testing.T) {
	e := newEnv(t)
	adm, _ := e.admin()
	tok := e.user(adm, "john")
	for _, tc := range [][2]string{{"GET", "/api/databases"}, {"POST", "/api/databases"}, {"DELETE", "/api/databases/1"}, {"POST", "/api/databases/1/web"}} {
		if w := e.do(tc[0], tc[1], map[string]string{"engine": "postgres", "name": "x"}, tok); w.Code != 404 {
			t.Errorf("%s %s: %d", tc[0], tc[1], w.Code)
		}
	}
	req := httptest.NewRequest("POST", "/api/internal/db-session", strings.NewReader(`{"token":"x"}`))
	req.RemoteAddr = "127.0.0.1:5555"
	req.Header.Set("X-Vh-Internal", dbKey)
	w := httptest.NewRecorder()
	e.r.ServeHTTP(w, req)
	if w.Code != 404 {
		t.Errorf("служебный обмен при выключенных базах: %d", w.Code)
	}
}

func TestDatabasesLifecycleThroughAPI(t *testing.T) {
	e := newEnv(t)
	e.withDatabases(100 << 20)
	adm, _ := e.admin()
	john, mary := e.user(adm, "john"), e.user(adm, "mary")

	if w := e.do("GET", "/api/databases", nil, ""); w.Code != 401 {
		t.Fatalf("без входа: %d", w.Code)
	}
	empty := decode[dbList](t, e.do("GET", "/api/databases", nil, john))
	if len(empty.Databases) != 0 || empty.Databases == nil || len(empty.Info.Engines) != 2 || empty.Info.PerEngine != 2 || empty.Info.SizeLimit != 100<<20 ||
		empty.Info.Host != "db.example.test" || empty.Info.Ports["postgres"] != 5433 || !empty.Info.WebClient || empty.Info.MaxAddrs != 10 {
		t.Fatalf("пустой список: %+v", empty)
	}

	for _, eng := range []string{"postgres", "mariadb"} {
		code, c, body := e.dbCreate(john, eng, "blog")
		if code != 201 || c.Database.Name != "john_blog" || c.Database.Engine != eng || c.Database.Status != "active" || len(c.Password) != 24 {
			t.Fatalf("%s: %d %s", eng, code, body)
		}
		if err := connectAs(t, eng, c.Database.Name, c.Password); err != nil {
			t.Fatalf("%s: выданный пароль не открывает базу: %v", eng, err)
		}
		// Пароль есть только в ответе на создание.
		if w := e.do("GET", "/api/databases", nil, john); strings.Contains(w.Body.String(), c.Password) || strings.Contains(strings.ToLower(w.Body.String()), "password") {
			t.Fatalf("%s: пароль попал в список: %s", eng, w.Body)
		}
		// Смена пароля: старый перестаёт работать.
		w := e.do("POST", fmt.Sprintf("/api/databases/%d/password", c.Database.ID), nil, john)
		fresh := decode[dbCreated](t, w)
		if w.Code != 200 || fresh.Password == c.Password || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("%s: сброс пароля: %d %s", eng, w.Code, w.Body)
		}
		if connectAs(t, eng, c.Database.Name, c.Password) == nil || connectAs(t, eng, c.Database.Name, fresh.Password) != nil {
			t.Fatalf("%s: старый пароль должен не работать, новый — работать", eng)
		}
		// Ошибки создания.
		if code, _, body := e.dbCreate(john, eng, "blog"); code != 409 || !strings.Contains(body, "db_taken") {
			t.Errorf("%s: повтор: %d %s", eng, code, body)
		}
		if code, _, body := e.dbCreate(john, eng, "Bad_Name"); code != 422 || !strings.Contains(body, "validation.db_name") || !strings.Contains(body, `"field":"name"`) {
			t.Errorf("%s: имя: %d %s", eng, code, body)
		}
		// Второй пользователь с тем же именем — своя база.
		if code, _, body := e.dbCreate(mary, eng, "blog"); code != 201 {
			t.Errorf("%s: mary: %d %s", eng, code, body)
		}
	}
	// Лимит (в тесте 2 базы каждой СУБД) — по СУБД отдельно.
	if code, _, body := e.dbCreate(john, "postgres", "second"); code != 201 {
		t.Fatalf("вторая: %d %s", code, body)
	}
	if code, _, body := e.dbCreate(john, "postgres", "third"); code != 403 || !strings.Contains(body, "db_limit") || !strings.Contains(body, "не больше 2") {
		t.Fatalf("лимит: %d %s", code, body)
	}
	if w := e.doLang("it", "POST", "/api/databases", map[string]string{"engine": "postgres", "name": "third"}, john); !strings.Contains(w.Body.String(), "al massimo 2") {
		t.Fatalf("итальянский текст лимита: %s", w.Body)
	}
	for _, bad := range []map[string]string{{"engine": "oracle", "name": "x"}, {"engine": "", "name": "x"}} {
		if w := e.do("POST", "/api/databases", bad, john); w.Code != 409 {
			t.Errorf("%v: %d %s", bad, w.Code, w.Body)
		}
	}
	if w := e.do("POST", "/api/databases", "не объект", john); w.Code != 400 {
		t.Errorf("не объект: %d", w.Code)
	}

	list := decode[dbList](t, e.do("GET", "/api/databases", nil, john))
	if len(list.Databases) != 3 {
		t.Fatalf("у john %d баз", len(list.Databases))
	}
	// Удаление.
	victim := list.Databases[0]
	if w := e.do("DELETE", fmt.Sprintf("/api/databases/%d", victim.ID), nil, john); w.Code != 204 {
		t.Fatalf("удаление: %d %s", w.Code, w.Body)
	}
	if w := e.do("DELETE", fmt.Sprintf("/api/databases/%d", victim.ID), nil, john); w.Code != 404 {
		t.Errorf("повторное удаление: %d", w.Code)
	}
}

func TestDatabasesOfOthersAreInvisible(t *testing.T) {
	e := newEnv(t)
	e.withDatabases(100 << 20)
	adm, _ := e.admin()
	john, mary := e.user(adm, "john"), e.user(adm, "mary")
	_, c, _ := e.dbCreate(john, "postgres", "private")
	id := c.Database.ID
	for _, tc := range []struct{ method, path string }{
		{"DELETE", fmt.Sprintf("/api/databases/%d", id)}, {"POST", fmt.Sprintf("/api/databases/%d/password", id)},
		{"PUT", fmt.Sprintf("/api/databases/%d/addrs", id)}, {"POST", fmt.Sprintf("/api/databases/%d/web", id)},
		{"POST", fmt.Sprintf("/api/databases/%d/check", id)},
	} {
		if w := e.do(tc.method, tc.path, map[string]any{"addrs": []string{"203.0.113.5"}}, mary); w.Code != 404 || !strings.Contains(w.Body.String(), "db_not_found") {
			t.Errorf("%s %s чужим: %d %s", tc.method, tc.path, w.Code, w.Body)
		}
		if w := e.do(tc.method, tc.path, nil, ""); w.Code != 401 {
			t.Errorf("%s %s без входа: %d", tc.method, tc.path, w.Code)
		}
	}
	if w := e.do("DELETE", "/api/databases/abc", nil, john); w.Code != 404 {
		t.Errorf("мусорный id: %d", w.Code)
	}
	if got := decode[dbList](t, e.do("GET", "/api/databases", nil, mary)); len(got.Databases) != 0 {
		t.Fatalf("чужие базы в списке: %+v", got.Databases)
	}
	// База john цела и пароль от неё по-прежнему работает.
	if err := connectAs(t, "postgres", c.Database.Name, c.Password); err != nil {
		t.Fatalf("после чужих попыток: %v", err)
	}
}

func TestDatabaseExternalAccessAddresses(t *testing.T) {
	e := newEnv(t)
	e.withDatabases(100 << 20)
	adm, _ := e.admin()
	tok := e.user(adm, "john")
	_, c, _ := e.dbCreate(tok, "mariadb", "ext")
	url := fmt.Sprintf("/api/databases/%d/addrs", c.Database.ID)
	w := e.do("PUT", url, map[string]any{"addrs": []string{"203.0.113.9", " 203.0.113.5 ", "203.0.113.5/32", "198.51.100.77/24"}}, tok)
	got := decode[struct{ Database dbView }](t, w).Database
	if w.Code != 200 || strings.Join(got.Addrs, ",") != "198.51.100.0/24,203.0.113.5,203.0.113.9" {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if list := decode[dbList](t, e.do("GET", "/api/databases", nil, tok)); len(list.Databases[0].Addrs) != 3 {
		t.Fatalf("список: %+v", list.Databases)
	}
	for name, addrs := range map[string][]string{"мусор": {"junk"}, "широкая": {"10.0.0.0/8"}, "loopback": {"127.0.0.1"}, "IPv6-сеть": {"2001:db8::/32"}} {
		w := e.do("PUT", url, map[string]any{"addrs": addrs}, tok)
		if w.Code != 422 || !strings.Contains(w.Body.String(), "validation.db_addr") || !strings.Contains(w.Body.String(), `"field":"addrs"`) {
			t.Errorf("%s: %d %s", name, w.Code, w.Body)
		}
	}
	var many []string
	for i := 1; i <= 11; i++ {
		many = append(many, fmt.Sprintf("203.0.113.%d", i))
	}
	if w := e.do("PUT", url, map[string]any{"addrs": many}, tok); w.Code != 422 || !strings.Contains(w.Body.String(), "db_addr_limit") {
		t.Errorf("слишком много: %d %s", w.Code, w.Body)
	}
	if w := e.do("PUT", url, "не объект", tok); w.Code != 400 {
		t.Errorf("не объект: %d", w.Code)
	}
	// Отказы не меняют сохранённый список; закрытие доступа — пустой список.
	if list := decode[dbList](t, e.do("GET", "/api/databases", nil, tok)); len(list.Databases[0].Addrs) != 3 {
		t.Fatalf("список изменился после отказов: %+v", list.Databases[0].Addrs)
	}
	w = e.do("PUT", url, map[string]any{"addrs": []string{}}, tok)
	if got := decode[struct{ Database dbView }](t, w).Database; w.Code != 200 || len(got.Addrs) != 0 || !strings.Contains(w.Body.String(), `"addrs":[]`) {
		t.Fatalf("закрытие доступа: %d %s", w.Code, w.Body)
	}
}

func internalCall(e *env, token, remote, key string, hdr map[string]string) *httptest.ResponseRecorder {
	body, _ := json.Marshal(map[string]string{"token": token})
	req := httptest.NewRequest("POST", "/api/internal/db-session", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = remote
	if key != "" {
		req.Header.Set("X-Vh-Internal", key)
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	e.r.ServeHTTP(w, req)
	return w
}

func TestDatabaseWebClientLoginFlow(t *testing.T) {
	e := newEnv(t)
	e.withDatabases(100 << 20)
	adm, _ := e.admin()
	tok := e.user(adm, "john")
	for _, eng := range []string{"postgres", "mariadb"} {
		_, c, _ := e.dbCreate(tok, eng, "web")
		w := e.do("POST", fmt.Sprintf("/api/databases/%d/web", c.Database.ID), nil, tok)
		link := decode[struct{ URL string }](t, w).URL
		if w.Code != 200 || !strings.HasPrefix(link, "https://db.example.test/?vhtoken=") || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("%s: %d %s", eng, w.Code, w.Body)
		}
		token := strings.TrimPrefix(link, "https://db.example.test/?vhtoken=")

		// Обмен токена: только с loopback, с секретом и без заголовков прокси.
		for name, r := range map[string]*httptest.ResponseRecorder{
			"внешний адрес":      internalCall(e, token, "203.0.113.5:4444", dbKey, nil),
			"нет секрета":        internalCall(e, token, "127.0.0.1:4444", "", nil),
			"неверный секрет":    internalCall(e, token, "127.0.0.1:4444", dbKey+"x", nil),
			"через прокси (XFF)": internalCall(e, token, "127.0.0.1:4444", dbKey, map[string]string{"X-Forwarded-For": "203.0.113.5"}),
			"через прокси (XRI)": internalCall(e, token, "127.0.0.1:4444", dbKey, map[string]string{"X-Real-IP": "203.0.113.5"}),
		} {
			if r.Code != 404 {
				t.Errorf("%s/%s: %d %s", eng, name, r.Code, r.Body)
			}
		}
		// Отказы не сжигают токен.
		ok := internalCall(e, token, "127.0.0.1:4444", dbKey, nil)
		var sess userdb.Session
		if err := json.Unmarshal(ok.Body.Bytes(), &sess); err != nil || ok.Code != 200 {
			t.Fatalf("%s: обмен: %d %s", eng, ok.Code, ok.Body)
		}
		if sess.DB != c.Database.Name || !strings.HasPrefix(sess.Username, "tmp_") || sess.Password == c.Password || ok.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("%s: %+v", eng, sess)
		}
		// Повторно токен не работает, мусор тоже.
		if r := internalCall(e, token, "127.0.0.1:4444", dbKey, nil); r.Code != 401 || !strings.Contains(r.Body.String(), "db_bad_session") {
			t.Errorf("%s: повторный обмен: %d %s", eng, r.Code, r.Body)
		}
		if r := internalCall(e, "", "127.0.0.1:4444", dbKey, nil); r.Code != 400 {
			t.Errorf("пустой токен: %d", r.Code)
		}
	}
}

func TestDatabaseFreezeAndCheckThroughAPI(t *testing.T) {
	e := newEnv(t)
	e.withDatabases(10 << 20)
	adm, _ := e.admin()
	tok := e.user(adm, "john")
	_, c, _ := e.dbCreate(tok, "mariadb", "big")
	d, err := sql.Open("mysql", c.Database.Name+":"+c.Password+"@tcp("+os.Getenv("VLADHOST_TEST_MARIADB_ADDR")+")/"+c.Database.Name)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	for _, q := range []string{
		"CREATE TABLE seq (n int)", "INSERT INTO seq VALUES (1),(2),(3),(4),(5),(6),(7),(8),(9),(10)", "CREATE TABLE bulk (v varchar(1000))",
		"INSERT INTO bulk SELECT repeat('q', 900) FROM seq a, seq b, seq c, seq d LIMIT 13000",
	} {
		if _, err := d.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	check := func() dbView {
		w := e.do("POST", fmt.Sprintf("/api/databases/%d/check", c.Database.ID), nil, tok)
		if w.Code != 200 {
			t.Fatalf("check: %d %s", w.Code, w.Body)
		}
		return decode[struct{ Database dbView }](t, w).Database
	}
	if got := check(); got.Status != "frozen" || got.SizeBytes < 10<<20 {
		t.Fatalf("база выше лимита не заморожена: %+v", got)
	}
	if _, err := d.Exec("INSERT INTO bulk VALUES ('x')"); err == nil {
		t.Fatal("запись в замороженную базу прошла")
	}
	if _, err := d.Exec("DELETE FROM bulk"); err != nil {
		t.Fatal(err)
	}
	if got := check(); got.Status != "active" {
		t.Fatalf("после очистки база не разморожена: %+v", got)
	}
	if _, err := d.Exec("INSERT INTO bulk VALUES ('again')"); err != nil {
		t.Fatalf("запись после разморозки: %v", err)
	}
}
