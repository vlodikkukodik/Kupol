// Package i18n — язык ответов сервера: русский (основной) и итальянский.
//
// Устройство «как в gettext»: в коде тексты остаются русскими — они же служат ключом (msgid), а итальянские переводы
// лежат в каталоге (it_*.go). Lang.T(msg, args…) переводит формат и подставляет значения: Lang.T("год — от %d до %d", a, b).
// Русский текст выдаётся как есть; непереведённый итальянский тоже (лучше русский текст, чем пустота), но
// coverage_test.go не даёт такому попасть в сборку: он находит в исходниках все русские строки, способные дойти до
// читателя, и требует для каждой перевод.
//
// Язык запроса — заголовок Accept-Language (его шлёт интерфейс сайта); он попадает в context.Context запроса (WithLang),
// и любой слой, у которого есть ctx, узнаёт его через From. Сообщения журнала, служебных команд и ошибки конфигурации —
// для оператора, не для читателя — остаются русскими.
package i18n

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Lang — язык интерфейса.
type Lang string

const (
	RU Lang = "ru"
	IT Lang = "it"

	// Default — язык по умолчанию: тексты в исходниках написаны на нём.
	Default = RU
)

// Langs — поддерживаемые языки.
var Langs = []Lang{RU, IT}

// Valid — известный язык.
func (l Lang) Valid() bool { return l == RU || l == IT }

// Tag — тег BCP 47 для заголовка Content-Language.
func (l Lang) Tag() string {
	if l == IT {
		return "it-IT"
	}
	return "ru-RU"
}

// Parse выбирает язык по заголовку Accept-Language («it-IT,it;q=0.9,en;q=0.8»): побеждает язык с наибольшим весом
// из поддерживаемых, при равенстве — тот, что стоит раньше. Ничего подходящего — русский.
func Parse(header string) Lang {
	best, bestQ := Default, -1.0
	for _, part := range strings.Split(header, ",") {
		tag, q := strings.TrimSpace(part), 1.0
		if i := strings.Index(tag, ";"); i >= 0 {
			for _, param := range strings.Split(tag[i+1:], ";") {
				if v, ok := strings.CutPrefix(strings.TrimSpace(param), "q="); ok {
					if f, err := strconv.ParseFloat(v, 64); err == nil {
						q = f
					}
				}
			}
			tag = strings.TrimSpace(tag[:i])
		}
		base := strings.ToLower(strings.SplitN(tag, "-", 2)[0])
		for _, l := range Langs {
			if string(l) == base && q > bestQ {
				best, bestQ = l, q
			}
		}
	}
	return best
}

type ctxKey struct{}

// WithLang кладёт язык в контекст запроса.
func WithLang(ctx context.Context, l Lang) context.Context {
	return context.WithValue(ctx, ctxKey{}, l)
}

// From — язык запроса; без языка в контексте — русский.
func From(ctx context.Context) Lang {
	if ctx != nil {
		if l, ok := ctx.Value(ctxKey{}).(Lang); ok && l.Valid() {
			return l
		}
	}
	return Default
}

// catalogs — переводы по языкам: русский текст → перевод (формат с теми же подстановками).
var catalogs = map[Lang]map[string]string{IT: {}}

// register добавляет переводы; повторный ключ — ошибка программы (два разных перевода одного текста).
func register(lang Lang, entries map[string]string) {
	c := catalogs[lang]
	for k, v := range entries {
		if _, dup := c[k]; dup {
			panic("i18n: повторный ключ каталога: " + k)
		}
		c[k] = v
	}
}

// Translate возвращает перевод текста msg (формата) без подстановки значений; нет перевода — сам msg.
func (l Lang) Translate(msg string) string {
	if l == Default {
		return msg
	}
	if s, ok := catalogs[l][msg]; ok {
		return s
	}
	return msg
}

// Has — есть ли перевод (для русского — всегда).
func (l Lang) Has(msg string) bool {
	if l == Default {
		return true
	}
	_, ok := catalogs[l][msg]
	return ok
}

// Msg — русский текст, который сам нужно перевести, когда он подставляется в другое сообщение:
// Lang.T("ожидается %s", i18n.Msg("строка")) — на итальянском «è previsto stringa».
type Msg string

// Resolve — копия args, где значения типа Msg заменены переводом на язык l (остальные — как есть).
func Resolve(l Lang, args []any) []any {
	if len(args) == 0 {
		return args
	}
	out := make([]any, len(args))
	for i, a := range args {
		if m, ok := a.(Msg); ok {
			out[i] = l.Translate(string(m))
		} else {
			out[i] = a
		}
	}
	return out
}

// T переводит формат msg и подставляет в него args, как fmt.Sprintf; аргументы типа Msg переводятся перед подстановкой.
func (l Lang) T(msg string, args ...any) string {
	return fmt.Sprintf(l.Translate(msg), Resolve(l, args)...)
}

// Catalog — копия каталога языка (для проверок).
func Catalog(l Lang) map[string]string {
	out := make(map[string]string, len(catalogs[l]))
	for k, v := range catalogs[l] {
		out[k] = v
	}
	return out
}

// Keys — ключи каталога в порядке.
func Keys(l Lang) []string {
	out := make([]string, 0, len(catalogs[l]))
	for k := range catalogs[l] {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
