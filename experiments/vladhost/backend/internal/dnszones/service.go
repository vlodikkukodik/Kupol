package dnszones

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"

	"vladhost/internal/apperr"
	"vladhost/internal/domainproof"
	"vladhost/internal/runtimes"
	"vladhost/internal/sites"
)

// ActionDNSSync — просьба исполнителю собрать зоны из dns/state.json и перечитать BIND.
const ActionDNSSync = "dns-sync"

// Пределы.
const (
	MaxZones   = 5
	MaxRecords = 100
)

var (
	ErrNotFound      = apperr.New(http.StatusNotFound, "dns_not_found", "dns object not found")
	ErrZoneInvalid   = apperr.Validation("domain", "dns_domain", "invalid domain")
	ErrZoneBase      = apperr.Validation("domain", "dns_domain_base", "domain of the hosting itself cannot be added")
	ErrNotVerified   = apperr.New(http.StatusConflict, "dns_not_verified", "domain ownership is not confirmed yet")
	ErrZoneTaken     = apperr.New(http.StatusConflict, "dns_zone_taken", "domain already has a zone").OnField("domain")
	ErrZoneLimit     = apperr.New(http.StatusForbidden, "dns_zone_limit", "dns zone limit reached")
	ErrRecordLimit   = apperr.New(http.StatusForbidden, "dns_record_limit", "dns record limit reached")
	ErrType          = apperr.Validation("type", "dns_type", "unsupported record type")
	ErrName          = apperr.Validation("name", "dns_name", "invalid record name")
	ErrValue         = apperr.Validation("value", "dns_value", "invalid record value")
	ErrTTL           = apperr.Validation("ttl", "dns_ttl", "invalid ttl")
	ErrPriority      = apperr.Validation("priority", "dns_priority", "invalid priority")
	ErrCNAMEApex     = apperr.Validation("name", "dns_cname_apex", "cname is not allowed at the domain apex")
	ErrCNAMEConflict = apperr.New(http.StatusConflict, "dns_cname_conflict", "a name with a cname cannot have other records").OnField("name")
	ErrDuplicate     = apperr.New(http.StatusConflict, "dns_record_duplicate", "the same record already exists").OnField("value")
	ErrNotDelegated  = apperr.New(http.StatusConflict, "dns_not_delegated", "domain is not delegated to our name servers")
	ErrSyncFailed    = apperr.New(http.StatusBadGateway, "dns_sync_failed", "dns server did not apply the change yet")
	ErrHelperDown    = apperr.New(http.StatusServiceUnavailable, "dns_helper_down", "dns helper is not available")
)

// SiteDomains говорит, какие свои домены подключены к сайтам пользователя (реализует *sites.Service).
type SiteDomains interface {
	DomainsByUser(ctx context.Context, userID int64) (map[int64][]sites.Domain, error)
}

// Resolver — DNS-запросы для проверки делегирования (подменяется в тестах).
type Resolver interface {
	LookupNS(ctx context.Context, name string) ([]string, error)  // имена серверов без точки в конце, в нижнем регистре
	LookupTXT(ctx context.Context, name string) ([]string, error) // для подтверждения владения доменом
}

type netResolver struct{}

func (netResolver) LookupTXT(ctx context.Context, name string) ([]string, error) {
	return net.DefaultResolver.LookupTXT(ctx, name)
}

func (netResolver) LookupNS(ctx context.Context, name string) ([]string, error) {
	ns, err := net.DefaultResolver.LookupNS(ctx, name)
	out := make([]string, 0, len(ns))
	for _, n := range ns {
		out = append(out, strings.ToLower(strings.TrimSuffix(n.Host, ".")))
	}
	// «Нет такого имени» и «нет NS-записей» — это ответ (у домена наших серверов нет), а не сбой проверки.
	var de *net.DNSError
	if errors.As(err, &de) && de.IsNotFound {
		return out, nil
	}
	return out, err
}

// Config — настройки.
type Config struct {
	NS         []string // наши серверы имён: ns.vladinc.ru, ns2.vladinc.ru (адреса у них один)
	Hostmaster string   // адрес для SOA в виде домена: hostmaster.vladinc.ru
	ServerIPs  []string // IP сервера: на него по умолчанию указывают A-записи новой зоны
	BaseDomain string   // vladinc.ru: свои зоны под ним не заводятся
	Dir        string   // папка обмена (dns/state.json пишет панель)
	Applier    runtimes.Applier
	Sites      SiteDomains
	Resolver   Resolver
}

// Service — DNS.
type Service struct {
	db  *gorm.DB
	cfg Config
	now func() time.Time

	syncMu sync.Mutex
	mu     sync.Mutex
	dirty  bool
}

// New создаёт службу.
func New(db *gorm.DB, cfg Config) *Service {
	if cfg.Resolver == nil {
		cfg.Resolver = netResolver{}
	}
	for i := range cfg.NS {
		cfg.NS[i] = strings.ToLower(strings.TrimSuffix(cfg.NS[i], "."))
	}
	return &Service{db: db, cfg: cfg, now: time.Now}
}

// Enabled: DNS включён, если известны серверы имён и папка исполнителя.
func (s *Service) Enabled() bool {
	return s != nil && len(s.cfg.NS) > 0 && s.cfg.Dir != "" && s.cfg.Applier != nil
}

// NS возвращает наши серверы имён (для подсказок в интерфейсе).
func (s *Service) NS() []string { return append([]string(nil), s.cfg.NS...) }

func normDomain(d string) (string, error) {
	d = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(d), "."))
	if !validDomain(d) {
		return "", ErrZoneInvalid
	}
	return d, nil
}

func (s *Service) zone(ctx context.Context, userID, id int64) (*Zone, error) {
	var z Zone
	if err := s.db.WithContext(ctx).Where("id = ? AND user_id = ?", id, userID).Take(&z).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &z, nil
}

// isBase: домен самого хостинга (vladinc.ru и его поддомены) добавлять нельзя: им управляет оператор.
func (s *Service) isBase(name string) bool {
	base := strings.ToLower(s.cfg.BaseDomain)
	return base != "" && (name == base || strings.HasSuffix(name, "."+base))
}

// attachedToSite: домен подключён к сайту этого пользователя как свой — владение уже подтверждено при подключении (нужна A-запись на наш сервер).
func (s *Service) attachedToSite(ctx context.Context, userID int64, name string) (bool, error) {
	if s.cfg.Sites == nil || s.isBase(name) {
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

// Eligible — подсказки для формы: свои домены, подключённые к сайтам, для которых у пользователя ещё нет зоны. Добавить можно и любой другой домен.
func (s *Service) Eligible(ctx context.Context, userID int64) ([]string, error) {
	out := []string{}
	if s.cfg.Sites == nil {
		return out, nil
	}
	attached, err := s.cfg.Sites.DomainsByUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	var mine []string
	if err := s.db.WithContext(ctx).Model(&Zone{}).Where("user_id = ?", userID).Pluck("domain", &mine).Error; err != nil {
		return nil, err
	}
	have := map[string]bool{}
	for _, m := range mine {
		have[m] = true
	}
	seen := map[string]bool{}
	for _, list := range attached {
		for _, d := range list {
			if d.Kind == "custom" && !s.isBase(d.Host) && !have[d.Host] && !seen[d.Host] && validDomain(d.Host) {
				seen[d.Host] = true
				out = append(out, d.Host)
			}
		}
	}
	sort.Strings(out)
	return out, nil
}

// EnableZone добавляет домен в DNS с записями по умолчанию (адрес сервера для домена и www). Домен может быть любым: если он подключён к сайту
// пользователя, владение подтверждено сразу, иначе зона ждёт подтверждения TXT-записью и на сервере не обслуживается.
func (s *Service) EnableZone(ctx context.Context, userID int64, raw string) (*Zone, error) {
	name, err := normDomain(raw)
	if err != nil {
		return nil, err
	}
	if s.isBase(name) {
		return nil, ErrZoneBase
	}
	attached, err := s.attachedToSite(ctx, userID, name)
	if err != nil {
		return nil, err
	}
	z := &Zone{UserID: userID, Domain: name, Serial: s.now().Unix(), Verified: attached, Token: domainproof.Token(), CreatedAt: s.now()}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(?)", userID+7_000_000).Error; err != nil {
			return err
		}
		var n int64
		if err := tx.Model(&Zone{}).Where("user_id = ?", userID).Count(&n).Error; err != nil {
			return err
		}
		if n >= MaxZones {
			return ErrZoneLimit.With(MaxZones)
		}
		if err := tx.Create(z).Error; err != nil {
			if strings.Contains(err.Error(), "SQLSTATE 23505") {
				return ErrZoneTaken
			}
			return err
		}
		for _, r := range s.defaultRecords(name) {
			r.ZoneID, r.CreatedAt = z.ID, s.now()
			if err := tx.Create(&r).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if !z.Verified {
		return z, nil // на сервер зона не попадает, пока владение не подтверждено
	}
	return z, s.syncAfter(ctx)
}

// Verify подтверждает владение доменом: домен подключён к сайту пользователя либо в его DNS найдена TXT-запись с кодом зоны. После подтверждения зона
// начинает обслуживаться, а чужие неподтверждённые заявки на тот же домен отзываются. Если домен уже подтвердил кто-то другой, отказ.
func (s *Service) Verify(ctx context.Context, userID, id int64) (*Zone, error) {
	z, err := s.zone(ctx, userID, id)
	if err != nil {
		return nil, err
	}
	if z.Verified {
		return z, nil
	}
	proven, err := s.attachedToSite(ctx, userID, z.Domain)
	if err != nil {
		return nil, err
	}
	if !proven {
		proven = domainproof.Check(ctx, s.cfg.Resolver.LookupTXT, z.Domain, z.Token)
	}
	if !proven {
		return nil, ErrNotVerified
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&Zone{}).Where("id = ?", z.ID).Updates(map[string]any{"verified": true, "serial": s.now().Unix()}).Error; err != nil {
			if strings.Contains(err.Error(), "SQLSTATE 23505") {
				return ErrZoneTaken
			}
			return err
		}
		return tx.Where("domain = ? AND user_id <> ? AND NOT verified", z.Domain, userID).Delete(&Zone{}).Error
	})
	if err != nil {
		return nil, err
	}
	z.Verified = true
	return z, s.syncAfter(ctx)
}

func (s *Service) defaultRecords(zone string) []Record {
	var out []Record
	for _, raw := range s.cfg.ServerIPs {
		ip, err := netip.ParseAddr(raw)
		if err != nil {
			continue
		}
		typ := "A"
		if ip.Is6() {
			typ = "AAAA"
		}
		out = append(out, Record{Name: "@", Type: typ, Value: ip.String(), TTL: 300})
	}
	if len(out) > 0 {
		out = append(out, Record{Name: "www", Type: "CNAME", Value: zone, TTL: 300})
	}
	return out
}

// DeleteZone удаляет зону со всеми записями.
func (s *Service) DeleteZone(ctx context.Context, userID, id int64) error {
	z, err := s.zone(ctx, userID, id)
	if err != nil {
		return err
	}
	if err := s.db.WithContext(ctx).Delete(&Zone{}, z.ID).Error; err != nil {
		return err
	}
	if !z.Verified {
		return nil // неподтверждённая зона на сервере не лежала
	}
	return s.syncAfter(ctx)
}

// ZoneView — зона с записями.
type ZoneView struct {
	Zone
	Records []Record `json:"records"`
	// Для неподтверждённой зоны: какую TXT-запись создать у текущего DNS-провайдера.
	VerifyName  string `json:"verify_name,omitempty"`
	VerifyValue string `json:"verify_value,omitempty"`
}

// List возвращает зоны пользователя с записями.
func (s *Service) List(ctx context.Context, userID int64) ([]ZoneView, error) {
	var zones []Zone
	if err := s.db.WithContext(ctx).Where("user_id = ?", userID).Order("id").Find(&zones).Error; err != nil {
		return nil, err
	}
	out := make([]ZoneView, 0, len(zones))
	if len(zones) == 0 {
		return out, nil
	}
	ids := make([]int64, len(zones))
	for i, z := range zones {
		ids[i] = z.ID
	}
	var recs []Record
	if err := s.db.WithContext(ctx).Where("zone_id IN ?", ids).Order("id").Find(&recs).Error; err != nil {
		return nil, err
	}
	for _, z := range zones {
		v := ZoneView{Zone: z, Records: []Record{}}
		if !z.Verified {
			v.VerifyName, v.VerifyValue = domainproof.RecordName(z.Domain), domainproof.RecordValue(z.Token)
		}
		for _, r := range recs {
			if r.ZoneID == z.ID {
				v.Records = append(v.Records, r)
			}
		}
		out = append(out, v)
	}
	return out, nil
}

// Info — сведения для интерфейса.
type Info struct {
	NS         []string `json:"ns"`
	ServerIPs  []string `json:"server_ips"`
	MaxZones   int      `json:"max_zones"`
	MaxRecords int      `json:"max_records"`
	Types      []string `json:"types"`
	TTLs       []int    `json:"ttls"`
	Eligible   []string `json:"eligible_domains"`
}

// Info возвращает сведения для пользователя.
func (s *Service) Info(ctx context.Context, userID int64) (*Info, error) {
	el, err := s.Eligible(ctx, userID)
	if err != nil {
		return nil, err
	}
	return &Info{NS: s.NS(), ServerIPs: append([]string{}, s.cfg.ServerIPs...), MaxZones: MaxZones, MaxRecords: MaxRecords, Types: Types, TTLs: TTLs, Eligible: el}, nil
}

// bump поднимает серийный номер зоны: он обязан расти при каждом изменении.
func (s *Service) bump(tx *gorm.DB, zoneID int64) error {
	return tx.Exec("UPDATE dns_zones SET serial = GREATEST(serial + 1, ?) WHERE id = ?", s.now().Unix(), zoneID).Error
}

// conflicts проверяет правила набора записей зоны при добавлении или замене записи (skipID — заменяемая запись).
func conflicts(existing []Record, in RecordInput, skipID int64) error {
	for _, r := range existing {
		if r.ID == skipID || r.Name != in.Name {
			continue
		}
		if in.Type == "CNAME" || r.Type == "CNAME" {
			return ErrCNAMEConflict
		}
		if r.Type == in.Type && r.Value == in.Value && r.Priority == in.Priority {
			return ErrDuplicate
		}
	}
	return nil
}

// SaveRecord создаёт запись (id == 0) или заменяет существующую.
func (s *Service) SaveRecord(ctx context.Context, userID, zoneID, id int64, in RecordInput) (*Record, error) {
	z, err := s.zone(ctx, userID, zoneID)
	if err != nil {
		return nil, err
	}
	norm, err := normalize(in, z.Domain)
	if err != nil {
		return nil, err
	}
	var out Record
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(?)", z.ID+8_000_000).Error; err != nil {
			return err
		}
		var existing []Record
		if err := tx.Where("zone_id = ?", z.ID).Find(&existing).Error; err != nil {
			return err
		}
		if id == 0 && len(existing) >= MaxRecords {
			return ErrRecordLimit.With(MaxRecords)
		}
		if id != 0 {
			found := false
			for _, r := range existing {
				found = found || r.ID == id
			}
			if !found {
				return ErrNotFound
			}
		}
		if err := conflicts(existing, norm, id); err != nil {
			return err
		}
		rec := Record{ID: id, ZoneID: z.ID, Name: norm.Name, Type: norm.Type, Value: norm.Value, Priority: norm.Priority, TTL: norm.TTL, CreatedAt: s.now()}
		if id == 0 {
			if err := tx.Create(&rec).Error; err != nil {
				return err
			}
		} else if err := tx.Model(&Record{}).Where("id = ?", id).Updates(map[string]any{"name": rec.Name, "type": rec.Type, "value": rec.Value, "priority": rec.Priority, "ttl": rec.TTL, "managed": ""}).Error; err != nil {
			return err
		}
		out = rec
		return s.bump(tx, z.ID)
	})
	if err != nil {
		return nil, err
	}
	if id != 0 {
		if err := s.db.WithContext(ctx).First(&out, id).Error; err != nil {
			return nil, err
		}
	}
	return &out, s.syncAfter(ctx)
}

// ZoneOfRecord возвращает зону записи, если она принадлежит пользователю.
func (s *Service) ZoneOfRecord(ctx context.Context, userID, id int64) (int64, error) {
	var r Record
	if err := s.db.WithContext(ctx).Take(&r, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return 0, ErrNotFound
		}
		return 0, err
	}
	if _, err := s.zone(ctx, userID, r.ZoneID); err != nil {
		return 0, err
	}
	return r.ZoneID, nil
}

// DeleteRecord удаляет запись.
func (s *Service) DeleteRecord(ctx context.Context, userID, id int64) error {
	var r Record
	if err := s.db.WithContext(ctx).Take(&r, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrNotFound
		}
		return err
	}
	if _, err := s.zone(ctx, userID, r.ZoneID); err != nil {
		return err
	}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Delete(&Record{}, r.ID).Error; err != nil {
			return err
		}
		return s.bump(tx, r.ZoneID)
	})
	if err != nil {
		return err
	}
	return s.syncAfter(ctx)
}

// --- синхронизация с сервером ---

type stateRecord struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Value    string `json:"value"`
	Priority int    `json:"priority,omitempty"`
	TTL      int    `json:"ttl"`
}

type stateZone struct {
	Name    string        `json:"name"`
	Serial  int64         `json:"serial"`
	Records []stateRecord `json:"records"`
}

type state struct {
	Version     int         `json:"version"`
	Nameservers []string    `json:"nameservers"`
	Hostmaster  string      `json:"hostmaster"`
	Zones       []stateZone `json:"zones"`
}

func (s *Service) buildState(ctx context.Context) (*state, error) {
	st := &state{Version: 1, Nameservers: s.NS(), Hostmaster: strings.ToLower(s.cfg.Hostmaster), Zones: []stateZone{}}
	var zones []Zone
	if err := s.db.WithContext(ctx).Where("verified").Order("id").Find(&zones).Error; err != nil {
		return nil, err
	}
	idx := map[int64]int{}
	for _, z := range zones {
		idx[z.ID] = len(st.Zones)
		st.Zones = append(st.Zones, stateZone{Name: z.Domain, Serial: z.Serial, Records: []stateRecord{}})
	}
	var recs []Record
	if err := s.db.WithContext(ctx).Order("id").Find(&recs).Error; err != nil {
		return nil, err
	}
	for _, r := range recs {
		if i, ok := idx[r.ZoneID]; ok {
			st.Zones[i].Records = append(st.Zones[i].Records, stateRecord{Name: r.Name, Type: r.Type, Value: r.Value, Priority: r.Priority, TTL: r.TTL})
		}
	}
	return st, nil
}

// Sync пишет состояние на диск и просит исполнителя применить его. Вызовы сериализованы.
func (s *Service) Sync(ctx context.Context) error {
	s.syncMu.Lock()
	defer s.syncMu.Unlock()
	st, err := s.buildState(ctx)
	if err != nil {
		return err
	}
	data, err := json.Marshal(st)
	if err != nil {
		return err
	}
	dir := filepath.Join(s.cfg.Dir, "dns")
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
	res, err := s.cfg.Applier.Do(ctx, runtimes.Request{Action: ActionDNSSync}, 60*time.Second)
	if err != nil {
		log.Printf("dns: синхронизация: %v", err)
		return ErrHelperDown
	}
	if !res.OK {
		log.Printf("dns: синхронизация отклонена: %s: %s", res.Error, strings.TrimSpace(res.Output))
		return ErrSyncFailed.With(res.Error)
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

// hasWork: есть ли зоны, которые нужно применять на сервере.
func (s *Service) hasWork(ctx context.Context) bool {
	var n int64
	return s.db.WithContext(ctx).Model(&Zone{}).Count(&n).Error != nil || n > 0
}

// Watch раз в every повторяет синхронизацию, если прошлая не удалась, и раз в час сверяет сервер с базой.
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
		s.mu.Unlock()
		need := dirty || last.IsZero() || time.Since(last) > time.Hour
		if need && !dirty && !s.hasWork(ctx) {
			need, last = false, time.Now()
		}
		if need {
			if err := s.Sync(ctx); err != nil {
				if ctx.Err() == nil {
					log.Printf("dns: повтор синхронизации: %v", err)
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
