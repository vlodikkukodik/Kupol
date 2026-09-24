package sites

import (
	"context"
	"errors"
	"log"
	"net"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"golang.org/x/net/idna"
	"golang.org/x/net/publicsuffix"
	"gorm.io/gorm"

	"vladhost/internal/apperr"
)

// Свои домены сайта. Владение доказывает сам факт, что A-запись домена указывает на IP этого сервера: изменить её
// может только тот, кто управляет DNS домена. Сертификат выпускает тот же выпускатель, что и для адресов сайтов.

const (
	DomainPendingDNS  = "pending_dns"
	DomainPendingCert = "pending_cert"
	DomainActive      = "active"
	DomainFailed      = "failed"

	// Причины, по которым DNS не подошёл (по ним фронтенд показывает переведённую подсказку).
	ProblemNoA     = "no_a"     // у домена нет A-записи
	ProblemWrongIP = "wrong_ip" // A-запись указывает не на наш сервер
	ProblemHasAAAA = "has_aaaa" // есть AAAA-запись: сервер работает только по IPv4
	ProblemLookup  = "lookup"   // DNS-сервер не ответил

	domainStaleAfter = 14 * 24 * time.Hour // домен без верной A-записи освобождается через две недели
	dnsRecheckEvery  = time.Minute
	maxDomainNameLen = 253
	defaultPerSite   = 5
	defaultPerUser   = 10
)

var (
	ErrDomainInvalid   = apperr.New(http.StatusUnprocessableEntity, "domain_invalid", "invalid domain name").OnField("host")
	ErrDomainReserved  = apperr.New(http.StatusUnprocessableEntity, "domain_reserved", "domain is reserved").OnField("host")
	ErrDomainTaken     = apperr.New(http.StatusConflict, "domain_taken", "domain is already connected").OnField("host")
	ErrDomainLimitSite = apperr.New(http.StatusForbidden, "domain_limit_site", "domain limit per site reached")
	ErrDomainLimitUser = apperr.New(http.StatusForbidden, "domain_limit_user", "domain limit per account reached")
	ErrDomainsDisabled = apperr.New(http.StatusConflict, "domains_unavailable", "custom domains are not enabled")
	ErrDomainNotFound  = apperr.New(http.StatusNotFound, "domain_not_found", "domain not found")
)

type Domain struct {
	ID              int64      `gorm:"primaryKey" json:"id"`
	SiteID          int64      `json:"-"`
	Host            string     `json:"host"`
	Status          string     `json:"status"`
	Problem         string     `json:"problem"`
	Found           string     `json:"-"` // найденные IP через запятую
	Error           string     `json:"error"`
	DNSCheckedAt    *time.Time `json:"-"`
	VerifiedAt      *time.Time `json:"verified_at"`
	CertRequestedAt *time.Time `json:"-"`
	CreatedAt       time.Time  `json:"created_at"`
}

// FoundIPs возвращает адреса, найденные при последней проверке DNS.
func (d Domain) FoundIPs() []string {
	if d.Found == "" {
		return []string{}
	}
	return strings.Split(d.Found, ",")
}

// Resolver — то, чем проверяется DNS (в тестах подменяется).
type Resolver interface {
	LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error)
}

type DomainConfig struct {
	ServerIPs  []string // IP этого сервера: A-запись домена должна указывать на них
	MappingDir string   // /data/vladhost/domains: {домен} → адрес сайта; читает веб-шлюз
	Resolver   Resolver
	PerSite    int
	PerUser    int
}

func (s *Service) ConfigureDomains(c DomainConfig) {
	if c.Resolver == nil {
		c.Resolver = &net.Resolver{PreferGo: true}
	}
	if c.PerSite <= 0 {
		c.PerSite = defaultPerSite
	}
	if c.PerUser <= 0 {
		c.PerUser = defaultPerUser
	}
	s.domains = &c
}

func (s *Service) DomainsEnabled() bool {
	return s.domains != nil && len(s.domains.ServerIPs) > 0 && s.domains.MappingDir != ""
}

// DomainInfo — сведения для интерфейса: на какие адреса направлять A-запись и сколько доменов можно.
func (s *Service) DomainInfo() (ips []string, perSite int) {
	if !s.DomainsEnabled() {
		return []string{}, 0
	}
	return s.domains.ServerIPs, s.domains.PerSite
}

var domainRe = regexp.MustCompile(`^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z][a-z0-9-]{0,61}[a-z0-9]$`)

// Зоны, которые не существуют в публичном DNS: сертификат для них не выпустить, а имя может указывать
// на внутренние ресурсы.
var reservedSuffixes = []string{".local", ".localhost", ".internal", ".test", ".example", ".invalid", ".localdomain", ".home.arpa", ".onion", ".lan", ".corp", ".intranet", ".private"}

// NormalizeDomain приводит имя к виду для хранения (нижний регистр, punycode) и проверяет его.
func NormalizeDomain(raw, baseDomain string) (string, error) {
	h := strings.ToLower(strings.TrimSpace(raw))
	h = strings.TrimSuffix(h, ".")
	if h == "" || len(h) > 4*maxDomainNameLen || strings.ContainsAny(h, "/:@ \t\r\n\x00*?#\\") {
		return "", ErrDomainInvalid
	}
	ascii, err := idna.Lookup.ToASCII(h)
	if err != nil {
		return "", ErrDomainInvalid
	}
	h = ascii
	if len(h) > maxDomainNameLen || !domainRe.MatchString(h) {
		return "", ErrDomainInvalid
	}
	if _, err := netip.ParseAddr(h); err == nil {
		return "", ErrDomainInvalid
	}
	base := strings.ToLower(baseDomain)
	if h == base || strings.HasSuffix(h, "."+base) {
		return "", ErrDomainReserved // сайты на нашем домене создаются как обычные адреса, а служебные имена заняты
	}
	for _, suf := range reservedSuffixes {
		if strings.HasSuffix(h, suf) {
			return "", ErrDomainReserved
		}
	}
	// Публичный суффикс целиком (com, co.uk, github.io) подключить нельзя: он принадлежит не пользователю.
	if _, err := publicsuffix.EffectiveTLDPlusOne(h); err != nil {
		return "", ErrDomainInvalid
	}
	return h, nil
}

func mapUniqueDomain(err error) error {
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == "23505" {
		return ErrDomainTaken
	}
	return err
}

// AddDomain привязывает домен к сайту. Сразу выполняется первая проверка DNS.
func (s *Service) AddDomain(ctx context.Context, userID, siteID int64, raw string) (*Domain, error) {
	if !s.DomainsEnabled() {
		return nil, ErrDomainsDisabled
	}
	site, err := s.Get(ctx, userID, siteID)
	if err != nil {
		return nil, err
	}
	host, err := NormalizeDomain(raw, s.baseDomain)
	if err != nil {
		return nil, err
	}
	var d *Domain
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Блокировка строки сайта сериализует параллельные добавления и не даёт обойти лимиты.
		if err := tx.Raw("SELECT id FROM sites WHERE id = ? FOR UPDATE", site.ID).Scan(new(int64)).Error; err != nil {
			return err
		}
		var perSite, perUser int64
		if err := tx.Model(&Domain{}).Where("site_id = ?", site.ID).Count(&perSite).Error; err != nil {
			return err
		}
		if int(perSite) >= s.domains.PerSite {
			return ErrDomainLimitSite.With(s.domains.PerSite)
		}
		if err := tx.Table("domains").Joins("JOIN sites ON sites.id = domains.site_id").
			Where("sites.user_id = ?", userID).Count(&perUser).Error; err != nil {
			return err
		}
		if int(perUser) >= s.domains.PerUser {
			return ErrDomainLimitUser.With(s.domains.PerUser)
		}
		d = &Domain{SiteID: site.ID, Host: host, Status: DomainPendingDNS}
		if err := tx.Create(d).Error; err != nil {
			return mapUniqueDomain(err)
		}
		return s.writeMapping(host, site.Host)
	})
	if err != nil {
		return nil, err
	}
	s.advanceDomain(ctx, d)
	return d, nil
}

func (s *Service) writeMapping(host, siteHost string) error {
	if err := os.MkdirAll(s.domains.MappingDir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(s.domains.MappingDir, host), []byte(siteHost+"\n"), 0o644)
}

func (s *Service) removeMapping(host string) {
	if s.domains == nil {
		return
	}
	if err := os.Remove(filepath.Join(s.domains.MappingDir, host)); err != nil && !errors.Is(err, os.ErrNotExist) {
		log.Printf("удаление привязки домена %s: %v", host, err)
	}
}

// ListDomains — домены сайта.
func (s *Service) ListDomains(ctx context.Context, userID, siteID int64) ([]Domain, error) {
	if _, err := s.Get(ctx, userID, siteID); err != nil {
		return nil, err
	}
	var out []Domain
	err := s.db.WithContext(ctx).Where("site_id = ?", siteID).Order("id").Find(&out).Error
	return out, err
}

// DomainsByUser возвращает домены всех сайтов пользователя, сгруппированные по сайту (для списка сайтов).
func (s *Service) DomainsByUser(ctx context.Context, userID int64) (map[int64][]Domain, error) {
	var rows []Domain
	err := s.db.WithContext(ctx).Table("domains").Select("domains.*").
		Joins("JOIN sites ON sites.id = domains.site_id").Where("sites.user_id = ?", userID).
		Order("domains.id").Find(&rows).Error
	out := map[int64][]Domain{}
	for _, d := range rows {
		out[d.SiteID] = append(out[d.SiteID], d)
	}
	return out, err
}

func (s *Service) getDomain(ctx context.Context, userID, siteID, id int64) (*Domain, *Site, error) {
	site, err := s.Get(ctx, userID, siteID)
	if err != nil {
		return nil, nil, err
	}
	var d Domain
	err = s.db.WithContext(ctx).Where("id = ? AND site_id = ?", id, site.ID).First(&d).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil, ErrDomainNotFound
	}
	return &d, site, err
}

// CheckDomain перепроверяет DNS (и повторяет выпуск сертификата для домена в состоянии failed).
func (s *Service) CheckDomain(ctx context.Context, userID, siteID, id int64) (*Domain, error) {
	d, _, err := s.getDomain(ctx, userID, siteID, id)
	if err != nil {
		return nil, err
	}
	if d.Status == DomainPendingDNS || d.Status == DomainFailed {
		s.advanceDomain(ctx, d)
	}
	return d, nil
}

// RemoveDomain отвязывает домен, удаляет привязку в шлюзе и просит выпускатель убрать сертификат и настройку nginx.
func (s *Service) RemoveDomain(ctx context.Context, userID, siteID, id int64) error {
	d, _, err := s.getDomain(ctx, userID, siteID, id)
	if err != nil {
		return err
	}
	if err := s.db.WithContext(ctx).Delete(&Domain{}, d.ID).Error; err != nil {
		return err
	}
	s.releaseDomain(d.Host)
	return nil
}

func (s *Service) releaseDomain(host string) {
	s.removeMapping(host)
	if s.certsEnabled() {
		if err := s.enqueue("delete", host); err != nil {
			log.Printf("заявка на удаление домена %s: %v", host, err)
		}
	}
}

// checkDNS проверяет, что A-записи домена указывают только на наш сервер и нет AAAA-записей.
func (s *Service) checkDNS(ctx context.Context, host string) (ok bool, problem string, found []string) {
	ctx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	// Имя запрашивается как абсолютное (точка в конце). Без неё резолвер сервера при ответе «нет записи» пробует
	// достроить имя search-доменом хостера (example.com.<их-домен>), у которого есть wildcard-запись на этот же IP, —
	// и любое чужое имя выглядело бы указывающим на нас.
	addrs, err := s.domains.Resolver.LookupIPAddr(ctx, host+".")
	if err != nil {
		var dnsErr *net.DNSError
		if errors.As(err, &dnsErr) && dnsErr.IsNotFound {
			return false, ProblemNoA, nil
		}
		return false, ProblemLookup, nil
	}
	var v4, v6 []string
	for _, a := range addrs {
		ip, ok := netip.AddrFromSlice(a.IP)
		if !ok {
			continue
		}
		ip = ip.Unmap()
		if ip.Is4() {
			v4 = append(v4, ip.String())
		} else {
			v6 = append(v6, ip.String())
		}
	}
	slices.Sort(v4)
	slices.Sort(v6)
	switch {
	case len(v4) == 0:
		return false, ProblemNoA, v6
	case slices.ContainsFunc(v4, func(ip string) bool { return !slices.Contains(s.domains.ServerIPs, ip) }):
		return false, ProblemWrongIP, v4
	case len(v6) > 0:
		return false, ProblemHasAAAA, v6
	}
	return true, "", v4
}

// advanceDomain проверяет DNS и, если он верный, переводит домен к выпуску сертификата.
func (s *Service) advanceDomain(ctx context.Context, d *Domain) {
	ok, problem, found := s.checkDNS(ctx, d.Host)
	now := time.Now()
	upd := map[string]any{"dns_checked_at": now, "problem": problem, "found": strings.Join(found, ",")}
	d.DNSCheckedAt, d.Problem, d.Found = &now, problem, strings.Join(found, ",")
	if ok {
		upd["verified_at"], upd["error"] = now, ""
		d.VerifiedAt, d.Error = &now, ""
		if s.certsEnabled() {
			if err := s.enqueue("issue", d.Host); err != nil {
				log.Printf("заявка на сертификат домена %s: %v", d.Host, err)
				return
			}
			upd["status"], upd["cert_requested_at"] = DomainPendingCert, now
			d.Status, d.CertRequestedAt = DomainPendingCert, &now
		} else {
			upd["status"] = DomainActive // выпуск сертификатов выключен (разработка): домен сразу рабочий
			d.Status = DomainActive
		}
	} else if d.Status == DomainFailed {
		return // сертификат не выпущен, а DNS сейчас неверный: оставляем прежнюю причину отказа
	}
	if err := s.db.WithContext(ctx).Model(&Domain{}).Where("id = ?", d.ID).Updates(upd).Error; err != nil {
		log.Printf("домены: обновление %s: %v", d.Host, err)
	}
}

// WatchDomains раз в interval перепроверяет DNS доменов, ждущих A-запись, следит за выпуском сертификатов
// и освобождает домены, которые две недели не получили верную запись.
func (s *Service) WatchDomains(ctx context.Context, interval time.Duration) {
	if !s.DomainsEnabled() {
		return
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		if err := s.reconcileDomains(ctx); err != nil && ctx.Err() == nil {
			log.Printf("домены: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (s *Service) reconcileDomains(ctx context.Context) error {
	var waiting []Domain
	if err := s.db.WithContext(ctx).Where("status IN ?", []string{DomainPendingDNS, DomainPendingCert}).Find(&waiting).Error; err != nil {
		return err
	}
	for i := range waiting {
		d := &waiting[i]
		switch d.Status {
		case DomainPendingDNS:
			if time.Since(d.CreatedAt) > domainStaleAfter {
				if err := s.db.WithContext(ctx).Delete(&Domain{}, d.ID).Error; err != nil {
					return err
				}
				s.releaseDomain(d.Host)
				continue
			}
			if d.DNSCheckedAt == nil || time.Since(*d.DNSCheckedAt) >= dnsRecheckEvery {
				s.advanceDomain(ctx, d)
			}
		case DomainPendingCert:
			status, msg, ok := s.readHostStatus(d.Host, d.CertRequestedAt)
			if !ok {
				if d.CertRequestedAt != nil && time.Since(*d.CertRequestedAt) > certPendingTimeout {
					status, msg = CertFailed, "certificate issuer did not respond in time"
				} else {
					continue
				}
			}
			next := DomainActive
			if status == CertFailed {
				next = DomainFailed
			}
			if err := s.db.WithContext(ctx).Model(&Domain{}).Where("id = ?", d.ID).
				Updates(map[string]any{"status": next, "error": msg}).Error; err != nil {
				return err
			}
		}
	}
	return nil
}
