package httpapi_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"kupol/internal/accounts"
	"kupol/internal/config"
	"kupol/internal/documents"
	"kupol/internal/httpapi"
	"kupol/internal/passwords"
	"kupol/internal/ratelimit"
	"kupol/internal/testutil"
)

const testOrigin = "https://kupol.test"

// stack — настоящий роутер поверх настоящей БД и настоящего сервиса аккаунтов
// (argon2id с облегчёнными параметрами, чтобы тесты шли быстро).
type stack struct {
	r       *gin.Engine
	db      *gorm.DB
	svc     *accounts.Service
	docs    *documents.Service
	limiter *ratelimit.Limiter
	cfg     config.Config
	clients int
}

func newStack(t *testing.T, tweak func(*config.Config)) *stack {
	t.Helper()
	cfg := config.Config{
		Env:         "dev",
		ProxySecret: secret,
		SiteOrigin:  testOrigin,
		Limits:      config.DefaultLimits(),
	}
	if tweak != nil {
		tweak(&cfg)
	}
	db := testutil.NewMigratedDB(t)
	hasher, err := passwords.NewHasher(passwords.Params{MemoryKiB: 64, Iterations: 1, Parallelism: 1}, 4)
	if err != nil {
		t.Fatal(err)
	}
	limiter := ratelimit.New(nil)
	svc, err := accounts.NewService(accounts.Options{DB: db, Hasher: hasher, Limiter: limiter, Limits: cfg.Limits, Log: testutil.Logger()})
	if err != nil {
		t.Fatal(err)
	}
	docs := documents.NewService(db, testutil.Logger(), nil)
	r, err := httpapi.New(httpapi.Deps{Config: cfg, DB: db, Log: testutil.Logger(), Accounts: svc, Documents: docs, Limiter: limiter})
	if err != nil {
		t.Fatal(err)
	}
	return &stack{r: r, db: db, svc: svc, docs: docs, limiter: limiter, cfg: cfg}
}

// client — «браузер» с хранилищем кук поверх тестового роутера.
type client struct {
	t      *testing.T
	st     *stack
	cookie *http.Cookie // текущая кука сессии
	ip     string
	origin string
}

// newClient создаёт «браузер» с собственным IP: регистрации разных клиентов не упираются в лимит по IP.
func (st *stack) newClient(t *testing.T) *client {
	st.clients++
	return &client{t: t, st: st, ip: fmt.Sprintf("203.0.113.%d", 10+st.clients), origin: testOrigin}
}

type response struct {
	*httptest.ResponseRecorder
}

func (r response) json() map[string]any {
	var m map[string]any
	if err := json.Unmarshal(r.Body.Bytes(), &m); err != nil {
		panic("не JSON: " + r.Body.String())
	}
	return m
}

func (r response) errCode() string {
	e, _ := r.json()["error"].(map[string]any)
	code, _ := e["code"].(string)
	return code
}

func (r response) fields() map[string]any {
	e, _ := r.json()["error"].(map[string]any)
	f, _ := e["fields"].(map[string]any)
	return f
}

// setCookies возвращает Set-Cookie ответа для куки сессии.
func (r response) sessionSetCookie() *http.Cookie {
	for _, c := range r.Result().Cookies() {
		if c.Name == httpapi.SessionCookieName {
			return c
		}
	}
	return nil
}

func (c *client) do(method, path string, body any) response {
	c.t.Helper()
	var rd *strings.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			c.t.Fatal(err)
		}
		rd = strings.NewReader(string(raw))
	} else {
		rd = strings.NewReader("")
	}
	req := httptest.NewRequest(method, path, rd)
	req.RemoteAddr = c.ip + ":4000"
	req.Header.Set("User-Agent", "test-browser")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.origin != "" && method != http.MethodGet && method != http.MethodHead {
		req.Header.Set("Origin", c.origin)
	}
	if c.cookie != nil {
		req.AddCookie(&http.Cookie{Name: c.cookie.Name, Value: c.cookie.Value})
	}
	w := httptest.NewRecorder()
	c.st.r.ServeHTTP(w, req)
	// хранилище кук браузера: Set-Cookie обновляет или стирает куку
	for _, sc := range w.Result().Cookies() {
		if sc.Name != httpapi.SessionCookieName {
			continue
		}
		if sc.MaxAge < 0 || sc.Value == "" {
			c.cookie = nil
		} else {
			c.cookie = sc
		}
	}
	return response{w}
}

// register проходит анкету и регистрируется; возвращает ответ.
func (c *client) register(login, password string) response {
	c.t.Helper()
	cp := c.do("GET", "/api/auth/captcha", nil)
	if cp.Code != 200 {
		c.t.Fatalf("captcha: %d %s", cp.Code, cp.Body)
	}
	m := cp.json()
	return c.do("POST", "/api/auth/register", map[string]any{
		"login": login, "password": password,
		"captcha_id": m["id"], "captcha_answer": captchaAnswer(c.t, m["question"].(string)),
	})
}

// captchaAnswer — ответы на вопросы анкеты, как их прочитал бы человек на главной странице.
func captchaAnswer(t *testing.T, question string) string {
	t.Helper()
	for _, p := range []struct{ contains, answer string }{
		{"В каком году основан", "1974"},
		{"Какой гриф", "Форма КУПОЛ-1"},
		{"Как сокращённо", "ЦАК"},
		{"Впишите пропущенное слово", "объектами"},
		{"С какого слова начинается", "Комитет"},
		{"Сколько букв", "5"},
	} {
		if strings.Contains(question, p.contains) {
			return p.answer
		}
	}
	t.Fatalf("неизвестный вопрос анкеты: %q", question)
	return ""
}
