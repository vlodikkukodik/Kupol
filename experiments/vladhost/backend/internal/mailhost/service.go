package mailhost

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"log"
	"math/big"
	"net/http"
	"net/mail"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/GehirnInc/crypt/sha512_crypt"
	"gorm.io/gorm"

	"vladhost/internal/apperr"
	"vladhost/internal/auth"
	"vladhost/internal/domainproof"
	"vladhost/internal/runtimes"
	"vladhost/internal/sites"
)

var (
	ErrNotFound      = apperr.New(http.StatusNotFound, "mail_not_found", "mail object not found")
	ErrDomainInvalid = apperr.Validation("domain", "mail_domain", "invalid mail domain")
	ErrNotVerified   = apperr.New(http.StatusConflict, "mail_not_verified", "domain ownership is not confirmed yet")
	ErrUnverified    = apperr.New(http.StatusConflict, "mail_domain_unverified", "confirm domain ownership first")
	ErrDomainTaken   = apperr.New(http.StatusConflict, "mail_domain_taken", "domain already used for mail").OnField("domain")
	ErrDomainLimit   = apperr.New(http.StatusForbidden, "mail_domain_limit", "mail domain limit reached")
	ErrLocalInvalid  = apperr.Validation("local", "mail_local", "invalid mailbox name")
	ErrLocalTaken    = apperr.New(http.StatusConflict, "mail_local_taken", "address already used").OnField("local")
	ErrBoxLimit      = apperr.New(http.StatusForbidden, "mail_box_limit", "mailbox limit reached")
	ErrAliasLimit    = apperr.New(http.StatusForbidden, "mail_alias_limit", "alias limit reached")
	ErrPassword      = apperr.Validation("password", "mail_password", "password is too short")
	ErrQuota         = apperr.Validation("quota_mb", "mail_quota", "invalid quota")
	ErrDest          = apperr.Validation("to", "mail_dest", "invalid destination address")
	ErrDestLoop      = apperr.Validation("to", "mail_dest_loop", "alias points to itself")
	ErrRulesText     = apperr.Validation("subject", "mail_autoreply_text", "invalid autoreply text")
	ErrRulesDates    = apperr.Validation("from", "mail_autoreply_dates", "invalid autoreply dates")
	ErrRulesDays     = apperr.Validation("days", "mail_autoreply_days", "invalid autoreply period")
	ErrSyncFailed    = apperr.New(http.StatusBadGateway, "mail_sync_failed", "mail server did not apply the change yet")
	ErrHelperDown    = apperr.New(http.StatusServiceUnavailable, "mail_helper_down", "mail helper is not available")
)

// Пределы.
const (
	MaxDomains     = 5
	MaxMailboxes   = 10
	MaxAliases     = 20
	MaxDest        = 5
	DefaultQuotaMB = 500
	MinQuotaMB     = 50
	MaxQuotaMB     = 2000
	MinPassword    = 10
	Selector       = "vh1"
	ActionMailSync = "mail-sync"
)

var (
	domainRe = regexp.MustCompile(`^(?:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z][a-z0-9-]{0,61}[a-z0-9]$`)
	localRe  = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9._+-]{0,62}[a-z0-9])?$`)
)

// SiteDomains говорит, какие свои домены подключены к сайтам пользователя (реализует *sites.Service).
type SiteDomains interface {
	DomainsByUser(ctx context.Context, userID int64) (map[int64][]sites.Domain, error)
}

// Config — настройки.
type Config struct {
	Host       string // mail.vladinc.ru: MX и настройки клиентов
	WebmailURL string // адрес webmail для кнопки в интерфейсе; пусто — кнопки нет
	ServerIP   string // для записи SPF
	BaseDomain string // vladinc.ru: под нашим доменом почта не заводится
	Dir        string // папка обмена с исполнителем (mail/state.json пишет панель, mail/usage.json пишет исполнитель)
	Applier    runtimes.Applier
	Sites      SiteDomains
	Resolver   Resolver
	DNS        DNSHosting // наш собственный DNS; nil — автоматической настройки записей нет
}

// Service — почта.
type Service struct {
	db  *gorm.DB
	cfg Config
	now func() time.Time

	syncMu sync.Mutex
	dirty  bool
	mu     sync.Mutex
}

// New создаёт службу.
func New(db *gorm.DB, cfg Config) *Service {
	if cfg.Resolver == nil {
		cfg.Resolver = netResolver{}
	}
	return &Service{db: db, cfg: cfg, now: time.Now}
}

// Enabled: почта включена, если известны имя почтового сервера и папка исполнителя.
func (s *Service) Enabled() bool {
	return s != nil && s.cfg.Host != "" && s.cfg.Dir != "" && s.cfg.Applier != nil
}

// --- проверки ---

func normDomain(d string) (string, error) {
	d = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(d), "."))
	if len(d) > 253 || !domainRe.MatchString(d) || strings.Contains(d, "..") {
		return "", ErrDomainInvalid
	}
	return d, nil
}

func normLocal(l string, allowStar bool) (string, error) {
	l = strings.ToLower(strings.TrimSpace(l))
	if allowStar && l == "*" {
		return l, nil
	}
	if !localRe.MatchString(l) || strings.Contains(l, "..") {
		return "", ErrLocalInvalid
	}
	return l, nil
}

const passAlphabet = "abcdefghjkmnpqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ23456789"

func newPassword() (string, error) {
	out := make([]byte, 20)
	for i := range out {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(passAlphabet))))
		if err != nil {
			return "", err
		}
		out[i] = passAlphabet[n.Int64()]
	}
	return string(out), nil
}

const saltAlphabet = "./ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"

// hashPassword — SHA512-CRYPT в виде, который читает dovecot: «{SHA512-CRYPT}$6$соль$хеш».
func hashPassword(pw string) (string, error) {
	salt := make([]byte, 16)
	for i := range salt {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(saltAlphabet))))
		if err != nil {
			return "", err
		}
		salt[i] = saltAlphabet[n.Int64()]
	}
	h, err := sha512_crypt.New().Generate([]byte(pw), append([]byte("$6$"), salt...))
	if err != nil {
		return "", err
	}
	return "{SHA512-CRYPT}" + h, nil
}

// passwordOrNew: свой пароль (от MinPassword знаков, без управляющих символов) или новый случайный. Второй результат — показывать ли пароль.
func passwordOrNew(pw string) (plain string, generated bool, err error) {
	if pw == "" {
		p, err := newPassword()
		return p, true, err
	}
	if len(pw) < MinPassword || len(pw) > 128 || strings.ContainsAny(pw, "\r\n\x00") {
		return "", false, ErrPassword.With(MinPassword)
	}
	return pw, false, nil
}

func newDKIM() (private, public string, err error) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return "", "", err
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return "", "", err
	}
	pub, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		return "", "", err
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})), base64.StdEncoding.EncodeToString(pub), nil
}

// --- домены ---

// lockUser сериализует изменения пользователя (лимиты нельзя обойти гонкой).
func lockUser(tx *gorm.DB, userID int64) error {
	return tx.Raw("SELECT id FROM users WHERE id = ? FOR UPDATE", userID).Scan(new(int64)).Error
}

func (s *Service) domain(ctx context.Context, userID, id int64) (*Domain, error) {
	var d Domain
	err := s.db.WithContext(ctx).Where("id = ? AND user_id = ?", id, userID).First(&d).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &d, err
}

// Eligible — подсказки для формы: свои домены пользователя, подключённые к сайтам, для которых почты ещё нет. Добавить можно и любой другой домен.
func (s *Service) Eligible(ctx context.Context, userID int64) ([]string, error) {
	out := []string{}
	if s.cfg.Sites == nil {
		return out, nil
	}
	attached, err := s.cfg.Sites.DomainsByUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	var used []string
	if err := s.db.WithContext(ctx).Model(&Domain{}).Where("user_id = ?", userID).Pluck("domain", &used).Error; err != nil {
		return nil, err
	}
	taken := map[string]bool{}
	for _, d := range used {
		taken[d] = true
	}
	seen := map[string]bool{}
	for _, list := range attached {
		for _, d := range list {
			if d.Kind == "custom" && !s.isBase(d.Host) && !taken[d.Host] && !seen[d.Host] && domainRe.MatchString(d.Host) {
				seen[d.Host] = true
				out = append(out, d.Host)
			}
		}
	}
	sort.Strings(out)
	return out, nil
}

// isBase: домен самого хостинга (vladinc.ru и его поддомены) для почты не подходит.
func (s *Service) isBase(name string) bool {
	base := strings.ToLower(s.cfg.BaseDomain)
	return base != "" && (name == base || strings.HasSuffix(name, "."+base))
}

// provenBySite: домен подключён к сайту этого пользователя как свой — владение проверено при подключении.
func (s *Service) provenBySite(ctx context.Context, userID int64, name string) (bool, error) {
	if s.cfg.Sites == nil {
		return false, nil
	}
	attached, err := s.cfg.Sites.DomainsByUser(ctx, userID)
	if err != nil {
		return false, err
	}
	for _, list := range attached {
		for _, d := range list {
			if d.Kind == "custom" && d.Host == name {
				return true, nil
			}
		}
	}
	return false, nil
}

// provenWithoutTXT: владение уже подтверждено другим способом — подключением к сайту или подтверждённой зоной DNS у нас.
func (s *Service) provenWithoutTXT(ctx context.Context, userID int64, name string) (bool, error) {
	if ok, err := s.provenBySite(ctx, userID, name); err != nil || ok {
		return ok, err
	}
	if s.cfg.DNS != nil {
		return s.cfg.DNS.Owns(ctx, userID, name)
	}
	return false, nil
}

// EnableDomain включает почту для домена. Домен может быть любым (кроме доменов самого хостинга): если он подключён к сайту пользователя или у него
// есть подтверждённая зона DNS у нас, владение подтверждено сразу, иначе домен ждёт подтверждения TXT-записью и почту не обслуживает.
func (s *Service) EnableDomain(ctx context.Context, u auth.User, raw string) (*Domain, error) {
	name, err := normDomain(raw)
	if err != nil {
		return nil, err
	}
	if s.isBase(name) {
		return nil, ErrDomainInvalid
	}
	verified, err := s.provenWithoutTXT(ctx, u.ID, name)
	if err != nil {
		return nil, err
	}
	priv, pub, err := newDKIM()
	if err != nil {
		return nil, err
	}
	d := &Domain{UserID: u.ID, Domain: name, Enabled: true, Verified: verified, Token: domainproof.Token(), DKIMSelector: Selector, DKIMPrivate: priv, DKIMPublic: pub, CreatedAt: s.now()}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockUser(tx, u.ID); err != nil {
			return err
		}
		var n int64
		if err := tx.Model(&Domain{}).Where("user_id = ?", u.ID).Count(&n).Error; err != nil {
			return err
		}
		if n >= MaxDomains {
			return ErrDomainLimit.With(MaxDomains)
		}
		if err := tx.Create(d).Error; err != nil {
			if isUnique(err) {
				return ErrDomainTaken
			}
			return err
		}
		// Адрес postmaster обязателен по RFC: письма на него идут владельцу аккаунта.
		if _, perr := mail.ParseAddress(u.Email); perr == nil {
			return tx.Create(&Alias{DomainID: d.ID, LocalPart: "postmaster", Destinations: strings.ToLower(u.Email), CreatedAt: s.now()}).Error
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if !d.Verified {
		return d, nil // на сервер неподтверждённый домен не попадает
	}
	// Освободившееся имя могло числиться в списке на удаление с диска: свежий домен не должен потерять почту.
	_ = s.db.WithContext(ctx).Where("path = ? OR path LIKE ?", name, name+"/%").Delete(&purge{}).Error
	return d, s.syncAfter(ctx)
}

// Verify подтверждает владение доменом: он подключён к сайту, у него есть подтверждённая зона DNS у нас либо в его DNS найдена TXT-запись с кодом.
// Подтверждение отзывает чужие неподтверждённые заявки на тот же домен; если домен уже подтвердил другой пользователь — отказ.
func (s *Service) Verify(ctx context.Context, userID, id int64) (*Domain, error) {
	d, err := s.domain(ctx, userID, id)
	if err != nil {
		return nil, err
	}
	if d.Verified {
		return d, nil
	}
	proven, err := s.provenWithoutTXT(ctx, userID, d.Domain)
	if err != nil {
		return nil, err
	}
	if !proven {
		proven = domainproof.Check(ctx, s.cfg.Resolver.LookupTXT, d.Domain, d.Token)
	}
	if !proven {
		return nil, ErrNotVerified
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&Domain{}).Where("id = ?", d.ID).Update("verified", true).Error; err != nil {
			if isUnique(err) {
				return ErrDomainTaken
			}
			return err
		}
		return tx.Where("domain = ? AND user_id <> ? AND NOT verified", d.Domain, userID).Delete(&Domain{}).Error
	})
	if err != nil {
		return nil, err
	}
	d.Verified = true
	_ = s.db.WithContext(ctx).Where("path = ? OR path LIKE ?", d.Domain, d.Domain+"/%").Delete(&purge{}).Error
	return d, s.syncAfter(ctx)
}

// verifiedDomain — домен пользователя, владение которым подтверждено (ящики и алиасы заводятся только у таких).
func (s *Service) verifiedDomain(ctx context.Context, userID, id int64) (*Domain, error) {
	d, err := s.domain(ctx, userID, id)
	if err != nil {
		return nil, err
	}
	if !d.Verified {
		return nil, ErrUnverified
	}
	return d, nil
}

// SetDomainEnabled включает или выключает приём почты для домена (ящики и письма остаются).
func (s *Service) SetDomainEnabled(ctx context.Context, userID, id int64, enabled bool) (*Domain, error) {
	d, err := s.domain(ctx, userID, id)
	if err != nil {
		return nil, err
	}
	if err := s.db.WithContext(ctx).Model(d).Update("enabled", enabled).Error; err != nil {
		return nil, err
	}
	d.Enabled = enabled
	return d, s.syncAfter(ctx)
}

// DeleteDomain удаляет домен со всеми ящиками, алиасами и почтой на диске.
func (s *Service) DeleteDomain(ctx context.Context, userID, id int64) error {
	d, err := s.domain(ctx, userID, id)
	if err != nil {
		return err
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Delete(&Domain{}, d.ID).Error; err != nil { // ящики и алиасы удаляются каскадом
			return err
		}
		if !d.Verified {
			return nil // на диске у неподтверждённого домена ничего не было
		}
		return tx.Exec("INSERT INTO mail_purge (path) VALUES (?) ON CONFLICT DO NOTHING", d.Domain).Error
	})
	if err != nil {
		return err
	}
	if !d.Verified {
		return nil
	}
	return s.syncAfter(ctx)
}

// --- ящики ---

func (s *Service) count(tx *gorm.DB, userID int64, table string) (int64, error) {
	var n int64
	err := tx.Table(table).Joins("JOIN mail_domains ON mail_domains.id = "+table+".domain_id").Where("mail_domains.user_id = ?", userID).Count(&n).Error
	return n, err
}

// CreateMailbox заводит ящик. Пароль — свой (от 10 знаков) или случайный; вернётся, только если его сгенерировала панель.
func (s *Service) CreateMailbox(ctx context.Context, userID, domainID int64, local, password string, quotaMB int) (*Mailbox, string, error) {
	d, err := s.verifiedDomain(ctx, userID, domainID)
	if err != nil {
		return nil, "", err
	}
	local, err = normLocal(local, false)
	if err != nil {
		return nil, "", err
	}
	if quotaMB == 0 {
		quotaMB = DefaultQuotaMB
	}
	if quotaMB < MinQuotaMB || quotaMB > MaxQuotaMB {
		return nil, "", ErrQuota.With(MinQuotaMB, MaxQuotaMB)
	}
	plain, generated, err := passwordOrNew(password)
	if err != nil {
		return nil, "", err
	}
	hash, err := hashPassword(plain)
	if err != nil {
		return nil, "", err
	}
	box := &Mailbox{DomainID: d.ID, LocalPart: local, PasswordHash: hash, QuotaMB: quotaMB, Enabled: true, CreatedAt: s.now(), AutoreplyDays: 1, ForwardKeep: true}
	_ = box.AfterFind(nil)
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockUser(tx, userID); err != nil {
			return err
		}
		n, err := s.count(tx, userID, "mailboxes")
		if err != nil {
			return err
		}
		if n >= MaxMailboxes {
			return ErrBoxLimit.With(MaxMailboxes)
		}
		var clash int64
		if err := tx.Model(&Alias{}).Where("domain_id = ? AND local_part = ?", d.ID, local).Count(&clash).Error; err != nil {
			return err
		}
		if clash > 0 {
			return ErrLocalTaken
		}
		if err := tx.Create(box).Error; err != nil {
			if isUnique(err) {
				return ErrLocalTaken
			}
			return err
		}
		return nil
	})
	if err != nil {
		return nil, "", err
	}
	_ = s.db.WithContext(ctx).Where("path = ?", d.Domain+"/"+local).Delete(&purge{}).Error
	shown := ""
	if generated {
		shown = plain
	}
	return box, shown, s.syncAfter(ctx)
}

func isUnique(err error) bool {
	return err != nil && strings.Contains(err.Error(), "SQLSTATE 23505")
}

func (s *Service) mailbox(ctx context.Context, userID, id int64) (*Mailbox, *Domain, error) {
	var box Mailbox
	if err := s.db.WithContext(ctx).Where("id = ?", id).First(&box).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, ErrNotFound
		}
		return nil, nil, err
	}
	d, err := s.domain(ctx, userID, box.DomainID)
	if err != nil {
		return nil, nil, err
	}
	return &box, d, nil
}

// UpdateMailbox меняет квоту и/или включённость ящика.
func (s *Service) UpdateMailbox(ctx context.Context, userID, id int64, quotaMB *int, enabled *bool) (*Mailbox, error) {
	box, _, err := s.mailbox(ctx, userID, id)
	if err != nil {
		return nil, err
	}
	up := map[string]any{}
	if quotaMB != nil {
		if *quotaMB < MinQuotaMB || *quotaMB > MaxQuotaMB {
			return nil, ErrQuota.With(MinQuotaMB, MaxQuotaMB)
		}
		up["quota_mb"], box.QuotaMB = *quotaMB, *quotaMB
	}
	if enabled != nil {
		up["enabled"], box.Enabled = *enabled, *enabled
	}
	if len(up) > 0 {
		if err := s.db.WithContext(ctx).Model(&Mailbox{}).Where("id = ?", box.ID).Updates(up).Error; err != nil {
			return nil, err
		}
	}
	return box, s.syncAfter(ctx)
}

// Пределы правил ящика (те же проверяет mail-sync.py на сервере).
const (
	MaxSubject = 200
	MaxBody    = 2000
)

func cleanText(v string, limit int, multiline bool) (string, bool) {
	v = strings.ReplaceAll(v, "\r\n", "\n")
	if strings.TrimSpace(v) == "" || utf8.RuneCountInString(v) > limit || !utf8.ValidString(v) {
		return "", false
	}
	for _, r := range v {
		if r == '\n' && multiline {
			continue
		}
		if r < 32 || r == 127 {
			return "", false
		}
	}
	if multiline {
		v = strings.Trim(v, "\n")
	}
	return strings.TrimSpace(v), true
}

func validDay(v string) bool {
	if v == "" {
		return true
	}
	_, err := time.Parse("2006-01-02", v)
	return err == nil && len(v) == 10
}

// SetMailboxRules задаёт автоответчик и пересылку ящика. Выключенные правила очищаются; письма, которые пересылка не смогла отправить, не теряются:
// при пересылке с копией они остаются в ящике.
func (s *Service) SetMailboxRules(ctx context.Context, userID, id int64, ar AutoReply, fw Forward) (*Mailbox, error) {
	box, d, err := s.mailbox(ctx, userID, id)
	if err != nil {
		return nil, err
	}
	up := map[string]any{"autoreply_enabled": false, "autoreply_subject": "", "autoreply_body": "", "autoreply_from": "", "autoreply_to": "", "autoreply_days": 1}
	if ar.Enabled {
		subject, ok := cleanText(ar.Subject, MaxSubject, false)
		if !ok {
			return nil, ErrRulesText.With(MaxSubject, MaxBody)
		}
		body, ok := cleanText(ar.Body, MaxBody, true)
		if !ok {
			return nil, apperr.Validation("body", "mail_autoreply_text", "invalid autoreply text").With(MaxSubject, MaxBody)
		}
		if !validDay(ar.From) || !validDay(ar.To) || (ar.From != "" && ar.To != "" && ar.From > ar.To) {
			return nil, ErrRulesDates
		}
		if ar.Days == 0 {
			ar.Days = 1
		}
		if ar.Days < 1 || ar.Days > 30 {
			return nil, ErrRulesDays
		}
		up["autoreply_enabled"], up["autoreply_subject"], up["autoreply_body"] = true, subject, body
		up["autoreply_from"], up["autoreply_to"], up["autoreply_days"] = ar.From, ar.To, ar.Days
	}
	dests := []string{}
	if len(fw.To) > 0 {
		if dests, err = normDests(d.Domain, box.LocalPart, fw.To); err != nil {
			if errors.Is(err, ErrDest) {
				return nil, apperr.Validation("forward", "mail_dest", "invalid destination address")
			}
			return nil, err
		}
	}
	up["forward_to"], up["forward_keep"] = strings.Join(dests, "\n"), fw.KeepCopy
	if err := s.db.WithContext(ctx).Model(&Mailbox{}).Where("id = ?", box.ID).Updates(up).Error; err != nil {
		return nil, err
	}
	var out Mailbox
	if err := s.db.WithContext(ctx).First(&out, box.ID).Error; err != nil {
		return nil, err
	}
	return &out, s.syncAfter(ctx)
}

// SetMailboxPassword меняет пароль ящика: свой или случайный (возвращается только сгенерированный).
func (s *Service) SetMailboxPassword(ctx context.Context, userID, id int64, password string) (string, error) {
	box, _, err := s.mailbox(ctx, userID, id)
	if err != nil {
		return "", err
	}
	plain, generated, err := passwordOrNew(password)
	if err != nil {
		return "", err
	}
	hash, err := hashPassword(plain)
	if err != nil {
		return "", err
	}
	if err := s.db.WithContext(ctx).Model(&Mailbox{}).Where("id = ?", box.ID).Update("password_hash", hash).Error; err != nil {
		return "", err
	}
	shown := ""
	if generated {
		shown = plain
	}
	return shown, s.syncAfter(ctx)
}

// DeleteMailbox удаляет ящик вместе с письмами.
func (s *Service) DeleteMailbox(ctx context.Context, userID, id int64) error {
	box, d, err := s.mailbox(ctx, userID, id)
	if err != nil {
		return err
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Delete(&Mailbox{}, box.ID).Error; err != nil {
			return err
		}
		return tx.Exec("INSERT INTO mail_purge (path) VALUES (?) ON CONFLICT DO NOTHING", d.Domain+"/"+box.LocalPart).Error
	})
	if err != nil {
		return err
	}
	return s.syncAfter(ctx)
}

// --- алиасы ---

func normDests(domain string, local string, to []string) ([]string, error) {
	if len(to) < 1 || len(to) > MaxDest {
		return nil, ErrDest
	}
	seen := map[string]bool{}
	var out []string
	for _, raw := range to {
		raw = strings.ToLower(strings.TrimSpace(raw))
		a, err := mail.ParseAddress(raw)
		if err != nil || a.Address != raw || len(raw) > 254 || strings.ContainsAny(raw, ",;\"'\\ ") {
			return nil, ErrDest
		}
		lp, dom, _ := strings.Cut(raw, "@")
		if !localRe.MatchString(lp) || !domainRe.MatchString(dom) {
			return nil, ErrDest
		}
		if raw == local+"@"+domain {
			return nil, ErrDestLoop
		}
		if !seen[raw] {
			seen[raw] = true
			out = append(out, raw)
		}
	}
	return out, nil
}

// SetAlias создаёт или заменяет алиас домена. «*» — общий ящик домена.
func (s *Service) SetAlias(ctx context.Context, userID, domainID int64, local string, to []string) (*Alias, error) {
	d, err := s.verifiedDomain(ctx, userID, domainID)
	if err != nil {
		return nil, err
	}
	local, err = normLocal(local, true)
	if err != nil {
		return nil, err
	}
	dests, err := normDests(d.Domain, local, to)
	if err != nil {
		return nil, err
	}
	var out *Alias
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockUser(tx, userID); err != nil {
			return err
		}
		var box int64
		if err := tx.Model(&Mailbox{}).Where("domain_id = ? AND local_part = ?", d.ID, local).Count(&box).Error; err != nil {
			return err
		}
		if box > 0 {
			return ErrLocalTaken
		}
		var existing Alias
		err := tx.Where("domain_id = ? AND local_part = ?", d.ID, local).First(&existing).Error
		switch {
		case err == nil:
			existing.Destinations = strings.Join(dests, "\n")
			if err := tx.Model(&Alias{}).Where("id = ?", existing.ID).Update("destinations", existing.Destinations).Error; err != nil {
				return err
			}
			out = &existing
		case errors.Is(err, gorm.ErrRecordNotFound):
			n, err := s.count(tx, userID, "mail_aliases")
			if err != nil {
				return err
			}
			if n >= MaxAliases {
				return ErrAliasLimit.With(MaxAliases)
			}
			out = &Alias{DomainID: d.ID, LocalPart: local, Destinations: strings.Join(dests, "\n"), CreatedAt: s.now()}
			return tx.Create(out).Error
		default:
			return err
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	out.To = dests
	return out, s.syncAfter(ctx)
}

// DeleteAlias удаляет алиас.
func (s *Service) DeleteAlias(ctx context.Context, userID, id int64) error {
	var a Alias
	if err := s.db.WithContext(ctx).Where("id = ?", id).First(&a).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrNotFound
		}
		return err
	}
	if _, err := s.domain(ctx, userID, a.DomainID); err != nil {
		return err
	}
	if err := s.db.WithContext(ctx).Delete(&Alias{}, a.ID).Error; err != nil {
		return err
	}
	return s.syncAfter(ctx)
}

// --- состояние для сервера ---

type stateDKIM struct {
	Selector   string `json:"selector"`
	PrivateKey string `json:"private_key"`
}

type stateAutoReply struct {
	Enabled bool   `json:"enabled"`
	Subject string `json:"subject"`
	Body    string `json:"body"`
	From    string `json:"from"`
	To      string `json:"to"`
	Days    int    `json:"days"`
}

type stateForward struct {
	To       []string `json:"to"`
	KeepCopy bool     `json:"keep_copy"`
}

type stateBox struct {
	Local     string          `json:"local"`
	Hash      string          `json:"hash"`
	QuotaMB   int             `json:"quota_mb"`
	Enabled   bool            `json:"enabled"`
	AutoReply *stateAutoReply `json:"autoreply,omitempty"`
	Forward   *stateForward   `json:"forward,omitempty"`
}

type stateAlias struct {
	Local string   `json:"local"`
	To    []string `json:"to"`
}

type stateDomain struct {
	Name      string       `json:"name"`
	Enabled   bool         `json:"enabled"`
	DKIM      *stateDKIM   `json:"dkim"`
	Mailboxes []stateBox   `json:"mailboxes"`
	Aliases   []stateAlias `json:"aliases"`
}

type state struct {
	Version int           `json:"version"`
	Domains []stateDomain `json:"domains"`
	Purge   []string      `json:"purge"`
}

func (s *Service) buildState(ctx context.Context) (*state, []string, error) {
	var doms []Domain
	if err := s.db.WithContext(ctx).Where("verified").Order("id").Find(&doms).Error; err != nil {
		return nil, nil, err
	}
	st := &state{Version: 1, Domains: []stateDomain{}, Purge: []string{}}
	idx := map[int64]int{}
	for _, d := range doms {
		idx[d.ID] = len(st.Domains)
		st.Domains = append(st.Domains, stateDomain{Name: d.Domain, Enabled: d.Enabled, DKIM: &stateDKIM{Selector: d.DKIMSelector, PrivateKey: d.DKIMPrivate}, Mailboxes: []stateBox{}, Aliases: []stateAlias{}})
	}
	var boxes []Mailbox
	if err := s.db.WithContext(ctx).Order("id").Find(&boxes).Error; err != nil {
		return nil, nil, err
	}
	for _, b := range boxes {
		if i, ok := idx[b.DomainID]; ok {
			sb := stateBox{Local: b.LocalPart, Hash: b.PasswordHash, QuotaMB: b.QuotaMB, Enabled: b.Enabled}
			if b.AutoReply.Enabled {
				ar := b.AutoReply
				sb.AutoReply = &stateAutoReply{Enabled: true, Subject: ar.Subject, Body: ar.Body, From: ar.From, To: ar.To, Days: ar.Days}
			}
			if len(b.Forward.To) > 0 {
				sb.Forward = &stateForward{To: b.Forward.To, KeepCopy: b.Forward.KeepCopy}
			}
			st.Domains[i].Mailboxes = append(st.Domains[i].Mailboxes, sb)
		}
	}
	var aliases []Alias
	if err := s.db.WithContext(ctx).Order("id").Find(&aliases).Error; err != nil {
		return nil, nil, err
	}
	for _, a := range aliases {
		if i, ok := idx[a.DomainID]; ok {
			st.Domains[i].Aliases = append(st.Domains[i].Aliases, stateAlias{Local: a.LocalPart, To: strings.Split(a.Destinations, "\n")})
		}
	}
	var paths []string
	if err := s.db.WithContext(ctx).Model(&purge{}).Order("path").Pluck("path", &paths).Error; err != nil {
		return nil, nil, err
	}
	st.Purge = append(st.Purge, paths...)
	return st, paths, nil
}

// Sync пишет состояние на диск и просит исполнителя применить его. Вызовы сериализованы.
func (s *Service) Sync(ctx context.Context) error {
	s.syncMu.Lock()
	defer s.syncMu.Unlock()
	st, paths, err := s.buildState(ctx)
	if err != nil {
		return err
	}
	data, err := json.Marshal(st)
	if err != nil {
		return err
	}
	dir := filepath.Join(s.cfg.Dir, "mail")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".state-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o640); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), filepath.Join(dir, "state.json")); err != nil {
		return err
	}
	res, err := s.cfg.Applier.Do(ctx, runtimes.Request{Action: ActionMailSync}, 60*time.Second)
	if err != nil {
		log.Printf("почта: синхронизация: %v", err)
		return ErrHelperDown
	}
	if !res.OK {
		log.Printf("почта: синхронизация отклонена: %s: %s", res.Error, strings.TrimSpace(res.Output))
		return ErrSyncFailed.With(res.Error)
	}
	// Удаление с диска выполнено: список можно очистить (то, что добавили за время синхронизации, остаётся).
	if len(paths) > 0 {
		if err := s.db.WithContext(ctx).Where("path IN ?", paths).Delete(&purge{}).Error; err != nil {
			return err
		}
	}
	return nil
}

// syncAfter вызывается после изменения данных: при неудаче данные уже сохранены, синхронизацию повторит Watch.
func (s *Service) syncAfter(ctx context.Context) error {
	err := s.Sync(context.WithoutCancel(ctx))
	s.mu.Lock()
	s.dirty = err != nil
	s.mu.Unlock()
	return err
}

// Watch раз в every повторяет синхронизацию, если прошлая не удалась, и раз в час сверяет состояние сервера с базой.
func (s *Service) Watch(ctx context.Context, every time.Duration) {
	if !s.Enabled() {
		return
	}
	tick := time.NewTicker(every)
	defer tick.Stop()
	last := time.Time{}
	for {
		s.mu.Lock()
		dirty := s.dirty
		need := dirty || last.IsZero() || time.Since(last) > time.Hour
		s.mu.Unlock()
		// Когда почты нет вовсе, применять нечего: не занимаем исполнителя (и не держим блокировку, пока он недоступен).
		if need && !dirty && !s.hasWork(ctx) {
			need = false
			last = time.Now()
		}
		if need {
			if err := s.Sync(ctx); err != nil {
				if ctx.Err() == nil {
					log.Printf("почта: повтор синхронизации: %v", err)
				}
				s.mu.Lock()
				s.dirty = true
				s.mu.Unlock()
			} else {
				s.mu.Lock()
				s.dirty = false
				s.mu.Unlock()
				last = time.Now()
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

// hasWork: есть ли что применять на сервере — домены или ещё не выполненное удаление.
func (s *Service) hasWork(ctx context.Context) bool {
	var n int64
	if s.db.WithContext(ctx).Model(&Domain{}).Where("verified").Count(&n).Error != nil || n > 0 {
		return true
	}
	return s.db.WithContext(ctx).Model(&purge{}).Count(&n).Error != nil || n > 0
}

// usage читает размеры ящиков, которые записал исполнитель при последней синхронизации.
func (s *Service) usage() map[string]map[string]int64 {
	out := map[string]map[string]int64{}
	data, err := os.ReadFile(filepath.Join(s.cfg.Dir, "mail", "usage.json"))
	if err != nil {
		return out
	}
	_ = json.Unmarshal(data, &out)
	return out
}
