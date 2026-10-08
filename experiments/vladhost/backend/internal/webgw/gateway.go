// Package webgw — веб-шлюз сайтов пользователей: отдаёт статику, понимает .htaccess и показывает страницы
// по умолчанию (пустой сайт, 403, 404 и другие ошибки). Работает без root и без доступа к БД: читает только
// каталог сайтов, все пути открывает через os.Root (выйти за каталог сайта и пройти по симлинку нельзя).
package webgw

import (
	"bytes"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"vladhost/internal/i18n"
	"vladhost/internal/runtimecfg"
	"vladhost/internal/sitecfg"
	"vladhost/internal/sitelog"
	"vladhost/internal/sitestats"
	"vladhost/internal/webgw/htaccess"
)

const (
	maxPasses     = 10 // внутренних перезапусков при переписывании URL (как LimitInternalRecursion в Apache)
	maxChainDepth = 24 // глубина каталогов, в которых ищем .htaccess
	maxHtpasswd   = 256 << 10
	maxListing    = 2000
	maxErrDoc     = 1 << 20
)

type Options struct {
	Root       string // {Root}/{host}/public
	BaseDomain string // vladinc.ru
	// DomainsDir — папка привязок своих доменов: файл {домен} содержит адрес сайта. Пусто — свои домены не обслуживаются.
	DomainsDir string
	// LogDir — куда писать журналы сайтов (доступ и ошибки). Пусто — журналы не ведутся.
	LogDir string
	// PHPSocketDir — каталог сокетов пулов PHP-FPM сайтов (по умолчанию /run/vhphp).
	PHPSocketDir string
}

type Handler struct {
	opts    Options
	hostRe  *regexp.Regexp
	subRe   *regexp.Regexp // поддомен сайта: {метка}.{сайт}.{пользователь}.{домен}
	baseLow string
	cache   sync.Map // ключ host|dir → *cachedConfig
	cacheN  int
	cacheMu sync.Mutex

	settingsCache sync.Map // адрес сайта → *cachedSettings
	runtimeCache  sync.Map // адрес сайта → *cachedRuntime

	authMu    sync.Mutex
	authFails map[string]*failure
	authSem   chan struct{} // ограничивает число одновременных проверок пароля: bcrypt дорог

	logs  *sitelog.Writer       // nil — журналы не ведутся
	stats *sitestats.Aggregator // nil — статистика не ведётся
}

type failure struct {
	n     int
	since time.Time
}

type cachedConfig struct {
	mod  time.Time
	size int64
	cfg  *htaccess.Config
}

func New(opts Options) *Handler {
	h := &Handler{
		opts:      opts,
		hostRe:    regexp.MustCompile(`^([a-z0-9-]+)\.([a-z0-9-]+)\.` + regexp.QuoteMeta(strings.ToLower(opts.BaseDomain)) + `$`),
		subRe:     regexp.MustCompile(`^[a-z0-9-]+\.([a-z0-9-]+)\.([a-z0-9-]+)\.` + regexp.QuoteMeta(strings.ToLower(opts.BaseDomain)) + `$`),
		baseLow:   strings.ToLower(opts.BaseDomain),
		authFails: map[string]*failure{},
		authSem:   make(chan struct{}, 4),
	}
	if opts.LogDir != "" {
		// Журналы вторичны: если каталог недоступен, сайты всё равно отдаются.
		if w, err := sitelog.NewWriter(opts.LogDir); err != nil {
			log.Printf("шлюз: журналы сайтов отключены: %v", err)
		} else {
			h.logs = w
		}
		if a, err := sitestats.New(opts.LogDir); err != nil {
			log.Printf("шлюз: статистика сайтов отключена: %v", err)
		} else {
			h.stats = a
		}
	}
	return h
}

// request — состояние обработки одного запроса.
type request struct {
	h     *Handler
	w     http.ResponseWriter
	r     *http.Request
	lang  i18n.Lang
	host  string // хост запроса (для редиректов и переменных .htaccess)
	site  string // адрес сайта на нашем домене, чьи файлы отдаём (совпадает с host, если домен не свой)
	base  string // папка сайта, которую отдаёт это имя (или корневая папка из настроек); пусто — весь public
	set   sitecfg.Settings
	root  *os.Root
	rt    runtimecfg.Config   // среда выполнения (PHP, приложение); для папок и поддоменов всегда статика
	query string              // строка запроса после переписывания URL (для PHP)
	eff   *htaccess.Effective // может быть nil до построения цепочки
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	lang := i18n.FromAcceptLanguage(r.Header.Get("Accept-Language"))
	host := normalizeHost(r.Host)
	var (
		sw       *statusWriter // появляется, когда сайт найден: журнал ведётся только для существующих сайтов
		siteHost string
	)
	defer func() {
		if p := recover(); p != nil {
			log.Printf("шлюз: паника при %s %s: %v", r.Method, r.URL.Path, p)
			if sw != nil {
				errorPage(sw, r, lang, http.StatusInternalServerError)
				h.logError(siteHost, r, host, "server_error", "panic")
			} else {
				errorPage(w, r, lang, http.StatusInternalServerError)
			}
		}
		if sw != nil {
			h.logAccess(siteHost, r, host, sw, start)
		}
	}()
	siteHost, base, ok := h.resolveSite(host)
	if !ok {
		noSitePage(w, r, lang)
		return
	}
	dir, ok := h.siteDir(siteHost)
	if !ok {
		noSitePage(w, r, lang)
		return
	}
	set := h.settings(siteHost)
	if base == "" {
		base = set.RootDir // имя без своей папки отдаёт корень сайта из настроек (по умолчанию весь public)
	}

	sw = &statusWriter{ResponseWriter: w}
	if set.HSTS {
		sw.Header().Set("Strict-Transport-Security", "max-age=15552000")
	}
	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		if target := h.wwwTarget(host, siteHost, set.WWW); target != "" {
			sw.Header().Set("Location", "https://"+target+r.URL.RequestURI())
			sw.Header().Set("Cache-Control", "max-age=3600") // настройку можно выключить: браузер не должен помнить редирект вечно
			sw.WriteHeader(http.StatusMovedPermanently)
			return
		}
	}

	siteRoot, err := os.OpenRoot(dir)
	if err != nil {
		noSitePage(sw, r, lang)
		return
	}
	defer func() { _ = siteRoot.Close() }()
	root := siteRoot
	if base != "" {
		// Имя привязано к папке сайта: она и есть его корень. OpenRoot внутри Root не выпускает наружу ни «..», ни ссылки.
		sub, err := siteRoot.OpenRoot(base)
		if err != nil {
			noSitePage(sw, r, lang)
			return
		}
		defer func() { _ = sub.Close() }()
		root = sub
	}

	q := &request{h: h, w: sw, r: r, lang: lang, host: host, site: siteHost, base: base, set: set, root: root, rt: runtimecfg.Config{Runtime: runtimecfg.Static}}
	if base == "" { // PHP и приложения работают на основном адресе сайта; поддомены и папки остаются статикой
		q.rt = h.runtime(siteHost)
	}
	switch q.rt.Runtime {
	case runtimecfg.Node, runtimecfg.Python:
		h.serveApp(sw, r, q, q.rt)
		return
	case runtimecfg.PHP:
		// Скрипты принимают любые методы; статические файлы отдаются как обычно.
	default:
		switch r.Method {
		case http.MethodGet, http.MethodHead:
		case http.MethodOptions:
			sw.Header().Set("Allow", "GET, HEAD, OPTIONS")
			sw.WriteHeader(http.StatusNoContent)
			return
		default:
			sw.Header().Set("Allow", "GET, HEAD, OPTIONS")
			q.fail(http.StatusMethodNotAllowed)
			return
		}
	}
	p := r.URL.Path
	if p == "" || p[0] != '/' || strings.ContainsRune(p, 0) {
		q.fail(http.StatusBadRequest)
		return
	}
	q.serve(cleanPath(p), r.URL.RawQuery)
}

func normalizeHost(h string) string {
	if host, _, err := net.SplitHostPort(h); err == nil {
		h = host
	}
	return strings.TrimSuffix(strings.ToLower(h), ".")
}

var customHostRe = regexp.MustCompile(`^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z][a-z0-9-]{0,61}[a-z0-9]$`)

// resolveSite по хосту запроса находит адрес сайта и папку внутри него. Хост — это либо адрес сайта
// ({сайт}.{пользователь}.домен, вся папка сайта), либо поддомен сайта ({метка}.{сайт}.{пользователь}.домен), либо
// свой домен пользователя: тогда сайт и папка берутся из файла привязки, который пишет панель.
func (h *Handler) resolveSite(host string) (site, dir string, ok bool) {
	if h.hostRe.MatchString(host) {
		return host, "", true
	}
	isSub := h.subRe.MatchString(host)
	if h.opts.DomainsDir == "" || len(host) > 253 || !customHostRe.MatchString(host) ||
		(!isSub && (host == h.baseLow || strings.HasSuffix(host, "."+h.baseLow))) {
		return "", "", false
	}
	root, err := os.OpenRoot(h.opts.DomainsDir)
	if err != nil {
		return "", "", false
	}
	defer func() { _ = root.Close() }()
	data, err := readFile(root, host, 512)
	if err != nil {
		return "", "", false
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	site = strings.TrimSpace(lines[0])
	if !h.hostRe.MatchString(site) { // содержимое привязки не доверяем: оно попадает в путь к файлам
		return "", "", false
	}
	// Поддомен принадлежит своему сайту и никакому другому, что бы ни лежало в файле привязки.
	if isSub && !strings.HasSuffix(host, "."+site) {
		return "", "", false
	}
	if len(lines) > 1 {
		dir, ok = cleanMappingDir(lines[1])
		if !ok {
			return "", "", false
		}
	}
	return site, dir, true
}

// cleanMappingDir проверяет папку из файла привязки: только вниз от корня сайта, без «..» и скрытых частей.
func cleanMappingDir(d string) (string, bool) {
	d = strings.TrimSpace(d)
	if d == "" {
		return "", true
	}
	if len(d) > 200 || strings.ContainsAny(d, "\\\x00") || strings.HasPrefix(d, "/") || path.Clean(d) != d {
		return "", false
	}
	for _, seg := range strings.Split(d, "/") {
		if seg == "" || seg == ".." || strings.HasPrefix(seg, ".") {
			return "", false
		}
	}
	return d, true
}

// siteDir возвращает каталог public сайта по имени хоста. Имя проверяется по строгому шаблону: оно попадает в путь.
func (h *Handler) siteDir(host string) (string, bool) {
	if !h.hostRe.MatchString(host) {
		return "", false
	}
	dir := filepath.Join(h.opts.Root, host, "public")
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		return "", false
	}
	return dir, true
}

// cleanPath нормализует путь URL: "/a/../b" → "/b", хвостовой "/" сохраняется. Выше корня выйти нельзя.
func cleanPath(p string) string {
	trail := strings.HasSuffix(p, "/")
	c := path.Clean("/" + p)
	if trail && c != "/" {
		c += "/"
	}
	return c
}

func rel(urlPath string) string {
	r := strings.Trim(urlPath, "/")
	if r == "" {
		return "."
	}
	return r
}

// --- цепочка .htaccess ---

func (q *request) effective(urlPath string) *htaccess.Effective {
	var chain []htaccess.Dir
	add := func(dirRel string) {
		if cfg := q.h.loadConfig(q.root, q.site+"|"+q.base, dirRel); cfg != nil {
			url := "/"
			if dirRel != "." {
				url = "/" + dirRel + "/"
			}
			chain = append(chain, htaccess.Dir{URL: url, Cfg: cfg})
		}
	}
	add(".")
	cur := ""
	for i, c := range strings.Split(strings.Trim(urlPath, "/"), "/") {
		if c == "" || i >= maxChainDepth {
			break
		}
		next := c
		if cur != "" {
			next = cur + "/" + c
		}
		fi, err := q.root.Lstat(next)
		if err != nil || !fi.IsDir() {
			break
		}
		add(next)
		cur = next
	}
	return htaccess.MergeWith(chain, q.defaults())
}

func (h *Handler) loadConfig(root *os.Root, host, dirRel string) *htaccess.Config {
	name := ".htaccess"
	if dirRel != "." {
		name = dirRel + "/.htaccess"
	}
	fi, err := root.Lstat(name)
	if err != nil || !fi.Mode().IsRegular() {
		return nil
	}
	key := host + "|" + dirRel
	if v, ok := h.cache.Load(key); ok {
		c := v.(*cachedConfig)
		if c.mod.Equal(fi.ModTime()) && c.size == fi.Size() {
			return c.cfg
		}
	}
	cfg := htaccess.Parse(nil)
	if fi.Size() > htaccess.MaxSize {
		cfg = htaccess.Parse(make([]byte, htaccess.MaxSize+1)) // диагностика «слишком большой файл»
	} else if data, err := readFile(root, name, htaccess.MaxSize); err == nil {
		cfg = htaccess.Parse(data)
	}
	h.cacheMu.Lock()
	if h.cacheN > 4000 { // простая защита памяти: при переполнении кэш очищается целиком
		h.cache.Clear()
		h.cacheN = 0
	}
	h.cacheN++
	h.cacheMu.Unlock()
	h.cache.Store(key, &cachedConfig{mod: fi.ModTime(), size: fi.Size(), cfg: cfg})
	return cfg
}

func readFile(root *os.Root, name string, limit int64) ([]byte, error) {
	f, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	return io.ReadAll(io.LimitReader(f, limit))
}

// --- обработка запроса ---

func (q *request) env(query string) htaccess.Env {
	isFile := func(p string) bool {
		fi, err := q.root.Lstat(rel(p))
		return err == nil && fi.Mode().IsRegular()
	}
	return htaccess.Env{
		Host: q.host, Method: q.r.Method, Query: query, RemoteAddr: clientIP(q.r),
		Header: q.r.Header.Get,
		IsFile: isFile,
		IsDir: func(p string) bool {
			fi, err := q.root.Lstat(rel(p))
			return err == nil && fi.IsDir()
		},
		IsNonEmpty: func(p string) bool {
			fi, err := q.root.Lstat(rel(p))
			return err == nil && fi.Mode().IsRegular() && fi.Size() > 0
		},
	}
}

func (q *request) serve(urlPath, query string) {
	st := htaccess.State{Path: urlPath, Query: query}
	for pass := 0; ; pass++ {
		if pass >= maxPasses {
			q.fail(http.StatusInternalServerError)
			return
		}
		q.eff = q.effective(st.Path)
		if status, target, ok := q.eff.MatchRedirect(st.Path); ok {
			if status == http.StatusGone {
				q.fail(http.StatusGone)
			} else {
				q.redirect(target, status)
			}
			return
		}
		changed := false
		for _, set := range q.eff.RewriteSets {
			out, ch := set.Apply(st, q.env(st.Query))
			switch {
			case out.RedirectStatus > 0:
				q.redirect(out.RedirectURL, out.RedirectStatus)
				return
			case out.Status > 0:
				q.fail(out.Status)
				return
			}
			st, changed = out.State, changed || ch
			if out.End {
				q.finish(st)
				return
			}
		}
		if !changed {
			q.finish(st)
			return
		}
	}
}

// redirect отправляет редирект; относительный адрес превращается в абсолютный.
func (q *request) redirect(target string, status int) {
	loc := target
	if strings.HasPrefix(target, "/") && !strings.HasPrefix(target, "//") {
		loc = "https://" + q.host + target
	} else if !strings.Contains(target, "://") && !strings.HasPrefix(target, "//") {
		loc = "https://" + q.host + "/" + target
	}
	if q.eff != nil {
		q.eff.ApplyHeaders(q.w.Header(), status, "")
	}
	q.w.Header().Set("Location", loc)
	q.w.Header().Set("Cache-Control", "no-store")
	q.w.WriteHeader(status)
}

// finish отдаёт файл или страницу для итогового пути.
func (q *request) finish(st htaccess.State) {
	p := st.Path
	q.query = st.Query
	for _, seg := range strings.Split(strings.Trim(p, "/"), "/") {
		if strings.HasPrefix(seg, ".ht") || strings.EqualFold(seg, ".htaccess") {
			q.fail(http.StatusForbidden) // .htaccess и .htpasswd никогда не отдаются
			return
		}
		if strings.HasPrefix(seg, ".") && seg != ".well-known" && seg != "" {
			q.fail(http.StatusNotFound) // .git, .env и прочие скрытые файлы не выдаём и не подтверждаем их наличие
			return
		}
	}
	fi, err := q.root.Lstat(rel(p))
	if err != nil || (!fi.IsDir() && !fi.Mode().IsRegular()) {
		if q.tryPHP(p, st.Query) { // /index.php/путь: скрипт есть, а «пути» под ним нет
			return
		}
		q.notFound(p)
		return
	}
	if fi.IsDir() {
		if !strings.HasSuffix(p, "/") {
			target := p + "/"
			if st.Query != "" {
				target += "?" + st.Query
			}
			q.redirect(target, http.StatusMovedPermanently)
			return
		}
		q.directory(p)
		return
	}
	q.file(p, fi)
}

// notFound: у совсем пустого сайта корень показывает «скоро здесь будет сайт», остальное — 404.
func (q *request) notFound(p string) {
	if p == "/" && q.siteEmpty() {
		emptySitePage(q.w, q.r, q.lang)
		return
	}
	q.fail(http.StatusNotFound)
}

// siteEmpty: в корне сайта нет ни одного видимого файла (скрытые, вроде .htaccess, не считаются).
func (q *request) siteEmpty() bool {
	d, err := q.root.Open(".")
	if err != nil {
		return false
	}
	defer func() { _ = d.Close() }()
	entries, err := d.ReadDir(-1)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), ".") {
			return false
		}
	}
	return true
}

func (q *request) directory(p string) {
	dirRel := rel(p)
	for _, name := range q.eff.DirectoryIndex {
		if name == "" || strings.ContainsRune(name, '/') || strings.HasPrefix(name, ".") {
			continue
		}
		full := name
		if dirRel != "." {
			full = dirRel + "/" + name
		}
		if fi, err := q.root.Lstat(full); err == nil && fi.Mode().IsRegular() {
			q.file(path.Join(p, name), fi)
			return
		}
	}
	if q.eff.Indexes {
		q.listing(p, dirRel)
		return
	}
	if p == "/" && q.siteEmpty() {
		emptySitePage(q.w, q.r, q.lang)
		return
	}
	q.fail(http.StatusForbidden)
}

func (q *request) file(p string, fi fs.FileInfo) {
	base := path.Base(p)
	if q.eff.Denied(base) {
		q.fail(http.StatusForbidden)
		return
	}
	if need := q.eff.Auth(base); need != nil && !q.authorize(need) {
		return
	}
	if q.rt.Runtime == runtimecfg.PHP && isPHPFile(base) {
		q.runPHP(p, "", q.query)
		return
	}
	f, err := q.root.Open(rel(p))
	if err != nil {
		q.fail(http.StatusNotFound)
		return
	}
	defer func() { _ = f.Close() }()

	hdr := q.w.Header()
	mime, enc := q.eff.Mime(base)
	if mime == "" {
		var head [512]byte
		n, _ := io.ReadFull(f, head[:])
		mime = http.DetectContentType(head[:n])
		if _, err := f.Seek(0, io.SeekStart); err != nil {
			q.fail(http.StatusInternalServerError)
			return
		}
	}
	hdr.Set("Content-Type", mime)
	if enc != "" {
		hdr.Set("Content-Encoding", enc)
		hdr.Add("Vary", "Accept-Encoding")
	}
	hdr.Set("X-Content-Type-Options", "nosniff")
	hdr.Set("ETag", fmt.Sprintf(`W/"%x-%x"`, fi.Size(), fi.ModTime().UnixNano()))
	if age, ok := q.eff.MaxAge(strings.SplitN(mime, ";", 2)[0], fi.ModTime(), time.Now()); ok {
		secs := int64(age / time.Second)
		hdr.Set("Cache-Control", "max-age="+strconv.FormatInt(secs, 10))
		hdr.Set("Expires", time.Now().Add(age).UTC().Format(http.TimeFormat))
	}
	q.eff.ApplyHeaders(hdr, http.StatusOK, base)
	http.ServeContent(q.w, q.r, base, fi.ModTime(), f)
}

// --- ошибки ---

// fail отвечает ошибкой: сначала ErrorDocument из .htaccess, иначе страница по умолчанию.
func (q *request) fail(status int) {
	if q.eff != nil {
		if doc, ok := q.eff.ErrorDocs[status]; ok && q.errorDocument(status, doc) {
			return
		}
		q.eff.ApplyHeaders(q.w.Header(), status, "")
	}
	q.h.logError(q.site, q.r, q.host, errorCode(status), q.r.URL.Path)
	errorPage(q.w, q.r, q.lang, status)
}

func (q *request) errorDocument(status int, doc htaccess.ErrorDoc) bool {
	switch doc.Kind {
	case "url":
		q.redirect(doc.Value, http.StatusFound)
		return true
	case "text":
		h := q.w.Header()
		h.Set("Content-Type", "text/plain; charset=utf-8")
		h.Set("X-Content-Type-Options", "nosniff")
		q.eff.ApplyHeaders(h, status, "")
		q.w.WriteHeader(status)
		if q.r.Method != http.MethodHead {
			_, _ = io.WriteString(q.w, doc.Value)
		}
		return true
	case "path":
		p := cleanPath(doc.Value)
		for _, seg := range strings.Split(strings.Trim(p, "/"), "/") {
			if strings.HasPrefix(seg, ".") && seg != "" {
				return false
			}
		}
		base := path.Base(p)
		fi, err := q.root.Lstat(rel(p))
		if err != nil || !fi.Mode().IsRegular() || fi.Size() > maxErrDoc || q.eff.Denied(base) || q.eff.Auth(base) != nil {
			return false
		}
		data, err := readFile(q.root, rel(p), maxErrDoc)
		if err != nil {
			return false
		}
		mime, _ := q.eff.Mime(base)
		if mime == "" {
			mime = http.DetectContentType(data)
		}
		h := q.w.Header()
		h.Set("Content-Type", mime)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Cache-Control", "no-store")
		q.eff.ApplyHeaders(h, status, base)
		q.w.WriteHeader(status)
		if q.r.Method != http.MethodHead {
			_, _ = q.w.Write(data)
		}
		return true
	}
	return false
}

// --- Basic-аутентификация ---

func clientIP(r *http.Request) string {
	// Перед шлюзом стоит nginx на этой же машине и перезаписывает X-Forwarded-For адресом клиента.
	if h, _, err := net.SplitHostPort(r.RemoteAddr); err == nil && (h == "127.0.0.1" || h == "::1") {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			parts := strings.Split(xff, ",")
			return strings.TrimSpace(parts[len(parts)-1])
		}
	}
	h, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return h
}

const (
	authMaxFails = 10
	authWindow   = 10 * time.Minute
)

func (h *Handler) authBlocked(ip string) bool {
	h.authMu.Lock()
	defer h.authMu.Unlock()
	f := h.authFails[ip]
	if f == nil {
		return false
	}
	if time.Since(f.since) > authWindow {
		delete(h.authFails, ip)
		return false
	}
	return f.n >= authMaxFails
}

func (h *Handler) authFailed(ip string) {
	h.authMu.Lock()
	defer h.authMu.Unlock()
	if len(h.authFails) > 20000 {
		h.authFails = map[string]*failure{}
	}
	f := h.authFails[ip]
	if f == nil || time.Since(f.since) > authWindow {
		f = &failure{since: time.Now()}
		h.authFails[ip] = f
	}
	f.n++
}

// authorize проверяет Basic-учётные данные. При неудаче сам отвечает и возвращает false.
func (q *request) authorize(need *htaccess.AuthNeed) bool {
	ip := clientIP(q.r)
	if q.h.authBlocked(ip) {
		q.w.Header().Set("Retry-After", "600")
		q.fail(http.StatusTooManyRequests)
		return false
	}
	user, pass, ok := q.r.BasicAuth()
	if !ok {
		q.challenge(need)
		return false
	}
	data, err := readFile(q.root, rel(need.UserFile), maxHtpasswd)
	if err != nil {
		q.fail(http.StatusInternalServerError) // файл паролей не найден: как в Apache, это ошибка настройки
		return false
	}
	hash, exists := htaccess.ParseHtpasswd(data)[user]
	allowed := exists
	if allowed && !need.Any {
		allowed = false
		for _, u := range need.Users {
			if u == user {
				allowed = true
			}
		}
	}
	q.h.authSem <- struct{}{}
	good := allowed && htaccess.VerifyPassword(hash, pass)
	<-q.h.authSem
	if !good {
		q.h.authFailed(ip)
		q.challenge(need)
		return false
	}
	return true
}

func (q *request) challenge(need *htaccess.AuthNeed) {
	realm := strings.NewReplacer(`"`, "'", "\r", "", "\n", "").Replace(need.Realm)
	q.w.Header().Set("WWW-Authenticate", `Basic realm="`+realm+`", charset="UTF-8"`)
	q.fail(http.StatusUnauthorized)
}

// --- листинг каталога (Options +Indexes) ---

type listRow struct {
	Name, Href, Size, Mod string
	Dir                   bool
}

var listTpl = template.Must(template.New("list").Parse(`<table>
<tr><th>{{.Name}}</th><th style="text-align:right">{{.Size}}</th><th style="text-align:right">{{.Mod}}</th></tr>
{{if .Up}}<tr><td><a href="{{.Up}}">↩ {{.UpLabel}}</a></td><td></td><td></td></tr>{{end}}
{{range .Rows}}<tr><td>{{if .Dir}}📁{{else}}📄{{end}} <a href="{{.Href}}">{{.Name}}</a></td><td class="n">{{.Size}}</td><td class="n">{{.Mod}}</td></tr>
{{else}}<tr><td colspan="3">{{$.Empty}}</td></tr>{{end}}
</table>`))

func (q *request) listing(p, dirRel string) {
	d, err := q.root.Open(dirRel)
	if err != nil {
		q.fail(http.StatusForbidden)
		return
	}
	defer func() { _ = d.Close() }()
	entries, err := d.ReadDir(-1)
	if err != nil {
		q.fail(http.StatusInternalServerError)
		return
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].IsDir() != entries[j].IsDir() {
			return entries[i].IsDir()
		}
		return strings.ToLower(entries[i].Name()) < strings.ToLower(entries[j].Name())
	})
	var rows []listRow
	for _, e := range entries {
		n := e.Name()
		if strings.HasPrefix(n, ".") || q.eff.Denied(n) || len(rows) >= maxListing {
			continue
		}
		info, err := e.Info()
		if err != nil || (!info.IsDir() && !info.Mode().IsRegular()) {
			continue
		}
		row := listRow{Name: cleanForDisplay(n), Href: (&url.URL{Path: n}).String(), Mod: info.ModTime().UTC().Format("2006-01-02 15:04")}
		if info.IsDir() {
			row.Dir, row.Href = true, row.Href+"/"
		} else {
			row.Size = humanSize(info.Size())
		}
		rows = append(rows, row)
	}
	var body bytes.Buffer
	data := map[string]any{
		"Name": i18n.T(q.lang, "web.index.name"), "Size": i18n.T(q.lang, "web.index.size"), "Mod": i18n.T(q.lang, "web.index.modified"),
		"UpLabel": i18n.T(q.lang, "web.index.up"), "Empty": i18n.T(q.lang, "web.index.empty"), "Rows": rows, "Up": "",
	}
	if p != "/" {
		data["Up"] = "../"
	}
	if err := listTpl.Execute(&body, data); err != nil {
		q.fail(http.StatusInternalServerError)
		return
	}
	q.eff.ApplyHeaders(q.w.Header(), http.StatusOK, "")
	renderPageWide(q.w, q.r, q.lang, pageData{
		Title: i18n.T(q.lang, "web.index.title", cleanForDisplay(p)), Tone: "ok", Body: template.HTML(body.String()), //nolint:gosec // тело собрано html/template с экранированием
	})
}

func humanSize(n int64) string {
	const unit = 1024
	if n < unit {
		return strconv.FormatInt(n, 10) + " B"
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGT"[exp])
}

// defaults переводит настройки сайта из панели в основу для .htaccess.
func (q *request) defaults() htaccess.Defaults {
	d := htaccess.Defaults{Indexes: q.set.Autoindex}
	if len(q.set.Index) > 0 {
		d.DirectoryIndex = q.set.Index
	} else if q.rt.Runtime == runtimecfg.PHP {
		d.DirectoryIndex = append([]string{"index.php"}, sitecfg.DefaultIndex...)
	}
	if len(q.set.ErrorPages) > 0 {
		d.ErrorDocs = map[int]htaccess.ErrorDoc{}
		for code, file := range q.set.ErrorPages {
			if n, err := strconv.Atoi(code); err == nil {
				d.ErrorDocs[n] = htaccess.ErrorDoc{Kind: "path", Value: "/" + file}
			}
		}
	}
	return d
}

// --- настройки сайта из панели ---

type cachedSettings struct {
	mod  time.Time
	size int64
	set  sitecfg.Settings
}

// settings возвращает настройки сайта; файл перечитывается, только когда он изменился.
func (h *Handler) settings(site string) sitecfg.Settings {
	dir := filepath.Join(h.opts.Root, site)
	fi, err := os.Lstat(filepath.Join(dir, sitecfg.FileName))
	if err != nil || !fi.Mode().IsRegular() {
		h.settingsCache.Delete(site)
		return sitecfg.Settings{}
	}
	if v, ok := h.settingsCache.Load(site); ok {
		c := v.(*cachedSettings)
		if c.mod.Equal(fi.ModTime()) && c.size == fi.Size() {
			return c.set
		}
	}
	set, err := sitecfg.Read(dir)
	if err != nil {
		return sitecfg.Settings{}
	}
	h.settingsCache.Store(site, &cachedSettings{mod: fi.ModTime(), size: fi.Size(), set: set})
	return set
}

// wwwTarget возвращает имя, на которое нужно перенаправить запрос по настройке «www», или пустую строку.
// Цель строится только из самого хоста и обязана обслуживаться этим же сайтом: открытым редиректом это стать не может.
func (h *Handler) wwwTarget(host, site, mode string) string {
	var cand string
	switch mode {
	case sitecfg.WWWAdd:
		if strings.HasPrefix(host, "www.") {
			return ""
		}
		cand = "www." + host
	case sitecfg.WWWRemove:
		if !strings.HasPrefix(host, "www.") {
			return ""
		}
		cand = strings.TrimPrefix(host, "www.")
	default:
		return ""
	}
	if s, _, ok := h.resolveSite(cand); ok && s == site {
		return cand
	}
	return ""
}
