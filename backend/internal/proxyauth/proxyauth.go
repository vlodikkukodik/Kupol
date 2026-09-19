// Package proxyauth реализует подпись запросов между PHP-прокси и Go API.
//
// PHP-прокси на shared-хостинге ставит реальный IP клиента и подписывает запрос
// общим секретом. Go доверяет IP из заголовка только при валидной подписи;
// иначе IP берётся из TCP-соединения (или из доверенного обратного прокси).
//
// Формат подписи (должен совпадать с frontend/public/api/index.php):
//
//	HMAC-SHA256(secret, "KUPOL-PROXY-V1\n" + ts + "\n" + ip + "\n" + METHOD + "\n" + requestURI)
//
// в hex, где ts — Unix-время в секундах, requestURI — сырая строка запроса
// (путь + query) ровно так, как она уходит на Go.
package proxyauth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/netip"
	"strconv"
	"strings"
	"time"
)

const (
	HeaderIP        = "X-Kupol-Client-Ip"
	HeaderTimestamp = "X-Kupol-Timestamp"
	HeaderSignature = "X-Kupol-Signature"

	// MaxSkew — допустимое расхождение часов прокси и API (в обе стороны).
	MaxSkew = 60 * time.Second

	prefix = "KUPOL-PROXY-V1"
)

var (
	ErrMissing   = errors.New("proxyauth: заголовки подписи отсутствуют")
	ErrBadIP     = errors.New("proxyauth: некорректный IP")
	ErrBadTime   = errors.New("proxyauth: некорректное время")
	ErrStale     = errors.New("proxyauth: подпись просрочена")
	ErrSignature = errors.New("proxyauth: неверная подпись")
)

func canonical(ts int64, ip, method, requestURI string) string {
	return prefix + "\n" + strconv.FormatInt(ts, 10) + "\n" + ip + "\n" + strings.ToUpper(method) + "\n" + requestURI
}

// Sign считает подпись. Используется в тестах и служебных проверках;
// в бою подписывает PHP-прокси.
func Sign(secret []byte, ts int64, ip, method, requestURI string) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(canonical(ts, ip, method, requestURI)))
	return hex.EncodeToString(mac.Sum(nil))
}

// Verify проверяет подпись и возвращает подтверждённый IP клиента.
// ErrMissing означает «запрос пришёл не через прокси» — это не атака.
func Verify(secret []byte, now time.Time, ipHdr, tsHdr, sigHdr, method, requestURI string) (netip.Addr, error) {
	if ipHdr == "" || tsHdr == "" || sigHdr == "" {
		return netip.Addr{}, ErrMissing
	}

	ip, err := netip.ParseAddr(ipHdr)
	if err != nil {
		return netip.Addr{}, ErrBadIP
	}
	ip = ip.Unmap()
	// В подпись входит строка IP ровно как её прислал прокси, поэтому Sign ниже
	// получает ipHdr, а не нормализованный ip.

	ts, err := strconv.ParseInt(tsHdr, 10, 64)
	if err != nil {
		return netip.Addr{}, ErrBadTime
	}
	skew := now.Sub(time.Unix(ts, 0))
	if skew < -MaxSkew || skew > MaxSkew {
		return netip.Addr{}, ErrStale
	}

	got, err := hex.DecodeString(sigHdr)
	if err != nil {
		return netip.Addr{}, ErrSignature
	}
	want, _ := hex.DecodeString(Sign(secret, ts, ipHdr, method, requestURI))
	if !hmac.Equal(got, want) {
		return netip.Addr{}, ErrSignature
	}
	return ip, nil
}
