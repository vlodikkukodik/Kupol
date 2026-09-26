// Package userdb — пользовательские базы данных: создание и удаление баз в PostgreSQL и MariaDB, пароли, внешний доступ по
// списку IP, лимит размера и временные входы для веб-клиента. Панель управляет серверами СУБД через служебные учётные записи;
// у каждой пользовательской базы своя одноимённая учётная запись, которая видит только эту базу.
package userdb

import (
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"net/netip"
	"regexp"
	"sort"
	"strings"
)

// Engine — вид СУБД.
type Engine string

const (
	Postgres Engine = "postgres"
	MariaDB  Engine = "mariadb"
)

// Engines — поддерживаемые СУБД в порядке показа.
var Engines = []Engine{Postgres, MariaDB}

// Valid сообщает, известна ли СУБД.
func (e Engine) Valid() bool { return e == Postgres || e == MariaDB }

const (
	// MaxNameLen — длина части имени, которую выбирает пользователь. Полное имя: {пользователь}_{имя}, пользователь ≤ 32 символов,
	// итого не больше 53: помещается и в идентификаторы PostgreSQL (63), и в имена пользователей MariaDB.
	MaxNameLen = 20
	// MaxIPs — сколько адресов можно разрешить на одну базу.
	MaxIPs      = 10
	passwordLen = 24
)

var nameRe = regexp.MustCompile(`^[a-z0-9]{1,20}$`)

// ValidName проверяет часть имени, которую выбирает пользователь: латиница и цифры (подчёркивание — только разделитель).
func ValidName(name string) bool { return nameRe.MatchString(name) }

// FullName собирает полное имя базы. Имя пользователя панели не содержит «_», а часть имени — тоже, поэтому полные имена
// разных пользователей не пересекаются ({a}_{b_c} и {a_b}_{c} невозможны).
func FullName(username, name string) string { return username + "_" + name }

// Алфавит без похожих символов (0/O, 1/l/I): пароль читают глазами и вводят руками. Без спецсимволов: пароль попадает
// в строки подключения и URL как есть.
const passwordAlphabet = "abcdefghjkmnpqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ23456789"

// NewPassword выпускает случайный пароль базы.
func NewPassword() (string, error) {
	out := make([]byte, passwordLen)
	for i := range out {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(passwordAlphabet))))
		if err != nil {
			return "", err
		}
		out[i] = passwordAlphabet[n.Int64()]
	}
	return string(out), nil
}

// ErrBadAddr — недопустимый адрес для внешнего доступа.
var ErrBadAddr = errors.New("userdb: invalid address")

// NormalizeAddr приводит разрешённый адрес к каноническому виду: одиночный IP («203.0.113.5», «2001:db8::1») или сеть IPv4 не шире /24
// («203.0.113.0/24», «/32» сокращается до адреса). IPv6-сети MariaDB не поддерживает, поэтому для IPv6 — только одиночные адреса.
// Loopback, неуказанные, мультикаст и link-local адреса отклоняются.
func NormalizeAddr(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || len(raw) > 64 || strings.ContainsAny(raw, " \t\r\n\x00\"'\\;#,") {
		return "", ErrBadAddr
	}
	if strings.Contains(raw, "/") {
		p, err := netip.ParsePrefix(raw)
		if err != nil || !p.Addr().Is4() || p.Bits() < 24 {
			return "", ErrBadAddr
		}
		p = p.Masked()
		if bad(p.Addr()) {
			return "", ErrBadAddr
		}
		if p.Bits() == 32 {
			return p.Addr().String(), nil
		}
		return p.String(), nil
	}
	a, err := netip.ParseAddr(raw)
	if err != nil || a.Zone() != "" {
		return "", ErrBadAddr
	}
	a = a.Unmap()
	if bad(a) {
		return "", ErrBadAddr
	}
	return a.String(), nil
}

func bad(a netip.Addr) bool {
	return a.IsLoopback() || a.IsUnspecified() || a.IsMulticast() || a.IsLinkLocalUnicast() || a.IsLinkLocalMulticast()
}

// NormalizeAddrs проверяет список, убирает повторы и сортирует. Не больше MaxIPs.
func NormalizeAddrs(in []string) ([]string, error) {
	seen := map[string]bool{}
	var out []string
	for _, r := range in {
		a, err := NormalizeAddr(r)
		if err != nil {
			return nil, fmt.Errorf("%w: %q", err, r)
		}
		if !seen[a] {
			seen[a] = true
			out = append(out, a)
		}
	}
	if len(out) > MaxIPs {
		return nil, fmt.Errorf("userdb: at most %d addresses", MaxIPs)
	}
	sort.Strings(out)
	return out, nil
}
