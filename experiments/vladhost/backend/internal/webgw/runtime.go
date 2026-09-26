package webgw

import (
	"context"
	"errors"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"vladhost/internal/runtimecfg"
)

const (
	phpTimeout     = 65 * time.Second // на секунду больше max_execution_time пула
	appTimeout     = 65 * time.Second
	maxRequestBody = 64 << 20
)

type cachedRuntime struct {
	mod, size int64
	cfg       runtimecfg.Config
}

// runtime возвращает среду выполнения сайта; файл перечитывается, только когда он изменился.
func (h *Handler) runtime(site string) runtimecfg.Config {
	dir := filepath.Join(h.opts.Root, site)
	mod, size := runtimecfg.Modified(dir)
	if mod == 0 {
		h.runtimeCache.Delete(site)
		return runtimecfg.Config{Runtime: runtimecfg.Static}
	}
	if v, ok := h.runtimeCache.Load(site); ok {
		if c := v.(*cachedRuntime); c.mod == mod && c.size == size {
			return c.cfg
		}
	}
	cfg := runtimecfg.Load(dir)
	h.runtimeCache.Store(site, &cachedRuntime{mod: mod, size: size, cfg: cfg})
	return cfg
}

func (h *Handler) sockDir() string {
	if h.opts.PHPSocketDir != "" {
		return h.opts.PHPSocketDir
	}
	return "/run/vhphp"
}

// --- Node.js и Python: обратный прокси к приложению ---

// serveApp отдаёт весь сайт приложению, которое слушает 127.0.0.1:{порт}. Порт берётся из проверенного числа, а не из файла как строка.
func (h *Handler) serveApp(w http.ResponseWriter, r *http.Request, q *request, rt runtimecfg.Config) {
	target := &url.URL{Scheme: "http", Host: net.JoinHostPort("127.0.0.1", strconv.Itoa(rt.Port))}
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)
	proxy := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(target)
			pr.Out.Host = pr.In.Host // приложение видит настоящее имя сайта
			pr.Out.Header.Del("X-Forwarded-For")
			pr.Out.Header.Set("X-Forwarded-For", clientIP(pr.In))
			pr.Out.Header.Set("X-Forwarded-Proto", "https")
			pr.Out.Header.Set("X-Forwarded-Host", pr.In.Host)
		},
		Transport: &http.Transport{
			DialContext:           (&net.Dialer{Timeout: 3 * time.Second}).DialContext,
			ResponseHeaderTimeout: appTimeout,
			DisableKeepAlives:     true,
		},
		ErrorHandler: func(rw http.ResponseWriter, _ *http.Request, err error) {
			var mb *http.MaxBytesError
			switch {
			case errors.As(err, &mb):
				q.fail(http.StatusRequestEntityTooLarge)
			case errors.Is(err, context.Canceled):
				// клиент ушёл: отвечать некому
			default:
				q.h.logError(q.site, q.r, q.host, "app_unavailable", err.Error())
				q.fail(http.StatusServiceUnavailable)
			}
		},
	}
	proxy.ServeHTTP(w, r)
}

// --- PHP: FastCGI к пулу сайта ---

// phpScript ищет в пути скрипт .php: «/a/index.php/x/y» → скрипт /a/index.php, PATH_INFO /x/y. Скрытые части и не-файлы не подходят.
func (q *request) phpScript(p string) (script, pathInfo string, ok bool) {
	segs := strings.Split(strings.Trim(p, "/"), "/")
	cur := ""
	for i, seg := range segs {
		if seg == "" || strings.HasPrefix(seg, ".") {
			return "", "", false
		}
		cur += "/" + seg
		if strings.HasSuffix(strings.ToLower(seg), ".php") {
			fi, err := q.root.Lstat(rel(cur))
			if err != nil || !fi.Mode().IsRegular() {
				return "", "", false
			}
			if i+1 < len(segs) {
				pathInfo = "/" + strings.Join(segs[i+1:], "/")
			}
			if strings.HasSuffix(p, "/") && pathInfo == "" {
				pathInfo = "/"
			}
			return cur, pathInfo, true
		}
		if fi, err := q.root.Lstat(rel(cur)); err != nil || !fi.IsDir() {
			return "", "", false
		}
	}
	return "", "", false
}

func isPHPFile(name string) bool { return strings.HasSuffix(strings.ToLower(name), ".php") }

// runPHP выполняет скрипт в пуле сайта. Настоящий путь скрипта собирается из корня сайта и проверенного пути URL.
func (q *request) runPHP(script, pathInfo, query string) {
	h := q.h
	docRoot := filepath.Join(h.opts.Root, q.site, "public")
	if q.base != "" {
		q.fail(http.StatusNotFound) // PHP работает только для основного адреса сайта, не для папок и поддоменов
		return
	}
	r := q.r
	_, port, _ := net.SplitHostPort(r.RemoteAddr)
	params := map[string]string{
		"GATEWAY_INTERFACE": "CGI/1.1",
		"SERVER_SOFTWARE":   "vladhost",
		"SERVER_PROTOCOL":   r.Proto,
		"REQUEST_METHOD":    r.Method,
		"SCRIPT_FILENAME":   filepath.Join(docRoot, filepath.FromSlash(strings.TrimPrefix(script, "/"))),
		"SCRIPT_NAME":       script,
		"DOCUMENT_ROOT":     docRoot,
		"DOCUMENT_URI":      script,
		"REQUEST_URI":       r.URL.RequestURI(),
		"QUERY_STRING":      query,
		"REMOTE_ADDR":       clientIP(r),
		"REMOTE_PORT":       port,
		"SERVER_NAME":       q.host,
		"SERVER_PORT":       "443",
		"HTTPS":             "on",
		"REDIRECT_STATUS":   "200",
		"CONTENT_TYPE":      r.Header.Get("Content-Type"),
		"CONTENT_LENGTH":    "",
	}
	if pathInfo != "" {
		params["PATH_INFO"] = pathInfo
	}
	if r.ContentLength > 0 {
		params["CONTENT_LENGTH"] = strconv.FormatInt(r.ContentLength, 10)
	}
	for k, vs := range r.Header {
		lk := strings.ToLower(k)
		if lk == "proxy" || lk == "content-type" || lk == "content-length" { // HTTP_PROXY превращается в переменную окружения (httpoxy)
			continue
		}
		params["HTTP_"+strings.ToUpper(strings.ReplaceAll(k, "-", "_"))] = strings.Join(vs, ", ")
	}
	params["HTTP_HOST"] = r.Host // Go выносит Host из заголовков, а PHP-приложения (WordPress) берут адрес именно из HTTP_HOST
	r.Body = http.MaxBytesReader(q.w, r.Body, maxRequestBody)
	rt := h.runtime(q.site)
	started, err := fcgiServe(r.Context(), q.w, r, rt.Socket(h.sockDir()), params, phpTimeout)
	if err == nil {
		return
	}
	if started {
		log.Printf("шлюз: php: обрыв ответа %s: %v", q.site, err)
		return
	}
	h.logError(q.site, r, q.host, "php_unavailable", err.Error())
	if errors.Is(err, errFCGIDown) {
		q.fail(http.StatusServiceUnavailable)
		return
	}
	q.fail(http.StatusInternalServerError)
}

// tryPHP вызывается, когда путь не совпал с файлом: возможно, это «/index.php/что-то». Возвращает true, если запрос обработан.
func (q *request) tryPHP(p, query string) bool {
	if q.rt.Runtime != runtimecfg.PHP {
		return false
	}
	script, info, ok := q.phpScript(p)
	if !ok {
		return false
	}
	base := path.Base(script)
	if q.eff.Denied(base) {
		q.fail(http.StatusForbidden)
		return true
	}
	if need := q.eff.Auth(base); need != nil && !q.authorize(need) {
		return true
	}
	q.runPHP(script, info, query)
	return true
}
