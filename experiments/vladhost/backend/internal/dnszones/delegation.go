package dnszones

import (
	"context"
	"sort"
	"strings"
	"time"
)

// Состояния делегирования домена на наши серверы имён.
const (
	DelegationOK      = "ok"      // у домена только наши серверы имён
	DelegationPartial = "partial" // указан только один из наших серверов (остальных нет), чужих нет
	DelegationMixed   = "mixed"   // наши серверы и чужие: ответы будут разными
	DelegationNone    = "none"    // наших серверов у домена нет
	DelegationUnknown = "unknown" // проверить не удалось
)

// Delegation — итог проверки: что сейчас у домена и что должно быть.
type Delegation struct {
	State    string   `json:"state"`
	Found    []string `json:"found"`
	Expected []string `json:"expected"`
}

// Delegated: домен обслуживается нашими серверами (только ими).
func (d Delegation) Delegated() bool { return d.State == DelegationOK || d.State == DelegationPartial }

// Check определяет по DNS, на чьи серверы имён указывает домен.
func (s *Service) Check(ctx context.Context, domain string) Delegation {
	c, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	found, err := s.cfg.Resolver.LookupNS(c, domain)
	res := Delegation{Found: []string{}, Expected: s.NS()}
	seen := map[string]bool{}
	for _, n := range found {
		n = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(n), "."))
		if n != "" && !seen[n] {
			seen[n] = true
			res.Found = append(res.Found, n)
		}
	}
	sort.Strings(res.Found)
	ours := map[string]bool{}
	for _, n := range s.cfg.NS {
		ours[n] = true
	}
	mine, foreign := 0, 0
	for _, n := range res.Found {
		if ours[n] {
			mine++
		} else {
			foreign++
		}
	}
	switch {
	case len(res.Found) == 0 && err != nil:
		res.State = DelegationUnknown
	case mine == len(s.cfg.NS) && foreign == 0:
		res.State = DelegationOK
	case mine > 0 && foreign == 0:
		res.State = DelegationPartial
	case mine > 0:
		res.State = DelegationMixed
	default:
		res.State = DelegationNone
	}
	return res
}

// Delegation проверяет делегирование зоны пользователя.
func (s *Service) Delegation(ctx context.Context, userID, zoneID int64) (*Delegation, error) {
	z, err := s.zone(ctx, userID, zoneID)
	if err != nil {
		return nil, err
	}
	d := s.Check(ctx, z.Domain)
	return &d, nil
}
