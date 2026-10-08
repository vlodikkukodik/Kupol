package htaccess

import (
	"net/http"
	"path"
	"strings"
	"time"
)

// Dir — .htaccess одного каталога цепочки. URL: "/" или "/a/b/".
type Dir struct {
	URL string
	Cfg *Config
}

// Effective — итоговые настройки для запроса: директивы всех .htaccess от корня сайта до каталога запроса.
// Дочерний каталог переопределяет родительский (заголовки накапливаются).
type Effective struct {
	chain []Dir

	DirectoryIndex []string
	Indexes        bool
	ErrorDocs      map[int]ErrorDoc
	Charset        string
	RewriteSets    []RewriteSet
	Redirects      []Redirect

	expiresOn     bool
	expiresByType map[string]ExpiresSpec
	expiresAll    *ExpiresSpec
	types         map[string]string
	encodings     map[string]string
}

// Defaults — настройки сайта из панели: они действуют, пока директива не задана в .htaccess (тот сильнее).
type Defaults struct {
	DirectoryIndex []string         // nil — index.html, index.htm
	Indexes        bool             // показывать список файлов каталога
	ErrorDocs      map[int]ErrorDoc // страницы ошибок по коду
}

// Merge собирает итоговые настройки. chain идёт от корня сайта вглубь.
func Merge(chain []Dir) *Effective { return MergeWith(chain, Defaults{}) }

// MergeWith то же, но с настройками сайта из панели в качестве основы.
func MergeWith(chain []Dir, def Defaults) *Effective {
	e := &Effective{
		chain: chain, DirectoryIndex: []string{"index.html", "index.htm"}, Indexes: def.Indexes, ErrorDocs: map[int]ErrorDoc{},
		Charset: "utf-8", expiresByType: map[string]ExpiresSpec{}, types: map[string]string{}, encodings: map[string]string{},
	}
	if def.DirectoryIndex != nil {
		e.DirectoryIndex = def.DirectoryIndex
	}
	for code, doc := range def.ErrorDocs {
		e.ErrorDocs[code] = doc
	}
	deepest := -1
	for i, d := range chain {
		c := d.Cfg
		if c.DirectoryIndex != nil {
			e.DirectoryIndex = c.DirectoryIndex
		}
		if c.Indexes != nil {
			e.Indexes = *c.Indexes
		}
		for code, doc := range c.ErrorDocs {
			e.ErrorDocs[code] = doc
		}
		if c.Charset != nil {
			e.Charset = *c.Charset
		}
		e.Redirects = append(e.Redirects, c.Redirects...)
		if c.ExpiresActive != nil {
			e.expiresOn = *c.ExpiresActive
		}
		for _, r := range c.ExpiresByType {
			e.expiresByType[r.Type] = r.Spec
		}
		if c.ExpiresAll != nil {
			e.expiresAll = c.ExpiresAll
		}
		for _, t := range c.Types {
			e.types[t.Ext] = t.Mime
		}
		for ext, enc := range c.Encodings {
			e.encodings[ext] = enc
		}
		if c.RewriteOn != nil && *c.RewriteOn {
			deepest = i
		}
	}
	// mod_rewrite: правила самого глубокого .htaccess с RewriteEngine On заменяют родительские,
	// если только он не просит RewriteOptions Inherit.
	if deepest >= 0 {
		add := func(d Dir) {
			e.RewriteSets = append(e.RewriteSets, RewriteSet{Dir: d.URL, Base: d.Cfg.RewriteBase, Rules: d.Cfg.Rewrites})
		}
		if chain[deepest].Cfg.RewriteInherit {
			for i := 0; i < deepest; i++ {
				if on := chain[i].Cfg.RewriteOn; on != nil && *on {
					add(chain[i])
				}
			}
		}
		add(chain[deepest])
	}
	return e
}

// Diags возвращает замечания всех файлов цепочки.
func (e *Effective) Diags() []Diag {
	var out []Diag
	for _, d := range e.chain {
		out = append(out, d.Cfg.Diags...)
	}
	return out
}

// MatchRedirect ищет подходящий Redirect/RedirectMatch. URL цели может быть относительным путём.
func (e *Effective) MatchRedirect(urlPath string) (status int, target string, ok bool) {
	for _, r := range e.Redirects {
		if r.Re != nil {
			if m := r.Re.FindStringSubmatchIndex(urlPath); m != nil {
				return r.Status, string(r.Re.ExpandString(nil, r.Target, urlPath, m)), true
			}
			continue
		}
		if urlPath == r.Prefix || strings.HasPrefix(urlPath, strings.TrimSuffix(r.Prefix, "/")+"/") {
			rest := strings.TrimPrefix(urlPath, r.Prefix)
			return r.Status, r.Target + rest, true
		}
	}
	return 0, "", false
}

func sanitizeHeader(v string) string {
	return strings.Map(func(r rune) rune {
		if r == '\r' || r == '\n' || r == 0 {
			return -1
		}
		return r
	}, v)
}

// ApplyHeaders выполняет директивы Header. file — имя файла ответа ("" для ответов без файла).
func (e *Effective) ApplyHeaders(h http.Header, status int, file string) {
	for _, d := range e.chain {
		for _, r := range d.Cfg.Headers {
			if !r.Always && status >= 400 {
				continue
			}
			if r.Scope != nil && (file == "" || !r.Scope.Match(file)) {
				continue
			}
			name, val := http.CanonicalHeaderKey(r.Name), sanitizeHeader(r.Value)
			switch r.Op {
			case "set":
				h.Set(name, val)
			case "unset":
				h.Del(name)
			case "add":
				h.Add(name, val)
			case "append":
				if cur := h.Get(name); cur != "" {
					h.Set(name, cur+", "+val)
				} else {
					h.Set(name, val)
				}
			case "merge":
				cur := h.Get(name)
				switch {
				case cur == "":
					h.Set(name, val)
				case !containsToken(cur, val):
					h.Set(name, cur+", "+val)
				}
			case "edit", "edit*":
				vals := h.Values(name)
				h.Del(name)
				for _, v := range vals {
					if r.Op == "edit*" {
						v = r.Re.ReplaceAllString(v, val)
					} else if loc := r.Re.FindStringSubmatchIndex(v); loc != nil {
						v = v[:loc[0]] + string(r.Re.ExpandString(nil, val, v, loc)) + v[loc[1]:]
					}
					h.Add(name, sanitizeHeader(v))
				}
			}
		}
	}
}

func containsToken(list, tok string) bool {
	for _, p := range strings.Split(list, ",") {
		if strings.EqualFold(strings.TrimSpace(p), tok) {
			return true
		}
	}
	return false
}

// MaxAge возвращает срок жизни кэша по ExpiresByType/ExpiresDefault.
func (e *Effective) MaxAge(mime string, modTime, now time.Time) (time.Duration, bool) {
	if !e.expiresOn {
		return 0, false
	}
	spec, ok := e.expiresByType[strings.ToLower(mime)]
	if !ok {
		if e.expiresAll == nil {
			return 0, false
		}
		spec = *e.expiresAll
	}
	if spec.FromModification {
		left := modTime.Add(spec.Dur).Sub(now)
		return max(left, 0), true
	}
	return spec.Dur, true
}

// Denied сообщает, закрыт ли файл директивами Require all denied / Deny from all.
func (e *Effective) Denied(file string) bool {
	denied := false
	for _, d := range e.chain {
		for _, r := range d.Cfg.Access {
			if r.Scope.Match(file) {
				denied = r.Kind == "denied"
			}
		}
	}
	return denied
}

// AuthNeed — требование Basic-аутентификации для файла.
type AuthNeed struct {
	Realm    string
	UserFile string // путь внутри сайта, абсолютный URL-подобный ("/dir/.htpasswd")
	Any      bool
	Users    []string
}

// Auth возвращает требование аутентификации или nil.
func (e *Effective) Auth(file string) *AuthNeed {
	var need AuthNeed
	basic, set, have := false, false, false
	for _, d := range e.chain {
		for _, r := range d.Cfg.Auth {
			if !r.Scope.Match(file) {
				continue
			}
			have = true
			if r.Basic {
				basic = true
			}
			if r.Name != "" {
				need.Realm = r.Name
			}
			if r.UserFile != "" {
				need.UserFile = resolveUserFile(d.URL, r.UserFile)
			}
			if r.Set {
				set, need.Any, need.Users = true, r.Any, r.Users
			}
		}
	}
	if !have || !basic || !set || need.UserFile == "" {
		return nil
	}
	if need.Realm == "" {
		need.Realm = "Restricted"
	}
	return &need
}

// resolveUserFile переводит AuthUserFile в путь внутри сайта. Абсолютные серверные пути (/home/u/.htpasswd)
// здесь недоступны, поэтому берётся имя файла в каталоге .htaccess; путь с "/" в начале, лежащий в сайте
// ("/private/.htpasswd"), тоже понимается как путь от корня сайта.
func resolveUserFile(dirURL, p string) string {
	if strings.HasPrefix(p, "/") {
		return path.Clean("/" + strings.TrimPrefix(dirURL, "/") + path.Base(p))
	}
	return path.Clean(dirURL + p)
}

// Mime определяет тип содержимого по имени файла и кодирование (для app.js.gz — application/javascript + gzip).
func (e *Effective) Mime(name string) (mime, encoding string) {
	for i := len(e.chain) - 1; i >= 0; i-- {
		for _, f := range e.chain[i].Cfg.ForceTypes {
			if f.Scope.Match(name) {
				return withCharset(f.Mime, e.Charset), ""
			}
		}
	}
	base := path.Base(name)
	ext := strings.ToLower(path.Ext(base))
	if enc, ok := e.encodings[ext]; ok {
		encoding = enc
		base = strings.TrimSuffix(base, path.Ext(base))
		ext = strings.ToLower(path.Ext(base))
	}
	if m, ok := e.types[ext]; ok {
		return withCharset(m, e.Charset), encoding
	}
	if m, ok := BuiltinMime[ext]; ok {
		return withCharset(m, e.Charset), encoding
	}
	return "", encoding
}

func withCharset(mime, charset string) string {
	if charset == "" || strings.Contains(mime, "charset") {
		return mime
	}
	if strings.HasPrefix(mime, "text/") || mime == "application/javascript" || mime == "application/json" ||
		mime == "application/xml" || mime == "image/svg+xml" || mime == "application/manifest+json" {
		return mime + "; charset=" + charset
	}
	return mime
}

// BuiltinMime — типы по расширению. Свои, а не из системных файлов: результат не зависит от сервера.
var BuiltinMime = map[string]string{
	".html": "text/html", ".htm": "text/html", ".css": "text/css", ".js": "application/javascript",
	".mjs": "application/javascript", ".json": "application/json", ".map": "application/json",
	".xml": "application/xml", ".txt": "text/plain", ".md": "text/markdown", ".csv": "text/csv",
	".svg": "image/svg+xml", ".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".gif": "image/gif",
	".webp": "image/webp", ".avif": "image/avif", ".ico": "image/x-icon", ".bmp": "image/bmp",
	".woff": "font/woff", ".woff2": "font/woff2", ".ttf": "font/ttf", ".otf": "font/otf", ".eot": "application/vnd.ms-fontobject",
	".pdf": "application/pdf", ".zip": "application/zip", ".gz": "application/gzip", ".tar": "application/x-tar",
	".wasm": "application/wasm", ".webmanifest": "application/manifest+json", ".rss": "application/rss+xml",
	".atom": "application/atom+xml", ".mp4": "video/mp4", ".webm": "video/webm", ".ogv": "video/ogg",
	".mp3": "audio/mpeg", ".ogg": "audio/ogg", ".wav": "audio/wav", ".m4a": "audio/mp4", ".flac": "audio/flac",
	".doc": "application/msword", ".xls": "application/vnd.ms-excel", ".ppt": "application/vnd.ms-powerpoint",
	".docx": "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
	".xlsx": "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
	".ics":  "text/calendar", ".vtt": "text/vtt", ".yaml": "text/yaml", ".yml": "text/yaml",
}
