package mailhost

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"vladhost/internal/apperr"
)

// ErrNotDelegated — домен не обслуживается нашими серверами имён, записи поставить нельзя.
var (
	ErrNotDelegated = apperr.New(http.StatusConflict, "mail_dns_not_delegated", "domain is not delegated to our name servers")
	ErrAutoDisabled = apperr.New(http.StatusNotFound, "mail_dns_auto_disabled", "automatic dns setup is not available")
)

// DNSHosting — наш собственный DNS (пакет dnszones), если он включён: mailhost не зависит от него напрямую.
type DNSHosting interface {
	Status(ctx context.Context, userID int64, domain string) (DNSStatus, error)
	ApplyMail(ctx context.Context, userID int64, domain string, recs []DNSMailRecord) error
	// Owns: у пользователя есть подтверждённая зона этого домена у нас — владение домеником уже доказано.
	Owns(ctx context.Context, userID int64, domain string) (bool, error)
}

// DNSStatus — есть ли у домена зона у нас и указывает ли он на наши серверы имён.
type DNSStatus struct {
	Zone      bool
	Delegated bool
	State     string
	Found     []string
	Expected  []string
}

// DNSMailRecord — запись, которую нужно поставить в зону.
type DNSMailRecord struct {
	Kind     string
	Type     string
	Name     string
	Value    string
	Priority int
}

// DNSAuto — что интерфейс показывает про автоматическую настройку: кнопка доступна, только если домен уже на наших серверах имён.
type DNSAuto struct {
	Available bool     `json:"available"` // наш DNS на этом сервере включён
	Zone      bool     `json:"zone"`      // у домена есть зона в панели
	Delegated bool     `json:"delegated"` // домен указывает на наши серверы имён: можно настроить кнопкой
	State     string   `json:"state"`     // ok, partial, mixed, none, unknown
	Found     []string `json:"found"`
	Expected  []string `json:"expected"`
}

// DNSResult — записи домена с их состоянием и возможность автоматической настройки.
type DNSResult struct {
	Records []Record `json:"records"`
	Auto    DNSAuto  `json:"auto"`
}

func (s *Service) autoState(ctx context.Context, userID int64, domain string) DNSAuto {
	a := DNSAuto{State: "none", Found: []string{}, Expected: []string{}}
	if s.cfg.DNS == nil {
		return a
	}
	a.Available = true
	st, err := s.cfg.DNS.Status(ctx, userID, domain)
	if err != nil {
		a.State = "unknown"
		return a
	}
	a.Zone, a.Delegated, a.State = st.Zone, st.Delegated, st.State
	if st.Found != nil {
		a.Found = st.Found
	}
	if st.Expected != nil {
		a.Expected = st.Expected
	}
	return a
}

// DNSInfo проверяет записи домена по DNS и сообщает, можно ли поставить их автоматически.
func (s *Service) DNSInfo(ctx context.Context, userID, id int64) (*DNSResult, error) {
	d, err := s.domain(ctx, userID, id)
	if err != nil {
		return nil, err
	}
	return &DNSResult{Records: s.Records(ctx, d), Auto: s.autoState(ctx, userID, d.Domain)}, nil
}

// AutoConfigure ставит записи почты (MX, SPF, DKIM, DMARC) в зону домена на наших серверах имён.
func (s *Service) AutoConfigure(ctx context.Context, userID, id int64) error {
	d, err := s.verifiedDomain(ctx, userID, id)
	if err != nil {
		return err
	}
	if s.cfg.DNS == nil {
		return ErrAutoDisabled
	}
	st, err := s.cfg.DNS.Status(ctx, userID, d.Domain)
	if err != nil {
		return err
	}
	if !st.Zone || !st.Delegated {
		return ErrNotDelegated
	}
	var recs []DNSMailRecord
	for _, r := range s.wanted(d) {
		m := DNSMailRecord{Kind: r.Kind, Type: r.Type, Name: r.Name, Value: r.Value}
		if r.Type == "MX" { // «10 mail.vladinc.ru.» → приоритет и имя
			prio, host, _ := strings.Cut(r.Value, " ")
			m.Priority, _ = strconv.Atoi(prio)
			m.Value = strings.TrimSuffix(host, ".")
		}
		recs = append(recs, m)
	}
	return s.cfg.DNS.ApplyMail(ctx, userID, d.Domain, recs)
}
