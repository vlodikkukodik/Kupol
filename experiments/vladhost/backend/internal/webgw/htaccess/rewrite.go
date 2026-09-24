package htaccess

import (
	"path"
	"regexp"
	"strconv"
	"strings"
)

// Переменные, которые понимает %{...} в RewriteCond и в подстановках.
var knownVars = map[string]bool{
	"REQUEST_URI": true, "REQUEST_FILENAME": true, "SCRIPT_FILENAME": true, "SCRIPT_NAME": true, "PATH_INFO": true,
	"QUERY_STRING": true, "HTTP_HOST": true, "SERVER_NAME": true, "HTTPS": true, "REQUEST_SCHEME": true,
	"SERVER_PORT": true, "REQUEST_METHOD": true, "REMOTE_ADDR": true, "HTTP_USER_AGENT": true,
	"HTTP_REFERER": true, "HTTP_COOKIE": true, "HTTP_ACCEPT": true, "HTTP_ACCEPT_ENCODING": true,
	"HTTP_ACCEPT_LANGUAGE": true, "DOCUMENT_ROOT": true, "THE_REQUEST": true, "SERVER_PROTOCOL": true,
}

var varRe = regexp.MustCompile(`%\{([A-Za-z_]+)(?::([^}]*))?\}`)

func (c *Config) checkVars(line int, directive, s string) {
	for _, m := range varRe.FindAllStringSubmatch(s, -1) {
		name := strings.ToUpper(m[1])
		if name == "HTTP" || name == "ENV" {
			continue
		}
		if !knownVars[name] {
			c.diag(line, directive, DiagUnsupported, "%{"+m[1]+"}")
		}
	}
}

func splitFlags(s string) []string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "[")
	s = strings.TrimSuffix(s, "]")
	if s == "" {
		return nil
	}
	var out []string
	for _, f := range strings.Split(s, ",") {
		if f = strings.TrimSpace(f); f != "" {
			out = append(out, f)
		}
	}
	return out
}

func (c *Config) rewriteCond(line int, name string, args []token) {
	if len(args) < 2 {
		c.diag(line, name, DiagSyntax, "arguments")
		return
	}
	ct := CondTest{Test: args[0].text}
	c.checkVars(line, name, ct.Test)
	nc := false
	if len(args) >= 3 {
		for _, f := range splitFlags(args[2].text) {
			switch strings.ToLower(f) {
			case "nc", "nocase":
				nc = true
			case "or", "ornext":
				ct.Or = true
			default:
				c.diag(line, name, DiagUnsupported, f)
			}
		}
	}
	pat := args[1].text
	if strings.HasPrefix(pat, "!") {
		ct.Negate, pat = true, pat[1:]
	}
	switch {
	case pat == "-f" || pat == "-d" || pat == "-s" || pat == "-l" || pat == "-h":
		ct.Op = pat
		if pat == "-h" {
			ct.Op = "-l"
		}
	case pat == "-F" || pat == "-U" || pat == "-x" || pat == "-L":
		c.diag(line, name, DiagUnsupported, pat)
		ct.Bad = true
	case strings.HasPrefix(pat, "="):
		ct.Op, ct.Arg = "=", pat[1:]
	case strings.HasPrefix(pat, "<") && !strings.HasPrefix(pat, "<="):
		ct.Op, ct.Arg = "<", pat[1:]
	case strings.HasPrefix(pat, ">") && !strings.HasPrefix(pat, ">="):
		ct.Op, ct.Arg = ">", pat[1:]
	default:
		expr := pat
		if nc {
			expr = "(?i)" + expr
		}
		re, err := regexp.Compile(expr)
		if err != nil {
			c.diag(line, name, DiagRegex, err.Error())
			ct.Bad = true
		} else {
			ct.Pattern = re
		}
	}
	c.pending = append(c.pending, ct)
}

func (c *Config) rewriteRule(line int, name string, args []token) {
	conds := c.pending
	c.pending = nil
	if len(args) < 2 {
		c.diag(line, name, DiagSyntax, "arguments")
		return
	}
	r := &RewriteRule{Line: line, Conds: conds, Subst: args[1].text}
	c.checkVars(line, name, r.Subst)
	nc := false
	drop := false
	if len(args) >= 3 {
		for _, f := range splitFlags(args[2].text) {
			key, val, _ := strings.Cut(f, "=")
			switch strings.ToLower(key) {
			case "l", "last":
				r.Flags.Last = true
			case "end":
				r.Flags.End = true
			case "nc", "nocase":
				nc = true
			case "qsa", "qsappend":
				r.Flags.QSA = true
			case "qsd", "qsdiscard":
				r.Flags.QSD = true
			case "ne", "noescape":
				r.Flags.NE = true
			case "c", "chain":
				r.Flags.Chain = true
			case "r", "redirect":
				code := 302
				if val != "" {
					n, err := strconv.Atoi(val)
					if err != nil || n < 300 || n > 399 {
						if w, ok := statusWord(val); ok {
							n = w
						} else {
							c.diag(line, name, DiagSyntax, f)
							n = 302
						}
					}
					code = n
				}
				r.Flags.Redirect = code
			case "f", "forbidden":
				r.Flags.Forbidden = true
			case "g", "gone":
				r.Flags.Gone = true
			case "s", "skip":
				n, err := strconv.Atoi(val)
				if err != nil || n < 0 {
					c.diag(line, name, DiagSyntax, f)
				} else {
					r.Flags.Skip = n
				}
			case "pt", "passthrough", "ns", "nosubreq", "b", "bnp", "bctls", "qsl", "nocase2", "ds", "discardpath":
				// безвредные для статики
			default:
				// P (прокси), H (обработчик), CO, E, T, N и прочие выполнить нельзя: правило пропускается целиком.
				c.diag(line, name, DiagUnsupported, f)
				drop = true
			}
		}
	}
	if drop {
		return
	}
	pat := args[0].text
	if strings.HasPrefix(pat, "!") {
		r.Negate, pat = true, pat[1:]
	}
	expr := pat
	if nc {
		expr = "(?i)" + expr
	}
	re, err := regexp.Compile(expr)
	if err != nil {
		c.diag(line, name, DiagRegex, err.Error())
		return
	}
	r.Pattern = re
	c.Rewrites = append(c.Rewrites, r)
}

// Env — то, что правилам нужно знать о запросе.
type Env struct {
	Host, Method, RemoteAddr, Query string
	Header                          func(name string) string
	IsFile, IsDir, IsNonEmpty       func(urlPath string) bool
}

// RewriteSet — набор правил одного .htaccess. Dir — URL-путь его каталога ("/" или "/a/b/").
type RewriteSet struct {
	Dir   string
	Base  string
	Rules []*RewriteRule
}

type State struct{ Path, Query string }

type Outcome struct {
	State
	RedirectStatus int    // > 0: внешний редирект
	RedirectURL    string // абсолютный URL или путь с запросом
	Status         int    // 403 или 410 по флагам F и G
	End            bool
}

func hasScheme(s string) bool {
	i := strings.Index(s, "://")
	if i <= 0 || i > 10 {
		return false
	}
	for _, r := range s[:i] {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') {
			return false
		}
	}
	return true
}

// Apply выполняет один проход набора правил. changed сообщает, изменился ли URL: тогда вызывающий пересчитывает
// цепочку .htaccess и делает следующий проход (как внутренний редирект в Apache).
func (set RewriteSet) Apply(st State, env Env) (out Outcome, changed bool) {
	out.State = st
	skip := 0
	chainBroken := false
	for _, r := range set.Rules {
		if skip > 0 {
			skip--
			continue
		}
		if chainBroken {
			chainBroken = r.Flags.Chain // цепочка продолжается, пока у правил стоит C
			continue
		}
		rel, ok := relTo(set.Dir, out.Path)
		if !ok {
			return out, changed
		}
		m := r.Pattern.FindStringSubmatch(rel)
		matched := (m != nil) != r.Negate
		if r.Negate {
			m = nil
		}
		var cond []string
		if matched {
			matched, cond = evalConds(r.Conds, out.State, env, m)
		}
		if !matched {
			chainBroken = r.Flags.Chain
			continue
		}
		switch {
		case r.Flags.Forbidden:
			out.Status = 403
			return out, changed
		case r.Flags.Gone:
			out.Status = 410
			return out, changed
		}
		if r.Subst != "-" {
			exp := expand(r.Subst, m, cond, out.State, env)
			np, nq, external := applySubst(set, exp, out.Query, r.Flags)
			if external || r.Flags.Redirect > 0 {
				status := r.Flags.Redirect
				if status == 0 {
					status = 302
				}
				out.RedirectStatus = status
				out.RedirectURL = np
				if nq != "" {
					out.RedirectURL += "?" + nq
				}
				return out, true
			}
			if np != out.Path || nq != out.Query {
				changed = true
			}
			out.Path, out.Query = np, nq
		} else if r.Flags.Redirect > 0 {
			out.RedirectStatus = r.Flags.Redirect
			out.RedirectURL = out.Path
			return out, true
		}
		if r.Flags.End {
			out.End = true
			return out, changed
		}
		if r.Flags.Last {
			return out, changed
		}
		skip = r.Flags.Skip
	}
	return out, changed
}

func relTo(dir, p string) (string, bool) {
	if dir == "/" || dir == "" {
		return strings.TrimPrefix(p, "/"), true
	}
	if p+"/" == dir {
		return "", true
	}
	if strings.HasPrefix(p, dir) {
		return p[len(dir):], true
	}
	return "", false
}

// applySubst превращает подстановку в новый путь и запрос. external — подстановка с адресом сайта (http://…).
func applySubst(set RewriteSet, exp, oldQuery string, f RewriteFlags) (p, q string, external bool) {
	pathPart, query, hasQ := strings.Cut(exp, "?")
	if hasScheme(pathPart) {
		if !hasQ && !f.QSD {
			query = oldQuery
		}
		return pathPart, query, true
	}
	if !strings.HasPrefix(pathPart, "/") {
		base := set.Base
		if base == "" {
			base = set.Dir
		}
		if !strings.HasSuffix(base, "/") {
			base += "/"
		}
		pathPart = base + pathPart
	}
	trail := strings.HasSuffix(pathPart, "/")
	pathPart = path.Clean(pathPart) // "/../x" → "/x": выйти выше корня сайта нельзя
	if trail && pathPart != "/" {
		pathPart += "/"
	}
	switch {
	case hasQ && f.QSA && oldQuery != "":
		if query != "" {
			query += "&"
		}
		query += oldQuery
	case !hasQ && !f.QSD:
		query = oldQuery
	}
	return pathPart, query, false
}

func evalConds(conds []CondTest, st State, env Env, ruleGroups []string) (bool, []string) {
	if len(conds) == 0 {
		return true, nil
	}
	var last []string
	orAcc := false
	for _, cd := range conds {
		res, groups := evalCond(cd, st, env, ruleGroups, last)
		if res && groups != nil {
			last = groups
		}
		orAcc = orAcc || res
		if cd.Or {
			continue
		}
		if !orAcc {
			return false, nil
		}
		orAcc = false
	}
	if orAcc { // последнее условие помечено OR: группа считается выполненной, если хоть одно истинно
		return true, last
	}
	return true, last
}

func evalCond(cd CondTest, st State, env Env, ruleGroups, condGroups []string) (bool, []string) {
	if cd.Bad {
		return false, nil
	}
	s := expand(cd.Test, ruleGroups, condGroups, st, env)
	var res bool
	var groups []string
	switch cd.Op {
	case "-f":
		res = strings.HasPrefix(s, "/") && env.IsFile != nil && env.IsFile(s)
	case "-d":
		res = strings.HasPrefix(s, "/") && env.IsDir != nil && env.IsDir(s)
	case "-s":
		res = strings.HasPrefix(s, "/") && env.IsNonEmpty != nil && env.IsNonEmpty(s)
	case "-l":
		res = false // симлинков в сайте нет: они запрещены
	case "=":
		res = s == cd.Arg
	case "<":
		res = s < cd.Arg
	case ">":
		res = s > cd.Arg
	default:
		if cd.Pattern != nil {
			groups = cd.Pattern.FindStringSubmatch(s)
			res = groups != nil
		}
	}
	if cd.Negate {
		return !res, nil
	}
	return res, groups
}

// expand подставляет $N (из правила), %N (из условия) и %{VAR}.
func expand(s string, rule, cond []string, st State, env Env) string {
	if !strings.ContainsAny(s, "$%") {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		ch := s[i]
		switch {
		case ch == '$' && i+1 < len(s) && s[i+1] >= '0' && s[i+1] <= '9':
			n := int(s[i+1] - '0')
			if n < len(rule) {
				b.WriteString(rule[n])
			}
			i++
		case ch == '%' && i+1 < len(s) && s[i+1] >= '0' && s[i+1] <= '9':
			n := int(s[i+1] - '0')
			if n < len(cond) {
				b.WriteString(cond[n])
			}
			i++
		case ch == '%' && i+1 < len(s) && s[i+1] == '{':
			end := strings.IndexByte(s[i:], '}')
			if end < 0 {
				b.WriteByte(ch)
				continue
			}
			b.WriteString(variable(s[i+2:i+end], st, env))
			i += end
		default:
			b.WriteByte(ch)
		}
	}
	return b.String()
}

func variable(spec string, st State, env Env) string {
	name, arg, _ := strings.Cut(spec, ":")
	hdr := func(n string) string {
		if env.Header == nil {
			return ""
		}
		return env.Header(n)
	}
	switch strings.ToUpper(name) {
	case "REQUEST_URI", "REQUEST_FILENAME", "SCRIPT_FILENAME", "SCRIPT_NAME":
		return st.Path
	case "PATH_INFO", "DOCUMENT_ROOT":
		return "" // DOCUMENT_ROOT пуст: "%{DOCUMENT_ROOT}%{REQUEST_URI}" даёт путь внутри сайта
	case "QUERY_STRING":
		return st.Query
	case "HTTP_HOST", "SERVER_NAME":
		return env.Host
	case "HTTPS":
		return "on"
	case "REQUEST_SCHEME":
		return "https"
	case "SERVER_PORT":
		return "443"
	case "SERVER_PROTOCOL":
		return "HTTP/1.1"
	case "REQUEST_METHOD":
		return env.Method
	case "REMOTE_ADDR":
		return env.RemoteAddr
	case "HTTP_USER_AGENT":
		return hdr("User-Agent")
	case "HTTP_REFERER":
		return hdr("Referer")
	case "HTTP_COOKIE":
		return hdr("Cookie")
	case "HTTP_ACCEPT":
		return hdr("Accept")
	case "HTTP_ACCEPT_ENCODING":
		return hdr("Accept-Encoding")
	case "HTTP_ACCEPT_LANGUAGE":
		return hdr("Accept-Language")
	case "HTTP":
		return hdr(arg)
	case "THE_REQUEST":
		u := st.Path
		if st.Query != "" {
			u += "?" + st.Query
		}
		return env.Method + " " + u + " HTTP/1.1"
	}
	return ""
}
