// Package i18n: тексты для пользователя на русском и итальянском. Язык запроса определяется по
// заголовку Accept-Language, который фронтенд выставляет по выбору пользователя в шапке.
package i18n

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

type Lang string

const (
	RU Lang = "ru"
	IT Lang = "it"

	Default = RU
)

var catalogs = map[Lang]map[string]string{RU: ru, IT: it}

// Supported — языки, для которых есть полный каталог.
func Supported() []Lang { return []Lang{RU, IT} }

// FromAcceptLanguage выбирает язык по заголовку Accept-Language с учётом весов (q). Если поддерживаемых
// языков в нём нет, возвращается язык по умолчанию.
func FromAcceptLanguage(header string) Lang {
	type cand struct {
		lang Lang
		q    float64
		pos  int
	}
	var cands []cand
	for i, part := range strings.Split(header, ",") {
		tag, params, _ := strings.Cut(strings.TrimSpace(part), ";")
		q := 1.0
		if v, ok := strings.CutPrefix(strings.TrimSpace(params), "q="); ok {
			if f, err := strconv.ParseFloat(v, 64); err == nil {
				q = f
			}
		}
		primary, _, _ := strings.Cut(strings.ToLower(strings.TrimSpace(tag)), "-")
		for _, l := range Supported() {
			if primary == string(l) && q > 0 {
				cands = append(cands, cand{l, q, i})
			}
		}
	}
	if len(cands) == 0 {
		return Default
	}
	sort.SliceStable(cands, func(a, b int) bool { return cands[a].q > cands[b].q })
	return cands[0].lang
}

// T возвращает текст по ключу на нужном языке и подставляет аргументы (как fmt.Sprintf).
// Если ключа нет в каталоге языка, берётся русский, а если и его нет — сам ключ (это видно сразу).
func T(lang Lang, key string, args ...any) string {
	msg, ok := catalogs[lang][key]
	if !ok {
		if msg, ok = catalogs[Default][key]; !ok {
			return key
		}
	}
	if len(args) == 0 {
		return msg
	}
	return fmt.Sprintf(msg, args...)
}

// Bi — текст на обоих языках через « / ». Для каналов, где язык клиента неизвестен (ответы FTP-сервера:
// в протоколе нет заголовка Accept-Language).
func Bi(key string, args ...any) string {
	ru, it := T(RU, key, args...), T(IT, key, args...)
	if ru == it {
		return ru
	}
	return ru + " / " + it
}

// Has сообщает, есть ли ключ в каталоге языка.
func Has(lang Lang, key string) bool {
	_, ok := catalogs[lang][key]
	return ok
}
