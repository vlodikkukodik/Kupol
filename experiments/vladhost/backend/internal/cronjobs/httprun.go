package cronjobs

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"syscall"
	"time"
)

const userAgent = "Vladhost-Cron/1.0"

var (
	errPrivate   = errors.New("cronjobs: address is in an internal network")
	errBadURL    = errors.New("cronjobs: invalid url")
	errRedirects = errors.New("too many redirects")
)

// cgnat — 100.64.0.0/10: общий адрес провайдеров и облаков, для наших целей то же, что внутренняя сеть.
var cgnat = netip.MustParsePrefix("100.64.0.0/10")

func privateAddr(a netip.Addr) bool {
	a = a.Unmap()
	return a.IsLoopback() || a.IsPrivate() || a.IsLinkLocalUnicast() || a.IsLinkLocalMulticast() || a.IsMulticast() ||
		a.IsUnspecified() || cgnat.Contains(a)
}

// checkURL проверяет адрес задачи: http(s), без логина в адресе, не IP внутренней сети.
func checkURL(raw string, allowPrivate bool) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || len(raw) > 2000 {
		return nil, errBadURL
	}
	if a, err := netip.ParseAddr(u.Hostname()); err == nil && !allowPrivate && privateAddr(a) {
		return nil, errPrivate
	}
	return u, nil
}

// newHTTPClient делает клиент для задач. Защита от обращения во внутреннюю сеть стоит на самом соединении (Control получает
// уже разрешённый адрес), поэтому её не обойти ни именем, которое указывает на 127.0.0.1, ни подменой DNS, ни редиректом.
func newHTTPClient(timeout time.Duration, allowPrivate bool) *http.Client {
	d := &net.Dialer{Timeout: 10 * time.Second}
	if !allowPrivate {
		d.Control = func(_, address string, _ syscall.RawConn) error {
			ap, err := netip.ParseAddrPort(address)
			if err != nil || privateAddr(ap.Addr()) {
				return errPrivate
			}
			return nil
		}
	}
	return &http.Client{
		Timeout:   timeout,
		Transport: &http.Transport{DialContext: d.DialContext, Proxy: nil, DisableKeepAlives: true, TLSHandshakeTimeout: 10 * time.Second},
		CheckRedirect: func(_ *http.Request, via []*http.Request) error {
			if len(via) > 3 {
				return errRedirects
			}
			return nil
		},
	}
}

// runHTTP выполняет запрос и возвращает успех, HTTP-статус и текст для журнала.
func runHTTP(ctx context.Context, c *http.Client, raw string, allowPrivate bool) (ok bool, code int, out, reason string) {
	u, err := checkURL(raw, allowPrivate)
	if err != nil {
		return false, 0, "", reasonOf(err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return false, 0, "", "bad_url"
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := c.Do(req)
	if err != nil {
		if r := reasonOf(err); r != "" {
			return false, 0, "", r
		}
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return false, 0, err.Error(), ""
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
	return resp.StatusCode < 400, resp.StatusCode, fmt.Sprintf("HTTP %s\n%s", resp.Status, body), ""
}

// reasonOf переводит известные ошибки в коды, которые интерфейс показывает на языке пользователя.
func reasonOf(err error) string {
	switch {
	case errors.Is(err, errPrivate):
		return "private_address"
	case errors.Is(err, errBadURL):
		return "bad_url"
	case errors.Is(err, errRedirects):
		return "too_many_redirects"
	}
	return ""
}
