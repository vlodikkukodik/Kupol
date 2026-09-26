// Package domainproof — подтверждение владения доменом, который добавляют прямо в разделах «DNS» и «Почта» (без привязки к сайту).
// Владелец создаёт у своего текущего DNS-провайдера TXT-запись «_vladhost-verify.домен» со значением «vladhost-verify=<код>»; панель находит её
// в публичном DNS. Пока домен не подтверждён, он на сервере не обслуживается, а подтвердить его первым может только тот, кто управляет его DNS.
package domainproof

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"strings"
	"time"
)

const (
	namePrefix  = "_vladhost-verify."
	valuePrefix = "vladhost-verify="
)

// Token создаёт случайный код подтверждения для домена.
func Token() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// RecordName — имя TXT-записи, которую нужно создать.
func RecordName(domain string) string {
	return namePrefix + strings.ToLower(strings.TrimSuffix(domain, "."))
}

// RecordValue — значение TXT-записи для кода.
func RecordValue(token string) string { return valuePrefix + token }

// LookupTXT — DNS-запрос TXT (подменяется в тестах).
type LookupTXT func(ctx context.Context, name string) ([]string, error)

// Check ищет в публичном DNS TXT-запись подтверждения. Части длинной записи склеены самим резолвером; кавычки и пробелы по краям не важны.
// Запись с пустым кодом не принимается никогда.
func Check(ctx context.Context, lookup LookupTXT, domain, token string) bool {
	if token == "" || lookup == nil {
		return false
	}
	c, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	records, _ := lookup(c, RecordName(domain)) // ошибка DNS — просто «не нашли»
	want := RecordValue(token)
	for _, r := range records {
		if strings.TrimSpace(strings.Trim(strings.TrimSpace(r), `"`)) == want {
			return true
		}
	}
	return false
}
