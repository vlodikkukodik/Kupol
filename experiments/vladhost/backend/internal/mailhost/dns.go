package mailhost

import (
	"context"
	"net"
	"strings"
	"time"
)

// Resolver — DNS-запросы для проверки записей домена (подменяется в тестах).
type Resolver interface {
	LookupMX(ctx context.Context, name string) ([]string, error) // имена почтовых серверов без точки в конце
	LookupTXT(ctx context.Context, name string) ([]string, error)
}

type netResolver struct{}

func (netResolver) LookupMX(ctx context.Context, name string) ([]string, error) {
	mx, err := net.DefaultResolver.LookupMX(ctx, name)
	var out []string
	for _, m := range mx {
		out = append(out, strings.ToLower(strings.TrimSuffix(m.Host, ".")))
	}
	return out, err
}

func (netResolver) LookupTXT(ctx context.Context, name string) ([]string, error) {
	return net.DefaultResolver.LookupTXT(ctx, name)
}

// Записи DNS, которые нужно создать у регистратора, и их состояние.
const (
	StateOK       = "ok"       // запись есть и подходит
	StateMissing  = "missing"  // записи нет
	StateMismatch = "mismatch" // запись есть, но значение другое
)

// Record — запись DNS.
type Record struct {
	Kind   string `json:"kind"` // mx, spf, dkim, dmarc
	Type   string `json:"type"` // MX или TXT
	Name   string `json:"name"`
	Value  string `json:"value"`
	State  string `json:"state"`
	Detail string `json:"detail,omitempty"` // что найдено (для несовпадения)
}

func joinTXT(parts []string) string { return strings.Join(parts, "") }

// wanted — записи, которые нужны домену (без проверки по DNS).
func (s *Service) wanted(d *Domain) []Record {
	host := strings.ToLower(s.cfg.Host)
	spf := "v=spf1 mx"
	if s.cfg.ServerIP != "" {
		spf += " ip4:" + s.cfg.ServerIP
	}
	spf += " ~all"
	return []Record{
		{Kind: "mx", Type: "MX", Name: d.Domain, Value: "10 " + host + "."},
		{Kind: "spf", Type: "TXT", Name: d.Domain, Value: spf},
		{Kind: "dkim", Type: "TXT", Name: d.DKIMSelector + "._domainkey." + d.Domain, Value: "v=DKIM1; k=rsa; p=" + d.DKIMPublic},
		{Kind: "dmarc", Type: "TXT", Name: "_dmarc." + d.Domain, Value: "v=DMARC1; p=none; rua=mailto:postmaster@" + d.Domain},
	}
}

// Records собирает нужные записи и проверяет их по DNS. Проверка не должна затягивать ответ: у каждого запроса своё короткое ожидание.
func (s *Service) Records(ctx context.Context, d *Domain) []Record {
	host := strings.ToLower(s.cfg.Host)
	ip := s.cfg.ServerIP
	recs := s.wanted(d)
	q := func() (context.Context, context.CancelFunc) { return context.WithTimeout(ctx, 5*time.Second) }
	for i := range recs {
		r := &recs[i]
		c, cancel := q()
		switch r.Kind {
		case "mx":
			mx, _ := s.cfg.Resolver.LookupMX(c, r.Name)
			r.State, r.Detail = StateMissing, ""
			if len(mx) > 0 {
				r.State, r.Detail = StateMismatch, strings.Join(mx, ", ")
				for _, m := range mx {
					if m == host {
						r.State, r.Detail = StateOK, ""
					}
				}
			}
		default:
			txt, _ := s.cfg.Resolver.LookupTXT(c, r.Name)
			r.State = StateMissing
			for _, raw := range txt {
				v := joinTXT([]string{raw})
				switch r.Kind {
				case "spf":
					if strings.HasPrefix(strings.ToLower(v), "v=spf1") {
						r.State, r.Detail = StateMismatch, v
						lv := strings.ToLower(v)
						if (ip != "" && strings.Contains(lv, "ip4:"+ip)) || strings.Contains(lv, "a:"+host) || strings.Contains(lv, "include:"+host) {
							r.State, r.Detail = StateOK, ""
						}
					}
				case "dkim":
					r.State, r.Detail = StateMismatch, v
					if strings.Contains(strings.ReplaceAll(v, " ", ""), "p="+d.DKIMPublic) {
						r.State, r.Detail = StateOK, ""
					}
				case "dmarc":
					if strings.HasPrefix(strings.ToLower(v), "v=dmarc1") {
						r.State, r.Detail = StateOK, ""
					}
				}
				if r.State == StateOK {
					break
				}
			}
		}
		cancel()
	}
	return recs
}
