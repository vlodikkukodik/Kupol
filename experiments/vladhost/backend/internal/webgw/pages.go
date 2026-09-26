package webgw

import (
	"html/template"
	"net/http"
	"strconv"
	"strings"

	"vladhost/internal/i18n"
)

// Страницы по умолчанию встроены в шлюз: они не лежат в папке сайта, пользователь не видит их в FTP и не может
// случайно удалить. Язык — по заголовку Accept-Language посетителя.

// Ключи каталога записаны литералами: сторож i18n сверяет их с обоими каталогами.
var errKeys = map[int][2]string{
	400: {"web.err.400.title", "web.err.400.text"},
	401: {"web.err.401.title", "web.err.401.text"},
	403: {"web.err.403.title", "web.err.403.text"},
	404: {"web.err.404.title", "web.err.404.text"},
	405: {"web.err.405.title", "web.err.405.text"},
	410: {"web.err.410.title", "web.err.410.text"},
	429: {"web.err.429.title", "web.err.429.text"},
	500: {"web.err.500.title", "web.err.500.text"},
	503: {"web.err.503.title", "web.err.503.text"},
}

type pageData struct {
	Lang, Code, Title, Text, Footer string
	Tone                            string // класс цвета: err, warn, ok
	Body                            template.HTML
}

var pageTpl = template.Must(template.New("page").Parse(`<!doctype html>
<html lang="{{.Lang}}">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<meta name="robots" content="noindex">
<title>{{if .Code}}{{.Code}} — {{end}}{{.Title}}</title>
<style>
*{box-sizing:border-box}
html{background:#070914}
body{margin:0;min-height:100vh;display:grid;place-items:center;padding:24px;color:#f1f2ff;
font:16px/1.55 ui-sans-serif,system-ui,-apple-system,"Segoe UI",Roboto,Helvetica,Arial,sans-serif;
background:radial-gradient(60vmax 60vmax at 12% -10%,rgba(99,102,241,.45),transparent 60%),
radial-gradient(50vmax 50vmax at 95% 10%,rgba(236,72,153,.32),transparent 60%),
radial-gradient(55vmax 55vmax at 45% 115%,rgba(6,182,212,.30),transparent 60%),#070914}
main{width:100%;max-width:640px;text-align:center;padding:44px 32px;border-radius:24px;
background:linear-gradient(180deg,rgba(255,255,255,.08),rgba(255,255,255,.04));
border:1px solid rgba(255,255,255,.12);box-shadow:0 30px 80px -30px rgba(0,0,0,.8)}
.code{font-size:clamp(64px,16vw,112px);font-weight:800;letter-spacing:-.04em;line-height:1;margin:0 0 8px;
background:linear-gradient(135deg,#6366f1,#8b5cf6 45%,#ec4899);-webkit-background-clip:text;background-clip:text;color:transparent}
.warn .code{background:linear-gradient(135deg,#f59e0b,#f43f5e);-webkit-background-clip:text;background-clip:text}
.ok .code{background:linear-gradient(135deg,#10b981,#22d3ee);-webkit-background-clip:text;background-clip:text}
h1{margin:0 0 10px;font-size:26px;letter-spacing:-.02em}
p{margin:0;color:rgba(241,242,255,.72)}
footer{margin-top:28px;font-size:13px;color:rgba(241,242,255,.42)}
footer b{background:linear-gradient(90deg,#818cf8,#f472b6);-webkit-background-clip:text;background-clip:text;color:transparent}
.icon{font-size:64px;line-height:1;margin-bottom:12px;filter:drop-shadow(0 8px 24px rgba(139,92,246,.6))}
table{width:100%;border-collapse:collapse;margin-top:22px;text-align:left;font-size:15px}
th{font-size:12px;text-transform:uppercase;letter-spacing:.06em;color:rgba(241,242,255,.46);padding:6px 10px}
td{padding:8px 10px;border-top:1px solid rgba(255,255,255,.08)}
td.n{text-align:right;color:rgba(241,242,255,.6);white-space:nowrap}
a{color:#7dd3fc;text-decoration:none}a:hover{color:#e0f2fe;text-decoration:underline}
main.wide{max-width:860px;text-align:left}main.wide h1{font-size:22px;word-break:break-all}
</style>
</head>
<body>
<main class="{{.Tone}}">
{{if .Code}}<div class="code">{{.Code}}</div>{{else}}<div class="icon">🚀</div>{{end}}
<h1>{{.Title}}</h1>
{{if .Text}}<p>{{.Text}}</p>{{end}}
{{.Body}}
<footer><b>Vladhost</b> · {{.Footer}}</footer>
</main>
</body>
</html>
`))

// renderPage отправляет страницу с нужным статусом. HEAD получает заголовки без тела.
func renderPage(w http.ResponseWriter, r *http.Request, lang i18n.Lang, status int, d pageData) {
	d.Lang = string(lang)
	d.Footer = i18n.T(lang, "web.footer")
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Content-Language", d.Lang)
	h.Set("Cache-Control", "no-store")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Add("Vary", "Accept-Language")
	w.WriteHeader(status)
	if r.Method != http.MethodHead {
		_ = pageTpl.Execute(w, d)
	}
}

// renderPageWide — то же, но широкая карточка (листинг каталога).
func renderPageWide(w http.ResponseWriter, r *http.Request, lang i18n.Lang, d pageData) {
	d.Tone += " wide"
	renderPage(w, r, lang, http.StatusOK, d)
}

// errorPage — страница ошибки по умолчанию.
func errorPage(w http.ResponseWriter, r *http.Request, lang i18n.Lang, status int) {
	keys, ok := errKeys[status]
	if !ok {
		keys = errKeys[500]
	}
	tone := "err"
	if status == 401 || status == 429 || status == 410 {
		tone = "warn"
	}
	renderPage(w, r, lang, status, pageData{
		Code: strconv.Itoa(status), Title: i18n.T(lang, keys[0]), Text: i18n.T(lang, keys[1]), Tone: tone,
	})
}

// emptySitePage — «здесь скоро появится сайт»: у сайта ещё нет ни одного файла.
func emptySitePage(w http.ResponseWriter, r *http.Request, lang i18n.Lang) {
	renderPage(w, r, lang, http.StatusOK, pageData{
		Title: i18n.T(lang, "web.empty.title"), Text: i18n.T(lang, "web.empty.text"), Tone: "ok",
	})
}

// noSitePage — по этому адресу сайта нет вообще.
func noSitePage(w http.ResponseWriter, r *http.Request, lang i18n.Lang) {
	renderPage(w, r, lang, http.StatusNotFound, pageData{
		Code: "404", Title: i18n.T(lang, "web.nosite.title"), Text: i18n.T(lang, "web.nosite.text"), Tone: "err",
	})
}

func cleanForDisplay(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, s)
}
