// Package htaccess разбирает файлы .htaccess и вычисляет их действие для запроса.
//
// nginx .htaccess не читает, поэтому нужное подмножество директив Apache реализовано здесь. Поддержано:
// DirectoryIndex, Options (Indexes), ErrorDocument, Redirect*, RewriteEngine/Base/Cond/Rule, Header, Expires*,
// AddType/ForceType/AddEncoding/AddDefaultCharset, Require/Order/Allow/Deny, AuthType Basic (AuthUserFile),
// блоки Files, FilesMatch, IfModule. Всё остальное не выполняется и попадает в список замечаний (Diag).
//
// Безопасность: регулярные выражения — RE2 (время линейное, ReDoS невозможен), размер файла и число директив
// ограничены, никакие директивы не выходят за каталог сайта (прокси, CGI и обработчики не поддержаны).
package htaccess

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	MaxSize       = 64 << 10
	maxDirectives = 500
)

// Коды замечаний. Фронтенд показывает по ним переведённый текст.
const (
	DiagUnsupported = "unsupported" // директива не поддерживается и игнорируется
	DiagSyntax      = "syntax"      // ошибка синтаксиса
	DiagRegex       = "regex"       // регулярное выражение не поддерживается (RE2 без lookahead и обратных ссылок)
	DiagTooBig      = "too_big"     // файл слишком большой, целиком проигнорирован
	DiagTooMany     = "too_many"    // слишком много директив, остальные проигнорированы
)

type Diag struct {
	Line      int    `json:"line"`
	Directive string `json:"directive"`
	Code      string `json:"code"`
	Detail    string `json:"detail,omitempty"`
}

// Scope ограничивает директиву файлами по имени (<Files>, <FilesMatch>). nil — весь каталог.
type Scope struct {
	Name string
	Re   *regexp.Regexp
}

func (s *Scope) Match(base string) bool {
	if s == nil {
		return true
	}
	if s.Re != nil {
		return s.Re.MatchString(base)
	}
	return s.Name == base
}

type ErrorDoc struct {
	Kind  string // "path", "text", "url"
	Value string
}

type Redirect struct {
	Status int
	Prefix string         // Redirect: путь-префикс
	Re     *regexp.Regexp // RedirectMatch
	Target string
}

type CondTest struct {
	Test    string // строка с %{VAR}
	Pattern *regexp.Regexp
	Op      string // "", "-f", "-d", "-s", "-l", "=", "<", ">"
	Arg     string // операнд для =, <, >
	Negate  bool
	Or      bool
	Bad     bool // условие не разобрано: всегда ложно
}

type RewriteRule struct {
	Line    int
	Conds   []CondTest
	Pattern *regexp.Regexp
	Negate  bool
	Subst   string
	Flags   RewriteFlags
}

type RewriteFlags struct {
	Last, End, QSA, QSD, NE, Chain bool
	Redirect                       int // 0 — нет, иначе код
	Forbidden, Gone                bool
	Skip                           int
}

type HeaderRule struct {
	Scope  *Scope
	Op     string // set, unset, add, append, merge, edit, edit*
	Always bool
	Name   string
	Value  string
	Re     *regexp.Regexp // для edit
}

type ExpiresRule struct {
	Type string
	Spec ExpiresSpec
}

type ExpiresSpec struct {
	FromModification bool
	Dur              time.Duration
}

type TypeRule struct {
	Ext  string // ".js"
	Mime string
}

type ForceType struct {
	Scope *Scope
	Mime  string
}

// Access: Kind = "denied" | "granted".
type AccessRule struct {
	Scope *Scope
	Kind  string
}

type AuthRule struct {
	Scope    *Scope
	Basic    bool
	Name     string
	UserFile string
	Any      bool     // Require valid-user
	Users    []string // Require user a b
	Set      bool     // директива Require при Basic-аутентификации
}

type Config struct {
	DirectoryIndex []string // nil — не задано
	Indexes        *bool
	ErrorDocs      map[int]ErrorDoc
	Redirects      []Redirect

	RewriteOn      *bool
	RewriteBase    string
	RewriteInherit bool
	Rewrites       []*RewriteRule

	Headers       []HeaderRule
	ExpiresActive *bool
	ExpiresByType []ExpiresRule
	ExpiresAll    *ExpiresSpec
	Types         []TypeRule
	Encodings     map[string]string // ".gz" → "gzip"
	ForceTypes    []ForceType
	Charset       *string // "" — Off
	Access        []AccessRule
	Auth          []AuthRule

	Diags []Diag

	pending []CondTest // RewriteCond, ждущие своего RewriteRule
}

func (c *Config) diag(line int, directive, code, detail string) {
	c.Diags = append(c.Diags, Diag{Line: line, Directive: directive, Code: code, Detail: detail})
}

type token struct {
	text   string
	quoted bool
}

// splitArgs делит строку на аргументы с учётом кавычек и экранирования внутри них.
func splitArgs(s string) ([]token, bool) {
	var out []token
	i := 0
	for i < len(s) {
		for i < len(s) && (s[i] == ' ' || s[i] == '\t') {
			i++
		}
		if i >= len(s) {
			break
		}
		if s[i] == '"' || s[i] == '\'' {
			q := s[i]
			i++
			var b strings.Builder
			closed := false
			for i < len(s) {
				if s[i] == '\\' && i+1 < len(s) && (s[i+1] == q || s[i+1] == '\\') {
					b.WriteByte(s[i+1])
					i += 2
					continue
				}
				if s[i] == q {
					closed = true
					i++
					break
				}
				b.WriteByte(s[i])
				i++
			}
			if !closed {
				return out, false
			}
			out = append(out, token{b.String(), true})
			continue
		}
		start := i
		for i < len(s) && s[i] != ' ' && s[i] != '\t' {
			i++
		}
		out = append(out, token{s[start:i], false})
	}
	return out, true
}

type logical struct {
	n    int
	text string
}

// logicalLines склеивает строки с переносом "\" и убирает пустые строки и комментарии.
func logicalLines(src string) []logical {
	src = strings.TrimPrefix(src, "\ufeff")
	src = strings.ReplaceAll(src, "\r\n", "\n")
	src = strings.ReplaceAll(src, "\r", "\n")
	var out []logical
	var cur strings.Builder
	start := 0
	for i, line := range strings.Split(src, "\n") {
		if cur.Len() == 0 {
			start = i + 1
		}
		t := strings.TrimRight(line, " \t")
		if strings.HasSuffix(t, "\\") {
			cur.WriteString(strings.TrimSuffix(t, "\\"))
			cur.WriteByte(' ')
			continue
		}
		cur.WriteString(t)
		text := strings.TrimSpace(cur.String())
		cur.Reset()
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		out = append(out, logical{start, text})
	}
	if s := strings.TrimSpace(cur.String()); s != "" && !strings.HasPrefix(s, "#") {
		out = append(out, logical{start, s})
	}
	return out
}

// Модули, которые считаются «загруженными» для <IfModule>. Остальные (mod_php, mod_security…) — нет,
// поэтому их блоки пропускаются без замечаний, как на сервере без этих модулей.
var knownModules = map[string]bool{
	"mod_rewrite.c": true, "mod_headers.c": true, "mod_expires.c": true, "mod_mime.c": true, "mod_alias.c": true,
	"mod_dir.c": true, "mod_autoindex.c": true, "mod_authz_core.c": true, "mod_auth_basic.c": true,
	"mod_authn_file.c": true, "mod_authz_host.c": true, "mod_deflate.c": true, "mod_filter.c": true,
	"core.c": true, "mod_authz_user.c": true, "mod_setenvif.c": false,
}

func moduleKnown(name string) bool {
	n := strings.ToLower(name)
	n = strings.TrimSuffix(strings.TrimSuffix(n, ".c"), ".so")
	if strings.HasSuffix(n, "_module") { // <IfModule rewrite_module>
		n = "mod_" + strings.TrimSuffix(n, "_module")
	}
	return knownModules[n+".c"]
}

type frame struct {
	name  string
	scope *Scope
	skip  bool
}

// noop — директивы, которые безвредно игнорируются без замечаний (сжатие делает nginx, остальное не влияет на статику).
var noop = map[string]bool{
	"fileetag": true, "serversignature": true, "defaulttype": true, "addlanguage": true, "adddescription": true,
	"indexoptions": true, "indexignore": true, "indexorderdefault": true, "headername": true, "readmename": true,
	"contentdigest": true, "addoutputfilterbytype": true, "addoutputfilter": true, "setoutputfilter": true,
	"authbasicprovider": true, "authbasicauthoritative": true, "authauthoritative": true, "addcharset": true,
	"multiviewsmatch": true, "checkspelling": true, "checkcaseonly": true, "allowoverride": true,
}

// Parse разбирает содержимое .htaccess. Ошибки не прерывают разбор: проблемные строки попадают в Diags.
func Parse(src []byte) *Config {
	c := &Config{ErrorDocs: map[int]ErrorDoc{}, Encodings: map[string]string{}}
	if len(src) > MaxSize {
		c.diag(0, "", DiagTooBig, strconv.Itoa(len(src)))
		return c
	}
	var stack []frame
	count := 0
	curScope := func() *Scope {
		for i := len(stack) - 1; i >= 0; i-- {
			if stack[i].scope != nil {
				return stack[i].scope
			}
		}
		return nil
	}
	skipping := func() bool {
		for _, f := range stack {
			if f.skip {
				return true
			}
		}
		return false
	}

	for _, ln := range logicalLines(string(src)) {
		text := ln.text
		if strings.HasPrefix(text, "<") && strings.HasSuffix(text, ">") {
			inner := text[1 : len(text)-1]
			if strings.HasPrefix(inner, "/") {
				name := strings.ToLower(strings.TrimSpace(inner[1:]))
				if len(stack) == 0 || stack[len(stack)-1].name != name {
					c.diag(ln.n, "</"+name+">", DiagSyntax, "unbalanced")
					continue
				}
				stack = stack[:len(stack)-1]
				continue
			}
			c.openBlock(&stack, ln.n, inner, skipping())
			continue
		}
		if skipping() {
			continue
		}
		if count++; count > maxDirectives {
			c.diag(ln.n, "", DiagTooMany, strconv.Itoa(maxDirectives))
			break
		}
		name, rest, _ := strings.Cut(text, " ")
		if i := strings.IndexByte(name, '\t'); i >= 0 {
			rest = name[i+1:] + " " + rest
			name = name[:i]
		}
		args, ok := splitArgs(rest)
		lname := strings.ToLower(name)
		if !ok {
			c.diag(ln.n, name, DiagSyntax, "quotes")
			continue
		}
		c.directive(ln.n, name, lname, args, curScope())
	}
	if len(stack) > 0 {
		c.diag(0, "<"+stack[len(stack)-1].name+">", DiagSyntax, "unclosed")
	}
	return c
}

func (c *Config) openBlock(stack *[]frame, line int, inner string, parentSkip bool) {
	args, ok := splitArgs(inner)
	if !ok || len(args) == 0 {
		c.diag(line, "<"+inner+">", DiagSyntax, "block")
		*stack = append(*stack, frame{name: "?", skip: true})
		return
	}
	name := strings.ToLower(args[0].text)
	args = args[1:]
	f := frame{name: name}
	switch name {
	case "ifmodule":
		if len(args) == 1 {
			m := args[0].text
			neg := strings.HasPrefix(m, "!")
			known := moduleKnown(strings.TrimPrefix(m, "!"))
			f.skip = neg == known // положительное условие при неизвестном модуле или отрицательное при известном
		} else {
			f.skip = true
		}
	case "files":
		switch {
		case len(args) == 1:
			f.scope = &Scope{Name: args[0].text}
		case len(args) == 2 && args[0].text == "~":
			f.scope = c.scopeRegex(line, "Files", args[1].text)
		default:
			f.skip = true
			if !parentSkip {
				c.diag(line, "<Files>", DiagSyntax, "arguments")
			}
		}
	case "filesmatch":
		if len(args) == 1 {
			f.scope = c.scopeRegex(line, "FilesMatch", args[0].text)
		} else {
			f.skip = true
			if !parentSkip {
				c.diag(line, "<FilesMatch>", DiagSyntax, "arguments")
			}
		}
		if f.scope == nil {
			f.skip = true
		}
	default:
		f.skip = true
		if !parentSkip {
			c.diag(line, "<"+name+">", DiagUnsupported, "")
		}
	}
	if f.scope == nil && (name == "files") && !f.skip {
		f.skip = true
	}
	*stack = append(*stack, f)
}

func (c *Config) scopeRegex(line int, dir, pattern string) *Scope {
	re, err := regexp.Compile(pattern)
	if err != nil {
		c.diag(line, "<"+dir+">", DiagRegex, err.Error())
		return nil
	}
	return &Scope{Re: re}
}

func (c *Config) directive(line int, name, lname string, args []token, scope *Scope) {
	need := func(n int) bool {
		if len(args) < n {
			c.diag(line, name, DiagSyntax, "arguments")
			return false
		}
		return true
	}
	switch lname {
	case "directoryindex":
		if !need(1) {
			return
		}
		c.DirectoryIndex = c.DirectoryIndex[:0:0]
		for _, a := range args {
			if strings.EqualFold(a.text, "disabled") {
				c.DirectoryIndex = []string{}
				return
			}
			c.DirectoryIndex = append(c.DirectoryIndex, a.text)
		}
	case "options":
		c.options(line, name, args)
	case "errordocument":
		c.errorDocument(line, name, args)
	case "redirect", "redirectpermanent", "redirecttemp", "redirectmatch":
		c.redirect(line, name, lname, args)
	case "rewriteengine":
		if need(1) {
			on := strings.EqualFold(args[0].text, "on")
			c.RewriteOn = &on
		}
	case "rewritebase":
		if need(1) {
			c.RewriteBase = args[0].text
		}
	case "rewriteoptions":
		for _, a := range args {
			if strings.EqualFold(a.text, "inherit") {
				c.RewriteInherit = true
			} else {
				c.diag(line, name, DiagUnsupported, a.text)
			}
		}
	case "rewritecond":
		c.rewriteCond(line, name, args)
	case "rewriterule":
		c.rewriteRule(line, name, args)
	case "header":
		c.header(line, name, args, scope)
	case "expiresactive":
		if need(1) {
			on := strings.EqualFold(args[0].text, "on")
			c.ExpiresActive = &on
		}
	case "expiresbytype":
		if need(2) {
			if spec, ok := ParseExpires(args[1].text); ok {
				c.ExpiresByType = append(c.ExpiresByType, ExpiresRule{Type: strings.ToLower(args[0].text), Spec: spec})
			} else {
				c.diag(line, name, DiagSyntax, args[1].text)
			}
		}
	case "expiresdefault":
		if need(1) {
			if spec, ok := ParseExpires(args[0].text); ok {
				c.ExpiresAll = &spec
			} else {
				c.diag(line, name, DiagSyntax, args[0].text)
			}
		}
	case "addtype":
		c.addType(line, name, args)
	case "forcetype":
		if need(1) {
			if strings.EqualFold(args[0].text, "none") {
				return
			}
			c.ForceTypes = append(c.ForceTypes, ForceType{Scope: scope, Mime: args[0].text})
		}
	case "addencoding":
		if need(2) {
			for _, e := range args[1:] {
				c.Encodings[normExt(e.text)] = strings.ToLower(args[0].text)
			}
		}
	case "adddefaultcharset":
		if need(1) {
			cs := args[0].text
			if strings.EqualFold(cs, "off") {
				cs = ""
			}
			c.Charset = &cs
		}
	case "require":
		c.require(line, name, args, scope)
	case "order":
		// Порядок значения не имеет: итог задают Allow/Deny ниже.
	case "allow", "deny":
		if len(args) >= 2 && strings.EqualFold(args[0].text, "from") && strings.EqualFold(args[1].text, "all") {
			kind := "granted"
			if lname == "deny" {
				kind = "denied"
			}
			c.Access = append(c.Access, AccessRule{Scope: scope, Kind: kind})
		} else {
			c.diag(line, name, DiagUnsupported, "from ip")
		}
	case "authtype":
		if need(1) {
			c.authRule(scope).Basic = strings.EqualFold(args[0].text, "basic")
			if !strings.EqualFold(args[0].text, "basic") {
				c.diag(line, name, DiagUnsupported, args[0].text)
			}
		}
	case "authname":
		if need(1) {
			c.authRule(scope).Name = args[0].text
		}
	case "authuserfile":
		if need(1) {
			c.authRule(scope).UserFile = args[0].text
		}
	default:
		if noop[lname] {
			return
		}
		c.diag(line, name, DiagUnsupported, "")
	}
}

// authRule возвращает правило авторизации для области (создаёт при необходимости).
func (c *Config) authRule(scope *Scope) *AuthRule {
	for i := range c.Auth {
		if c.Auth[i].Scope == scope {
			return &c.Auth[i]
		}
	}
	c.Auth = append(c.Auth, AuthRule{Scope: scope})
	return &c.Auth[len(c.Auth)-1]
}

func (c *Config) require(line int, name string, args []token, scope *Scope) {
	if len(args) == 0 {
		c.diag(line, name, DiagSyntax, "arguments")
		return
	}
	a0 := strings.ToLower(args[0].text)
	switch {
	case a0 == "all" && len(args) == 2 && strings.EqualFold(args[1].text, "denied"):
		c.Access = append(c.Access, AccessRule{Scope: scope, Kind: "denied"})
	case a0 == "all" && len(args) == 2 && strings.EqualFold(args[1].text, "granted"):
		c.Access = append(c.Access, AccessRule{Scope: scope, Kind: "granted"})
	case a0 == "valid-user":
		r := c.authRule(scope)
		r.Any, r.Set = true, true
	case a0 == "user" && len(args) >= 2:
		r := c.authRule(scope)
		r.Set = true
		for _, u := range args[1:] {
			r.Users = append(r.Users, u.text)
		}
	default:
		c.diag(line, name, DiagUnsupported, strings.Join(tokenTexts(args), " "))
	}
}

func tokenTexts(a []token) []string {
	out := make([]string, len(a))
	for i, t := range a {
		out[i] = t.text
	}
	return out
}

func (c *Config) options(line int, name string, args []token) {
	for _, a := range args {
		opt := strings.ToLower(a.text)
		on := true
		if strings.HasPrefix(opt, "+") {
			opt = opt[1:]
		} else if strings.HasPrefix(opt, "-") {
			opt, on = opt[1:], false
		}
		switch opt {
		case "indexes":
			c.Indexes = &on
		case "none":
			off := false
			c.Indexes = &off
		case "multiviews", "followsymlinks", "symlinksifownermatch", "all":
			if opt == "all" {
				c.Indexes = &on
			}
		default:
			c.diag(line, name, DiagUnsupported, a.text)
		}
	}
}

func (c *Config) errorDocument(line int, name string, args []token) {
	if len(args) < 2 {
		c.diag(line, name, DiagSyntax, "arguments")
		return
	}
	code, err := strconv.Atoi(args[0].text)
	if err != nil || code < 400 || code > 599 {
		c.diag(line, name, DiagSyntax, args[0].text)
		return
	}
	v := args[1]
	switch {
	case v.quoted:
		c.ErrorDocs[code] = ErrorDoc{Kind: "text", Value: v.text}
	case strings.HasPrefix(v.text, "http://") || strings.HasPrefix(v.text, "https://"):
		c.ErrorDocs[code] = ErrorDoc{Kind: "url", Value: v.text}
	case strings.HasPrefix(v.text, "/"):
		c.ErrorDocs[code] = ErrorDoc{Kind: "path", Value: v.text}
	default:
		c.diag(line, name, DiagSyntax, v.text)
	}
}

func statusWord(s string) (int, bool) {
	switch strings.ToLower(s) {
	case "permanent":
		return 301, true
	case "temp":
		return 302, true
	case "seeother":
		return 303, true
	case "gone":
		return 410, true
	}
	n, err := strconv.Atoi(s)
	return n, err == nil && n >= 300 && n <= 399
}

func (c *Config) redirect(line int, name, lname string, args []token) {
	status := 302
	switch lname {
	case "redirectpermanent":
		status = 301
	case "redirecttemp":
		status = 302
	default:
		if len(args) > 0 {
			if n, ok := statusWord(args[0].text); ok {
				status, args = n, args[1:]
			}
		}
	}
	gone := status == 410
	if (gone && len(args) < 1) || (!gone && len(args) < 2) {
		c.diag(line, name, DiagSyntax, "arguments")
		return
	}
	r := Redirect{Status: status}
	if gone && len(args) == 1 {
		args = append(args, token{})
	}
	r.Target = args[1].text
	if lname == "redirectmatch" {
		re, err := regexp.Compile(args[0].text)
		if err != nil {
			c.diag(line, name, DiagRegex, err.Error())
			return
		}
		r.Re = re
	} else {
		if !strings.HasPrefix(args[0].text, "/") {
			c.diag(line, name, DiagSyntax, args[0].text)
			return
		}
		r.Prefix = args[0].text
	}
	c.Redirects = append(c.Redirects, r)
}

func (c *Config) header(line int, name string, args []token, scope *Scope) {
	h := HeaderRule{Scope: scope}
	if len(args) > 0 && strings.EqualFold(args[0].text, "always") {
		h.Always, args = true, args[1:]
	}
	if len(args) < 2 {
		c.diag(line, name, DiagSyntax, "arguments")
		return
	}
	h.Op = strings.ToLower(args[0].text)
	h.Name = args[1].text
	rest := args[2:]
	for len(rest) > 0 && strings.HasPrefix(strings.ToLower(rest[len(rest)-1].text), "env=") {
		c.diag(line, name, DiagUnsupported, rest[len(rest)-1].text)
		return
	}
	switch h.Op {
	case "set", "add", "append", "merge":
		if len(rest) < 1 {
			c.diag(line, name, DiagSyntax, "arguments")
			return
		}
		h.Value = rest[0].text
	case "unset":
	case "edit", "edit*":
		if len(rest) < 2 {
			c.diag(line, name, DiagSyntax, "arguments")
			return
		}
		re, err := regexp.Compile(rest[0].text)
		if err != nil {
			c.diag(line, name, DiagRegex, err.Error())
			return
		}
		h.Re, h.Value = re, rest[1].text
	default:
		c.diag(line, name, DiagUnsupported, h.Op)
		return
	}
	if forbiddenHeader(h.Name) {
		c.diag(line, name, DiagUnsupported, h.Name)
		return
	}
	c.Headers = append(c.Headers, h)
}

// Заголовки, которые пользователь менять не может: они ломают протокол или (Set-Cookie) позволяют подбросить
// cookie для всего родительского домена, где живёт панель.
func forbiddenHeader(name string) bool {
	switch strings.ToLower(name) {
	case "content-length", "transfer-encoding", "connection", "set-cookie", "set-cookie2", "content-encoding",
		"location", "host", "upgrade", "te", "trailer", "keep-alive", "proxy-authenticate", "www-authenticate":
		return true
	}
	return strings.ContainsAny(name, " \t:\r\n\x00")
}

func normExt(e string) string {
	e = strings.ToLower(e)
	if !strings.HasPrefix(e, ".") {
		e = "." + e
	}
	return e
}

func (c *Config) addType(line int, name string, args []token) {
	if len(args) < 2 {
		c.diag(line, name, DiagSyntax, "arguments")
		return
	}
	mime := strings.ToLower(args[0].text)
	if strings.Contains(mime, "httpd") || strings.Contains(mime, "php") || strings.Contains(mime, "cgi") {
		c.diag(line, name, DiagUnsupported, mime) // обработчики не поддержаны
		return
	}
	for _, e := range args[1:] {
		c.Types = append(c.Types, TypeRule{Ext: normExt(e.text), Mime: mime})
	}
}

// ParseExpires разбирает "access plus 1 month 2 days", "modification plus 3 hours" и устаревшее "A3600"/"M3600".
func ParseExpires(s string) (ExpiresSpec, bool) {
	s = strings.TrimSpace(strings.ToLower(s))
	var spec ExpiresSpec
	if len(s) > 1 && (s[0] == 'a' || s[0] == 'm') {
		if n, err := strconv.Atoi(s[1:]); err == nil && n >= 0 {
			spec.FromModification = s[0] == 'm'
			spec.Dur = time.Duration(n) * time.Second
			return spec, true
		}
	}
	f := strings.Fields(s)
	if len(f) < 3 || f[1] != "plus" {
		return spec, false
	}
	switch f[0] {
	case "access", "now":
	case "modification":
		spec.FromModification = true
	default:
		return spec, false
	}
	units := map[string]time.Duration{
		"second": time.Second, "minute": time.Minute, "hour": time.Hour, "day": 24 * time.Hour,
		"week": 7 * 24 * time.Hour, "month": 30 * 24 * time.Hour, "year": 365 * 24 * time.Hour,
	}
	rest := f[2:]
	if len(rest)%2 != 0 {
		return spec, false
	}
	for i := 0; i < len(rest); i += 2 {
		n, err := strconv.Atoi(rest[i])
		u, ok := units[strings.TrimSuffix(rest[i+1], "s")]
		if err != nil || n < 0 || !ok {
			return spec, false
		}
		spec.Dur += time.Duration(n) * u
	}
	// Больше десяти лет не имеет смысла и может переполнить Duration.
	if spec.Dur > 10*365*24*time.Hour {
		spec.Dur = 10 * 365 * 24 * time.Hour
	}
	return spec, true
}
