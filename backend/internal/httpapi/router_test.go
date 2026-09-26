package httpapi_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"kupol/internal/config"
	"kupol/internal/httpapi"
	"kupol/internal/proxyauth"
)

var secret = []byte("0123456789abcdef0123456789abcdef")

func newEngine(t *testing.T, trusted ...string) (*gin.Engine, func()) {
	t.Helper()
	st := newStack(t, func(c *config.Config) { c.TrustedProxies = trusted })
	closeDB := func() {
		sqlDB, _ := st.db.DB()
		_ = sqlDB.Close()
	}
	return st.r, closeDB
}

func do(r http.Handler, req *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func signed(method, uri, ip string, ts time.Time) *http.Request {
	req := httptest.NewRequest(method, uri, nil)
	req.RemoteAddr = "198.51.100.99:5555" // хостинг с PHP-прокси
	t := ts.Unix()
	req.Header.Set(proxyauth.HeaderIP, ip)
	req.Header.Set(proxyauth.HeaderTimestamp, strconv.FormatInt(t, 10))
	req.Header.Set(proxyauth.HeaderSignature, proxyauth.Sign(secret, t, ip, method, uri))
	return req
}

type health struct {
	Status   string `json:"status"`
	DB       string `json:"db"`
	Version  string `json:"version"`
	ClientIP string `json:"client_ip"`
	IPSource string `json:"ip_source"`
}

func decodeHealth(t *testing.T, w *httptest.ResponseRecorder) health {
	t.Helper()
	var h health
	if err := json.Unmarshal(w.Body.Bytes(), &h); err != nil {
		t.Fatalf("json: %v (%s)", err, w.Body.String())
	}
	return h
}

type apiError struct {
	Error struct {
		Code      string `json:"code"`
		Message   string `json:"message"`
		RequestID string `json:"request_id"`
	} `json:"error"`
}

func decodeError(t *testing.T, w *httptest.ResponseRecorder) apiError {
	t.Helper()
	var e apiError
	if err := json.Unmarshal(w.Body.Bytes(), &e); err != nil {
		t.Fatalf("json: %v (%s)", err, w.Body.String())
	}
	return e
}

func TestHealthDirect(t *testing.T) {
	r, _ := newEngine(t)
	for _, path := range []string{"/health", "/api/health"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.RemoteAddr = "192.0.2.10:4000"
		w := do(r, req)
		if w.Code != 200 {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body)
		}
		h := decodeHealth(t, w)
		if h.Status != "ok" || h.DB != "ok" || h.ClientIP != "192.0.2.10" || h.IPSource != "direct" {
			t.Errorf("%s: %+v", path, h)
		}
		if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
			t.Errorf("Content-Type %q", ct)
		}
	}
}

func TestHealthViaSignedProxy(t *testing.T) {
	r, _ := newEngine(t)
	w := do(r, signed("GET", "/api/health", "203.0.113.7", time.Now()))
	if w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	h := decodeHealth(t, w)
	if h.ClientIP != "203.0.113.7" || h.IPSource != "proxy" {
		t.Fatalf("%+v", h)
	}
}

func TestHealthIPv6ViaSignedProxy(t *testing.T) {
	r, _ := newEngine(t)
	w := do(r, signed("GET", "/api/health", "2001:db8::42", time.Now()))
	if h := decodeHealth(t, w); h.ClientIP != "2001:db8::42" || h.IPSource != "proxy" {
		t.Fatalf("%+v", h)
	}
}

func TestSignatureBoundToQueryString(t *testing.T) {
	r, _ := newEngine(t)
	w := do(r, signed("GET", "/api/health?a=1", "203.0.113.7", time.Now()))
	if w.Code != 200 || decodeHealth(t, w).IPSource != "proxy" {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
}

func TestUnsignedIPHeaderIgnored(t *testing.T) {
	r, _ := newEngine(t)
	req := httptest.NewRequest("GET", "/api/health", nil)
	req.RemoteAddr = "192.0.2.10:4000"
	req.Header.Set(proxyauth.HeaderIP, "1.2.3.4") // подделка без подписи
	h := decodeHealth(t, do(r, req))
	if h.ClientIP != "192.0.2.10" || h.IPSource != "direct" {
		t.Fatalf("подделанный IP принят: %+v", h)
	}
}

func TestBadSignatureRejected(t *testing.T) {
	r, _ := newEngine(t)

	forged := signed("GET", "/api/health", "203.0.113.7", time.Now())
	forged.Header.Set(proxyauth.HeaderIP, "1.2.3.4")

	stale := signed("GET", "/api/health", "203.0.113.7", time.Now().Add(-5*time.Minute))

	otherMethod := signed("GET", "/api/health", "203.0.113.7", time.Now())
	otherMethod.Method = http.MethodPost

	for name, req := range map[string]*http.Request{"подделанный IP": forged, "просрочено": stale, "другой метод": otherMethod} {
		w := do(r, req)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("%s: код %d", name, w.Code)
			continue
		}
		if e := decodeError(t, w); e.Error.Code != "bad_proxy_signature" {
			t.Errorf("%s: %+v", name, e)
		}
	}
}

func TestForwardedForOnlyFromTrustedProxy(t *testing.T) {
	// Обратный прокси перед Go (nginx на VPS) доверенный.
	r, _ := newEngine(t, "10.0.0.0/8")

	req := httptest.NewRequest("GET", "/health", nil)
	req.RemoteAddr = "10.1.2.3:5000"
	req.Header.Set("X-Forwarded-For", "203.0.113.50")
	if h := decodeHealth(t, do(r, req)); h.ClientIP != "203.0.113.50" || h.IPSource != "direct" {
		t.Fatalf("XFF от доверенного не учтён: %+v", h)
	}

	// А недоверенный клиент подделать XFF не может.
	req = httptest.NewRequest("GET", "/health", nil)
	req.RemoteAddr = "192.0.2.10:4000"
	req.Header.Set("X-Forwarded-For", "203.0.113.50")
	if h := decodeHealth(t, do(r, req)); h.ClientIP != "192.0.2.10" {
		t.Fatalf("XFF от недоверенного принят: %+v", h)
	}
}

func TestForwardedForIgnoredByDefault(t *testing.T) {
	r, _ := newEngine(t) // доверенных прокси нет
	req := httptest.NewRequest("GET", "/health", nil)
	req.RemoteAddr = "192.0.2.10:4000"
	req.Header.Set("X-Forwarded-For", "203.0.113.50")
	req.Header.Set("X-Real-IP", "203.0.113.51")
	if h := decodeHealth(t, do(r, req)); h.ClientIP != "192.0.2.10" {
		t.Fatalf("заголовки приняты без доверенных прокси: %+v", h)
	}
}

func TestSignatureHeadersStrippedFromHandlers(t *testing.T) {
	r, _ := newEngine(t)
	var seen http.Header
	r.GET("/__echo", func(c *gin.Context) {
		seen = c.Request.Header.Clone()
		c.Status(204)
	})
	do(r, signed("GET", "/__echo", "203.0.113.7", time.Now()))
	for _, k := range []string{proxyauth.HeaderIP, proxyauth.HeaderTimestamp, proxyauth.HeaderSignature} {
		if seen.Get(k) != "" {
			t.Errorf("заголовок %s дошёл до обработчика", k)
		}
	}
}

func TestNotFoundAndMethodNotAllowed(t *testing.T) {
	r, _ := newEngine(t)

	w := do(r, httptest.NewRequest("GET", "/api/nope", nil))
	if w.Code != 404 {
		t.Fatalf("%d", w.Code)
	}
	if e := decodeError(t, w); e.Error.Code != "not_found" || e.Error.Message != "Дело не найдено" {
		t.Errorf("%+v", e)
	}

	w = do(r, httptest.NewRequest("POST", "/api/health", nil))
	if w.Code != 405 {
		t.Fatalf("%d", w.Code)
	}
	if e := decodeError(t, w); e.Error.Code != "method_not_allowed" {
		t.Errorf("%+v", e)
	}
}

func TestRequestID(t *testing.T) {
	r, _ := newEngine(t)

	w := do(r, httptest.NewRequest("GET", "/health", nil))
	gen := w.Header().Get("X-Request-Id")
	if len(gen) != 16 {
		t.Errorf("сгенерированный id: %q", gen)
	}

	req := httptest.NewRequest("GET", "/health", nil)
	req.Header.Set("X-Request-Id", "abcd1234-ef56")
	if got := do(r, req).Header().Get("X-Request-Id"); got != "abcd1234-ef56" {
		t.Errorf("валидный id не сохранён: %q", got)
	}

	req = httptest.NewRequest("GET", "/health", nil)
	req.Header.Set("X-Request-Id", "bad id\r\nX-Injected: 1")
	got := do(r, req).Header().Get("X-Request-Id")
	if got == "" || strings.ContainsAny(got, " \r\n") {
		t.Errorf("небезопасный id пропущен: %q", got)
	}

	w = do(r, httptest.NewRequest("GET", "/api/nope", nil))
	if e := decodeError(t, w); e.Error.RequestID == "" || e.Error.RequestID != w.Header().Get("X-Request-Id") {
		t.Errorf("request_id в теле ошибки: %+v", e)
	}
}

func TestSecurityHeaders(t *testing.T) {
	r, _ := newEngine(t)
	w := do(r, httptest.NewRequest("GET", "/health", nil))
	for k, want := range map[string]string{
		"X-Content-Type-Options": "nosniff",
		"Referrer-Policy":        "no-referrer",
		"Cache-Control":          "no-store",
	} {
		if got := w.Header().Get(k); got != want {
			t.Errorf("%s = %q", k, got)
		}
	}
}

func TestPanicRecovered(t *testing.T) {
	r, _ := newEngine(t)
	r.GET("/__panic", func(c *gin.Context) { panic("boom") })
	w := do(r, httptest.NewRequest("GET", "/__panic", nil))
	if w.Code != 500 {
		t.Fatalf("%d", w.Code)
	}
	if e := decodeError(t, w); e.Error.Code != "internal" || e.Error.Message != "Сбой архива" {
		t.Errorf("%+v", e)
	}
	// Сервер продолжает работать.
	if w := do(r, httptest.NewRequest("GET", "/health", nil)); w.Code != 200 {
		t.Errorf("после паники: %d", w.Code)
	}
}

func TestBodyLimit(t *testing.T) {
	r, _ := newEngine(t)
	r.POST("/__read", func(c *gin.Context) {
		if _, err := io.Copy(io.Discard, c.Request.Body); err != nil {
			httpapi.Fail(c, http.StatusRequestEntityTooLarge, httpapi.CodeTooLarge, "Слишком большой запрос")
			return
		}
		c.Status(204)
	})

	// Известная длина: отсекается сразу.
	big := strings.NewReader(strings.Repeat("x", httpapi.MaxBodyBytes+1))
	if w := do(r, httptest.NewRequest("POST", "/__read", big)); w.Code != 413 {
		t.Errorf("Content-Length: %d", w.Code)
	}

	// Неизвестная длина (chunked): отсекается при чтении.
	req := httptest.NewRequest("POST", "/__read", io.NopCloser(strings.NewReader(strings.Repeat("x", httpapi.MaxBodyBytes+1))))
	req.ContentLength = -1
	if w := do(r, req); w.Code != 413 {
		t.Errorf("chunked: %d", w.Code)
	}

	ok := strings.NewReader(strings.Repeat("x", 1024))
	if w := do(r, httptest.NewRequest("POST", "/__read", ok)); w.Code != 204 {
		t.Errorf("нормальное тело: %d", w.Code)
	}
}

func TestHealthReportsDBFailure(t *testing.T) {
	r, closeDB := newEngine(t)
	closeDB()
	w := do(r, httptest.NewRequest("GET", "/health", nil))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if h := decodeHealth(t, w); h.Status != "fail" || h.DB != "unavailable" {
		t.Errorf("%+v", h)
	}
}
