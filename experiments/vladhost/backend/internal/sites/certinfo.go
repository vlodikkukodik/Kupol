package sites

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gorm.io/gorm"

	"vladhost/internal/apperr"
)

// Сведения о сертификате (издатель, срок, имена) пишет root-скрипт cert-info.sh в {CertsDir}/info/{host}:
// сами сертификаты лежат в каталоге, закрытом от панели. Формат — строки key=value.

const (
	// RenewCooldown — как часто можно перевыпускать сертификат вручную. У Let's Encrypt лимит на повторные сертификаты
	// для одного набора имён (5 в неделю), а автопродление certbot не считается ручным: оно идёт само за 30 дней до срока.
	RenewCooldown = 72 * time.Hour
	// ExpiryWarnDays — за сколько суток до окончания панель предупреждает: автопродление к этому сроку уже должно было сработать.
	ExpiryWarnDays = 14
	maxInfoSize    = 8 << 10
)

var (
	ErrCertRenewState    = apperr.New(http.StatusConflict, "cert_renew_state", "certificate cannot be renewed now")
	ErrCertRenewCooldown = apperr.New(http.StatusTooManyRequests, "cert_renew_cooldown", "certificate was requested recently").With(int(RenewCooldown / time.Hour))
)

// CertInfo — сведения о выпущенном сертификате.
type CertInfo struct {
	Issuer    string    `json:"issuer"`
	NotBefore time.Time `json:"not_before"`
	NotAfter  time.Time `json:"not_after"`
	Names     []string  `json:"names"`
}

// DaysLeft — сколько целых суток осталось до окончания (отрицательно, если сертификат уже истёк).
func (c CertInfo) DaysLeft(now time.Time) int {
	d := c.NotAfter.Sub(now)
	days := int(d / (24 * time.Hour))
	if d < 0 && d%(24*time.Hour) != 0 {
		days-- // округление к меньшему: 12 часов назад — это «-1», а не «0»
	}
	return days
}

func printable(s string, max int) string {
	var b strings.Builder
	for _, r := range s {
		if r >= 0x20 && r != 0x7f && r != '=' {
			b.WriteRune(r)
		}
		if b.Len() >= max {
			break
		}
	}
	return strings.TrimSpace(b.String())
}

// parseCertInfo разбирает файл сведений; повреждённый или неполный файл — nil.
func parseCertInfo(data []byte) *CertInfo {
	var c CertInfo
	var haveFrom, haveTo bool
	for _, line := range strings.Split(string(data), "\n") {
		k, v, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok {
			continue
		}
		switch k {
		case "not_before":
			if t, err := time.Parse(time.RFC3339, v); err == nil {
				c.NotBefore, haveFrom = t, true
			}
		case "not_after":
			if t, err := time.Parse(time.RFC3339, v); err == nil {
				c.NotAfter, haveTo = t, true
			}
		case "issuer":
			c.Issuer = printable(v, 120)
		case "names":
			for _, n := range strings.Split(v, ",") {
				if n = printable(n, 253); n != "" && len(c.Names) < 20 {
					c.Names = append(c.Names, n)
				}
			}
		}
	}
	if !haveFrom || !haveTo || !c.NotAfter.After(c.NotBefore) {
		return nil
	}
	if c.Names == nil {
		c.Names = []string{}
	}
	return &c
}

// CertInfo возвращает сведения о сертификате хоста или nil, если их нет (сертификат не выпущен или выпущен до появления файла).
func (s *Service) CertInfo(host string) *CertInfo {
	if !s.certsEnabled() || host == "" || strings.ContainsAny(host, "/\\\x00") || strings.HasPrefix(host, ".") {
		return nil
	}
	f, err := os.Open(filepath.Join(s.certsDir, "info", host))
	if err != nil {
		return nil
	}
	defer func() { _ = f.Close() }()
	buf := make([]byte, maxInfoSize)
	n, _ := f.Read(buf)
	return parseCertInfo(buf[:n])
}

// hasValidCert: у хоста есть действующий сертификат (например, после неудачного перевыпуска старый продолжает работать).
func (s *Service) hasValidCert(host string) bool {
	c := s.CertInfo(host)
	return c != nil && c.NotAfter.After(time.Now())
}

// RenewAvailableAt — когда снова можно перевыпустить сертификат вручную; nil — можно сейчас.
func RenewAvailableAt(requestedAt *time.Time) *time.Time {
	if requestedAt == nil {
		return nil
	}
	if at := requestedAt.Add(RenewCooldown); at.After(time.Now()) {
		return &at
	}
	return nil
}

// RenewCert перевыпускает сертификат адреса сайта или его домена (своего или поддомена) заново. Только для работающего
// сертификата и не чаще раза в RenewCooldown: у Let's Encrypt есть лимиты, общие на весь сервер.
func (s *Service) RenewCert(ctx context.Context, userID, siteID int64, host string) error {
	site, err := s.Get(ctx, userID, siteID)
	if err != nil {
		return err
	}
	if !s.certsEnabled() {
		return ErrCertRenewState
	}
	host = strings.ToLower(strings.TrimSpace(host))
	if host == site.Host {
		if site.CertStatus != CertActive {
			return ErrCertRenewState
		}
		if RenewAvailableAt(site.CertRequestedAt) != nil {
			return ErrCertRenewCooldown
		}
		if err := s.enqueue("renew", host); err != nil {
			return err
		}
		return s.db.WithContext(ctx).Model(site).Updates(map[string]any{
			"cert_status": CertPending, "cert_error": "", "cert_requested_at": time.Now(),
		}).Error
	}
	var d Domain
	if err := s.db.WithContext(ctx).Where("site_id = ? AND host = ?", site.ID, host).First(&d).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrDomainNotFound
		}
		return err
	}
	if d.Status != DomainActive {
		return ErrCertRenewState
	}
	if RenewAvailableAt(d.CertRequestedAt) != nil {
		return ErrCertRenewCooldown
	}
	if err := s.enqueue("renew", host); err != nil {
		return err
	}
	return s.db.WithContext(ctx).Model(&Domain{}).Where("id = ?", d.ID).Updates(map[string]any{
		"status": DomainPendingCert, "error": "", "cert_requested_at": time.Now(),
	}).Error
}
