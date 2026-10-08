package dnszones

import (
	"context"
	"errors"

	"vladhost/internal/mailhost"
)

// mailAdapter связывает почту с собственным DNS: пакет mailhost не знает про dnszones.
type mailAdapter struct{ svc *Service }

// ForMail возвращает то, что почта использует для автоматической настройки записей домена.
func (s *Service) ForMail() mailhost.DNSHosting { return mailAdapter{s} }

func (a mailAdapter) Status(ctx context.Context, userID int64, domain string) (mailhost.DNSStatus, error) {
	st, err := a.svc.Status(ctx, userID, domain)
	return mailhost.DNSStatus{Zone: st.Zone, Delegated: st.Delegated, State: st.Delegation.State, Found: st.Delegation.Found, Expected: st.Delegation.Expected}, err
}

func (a mailAdapter) Owns(ctx context.Context, userID int64, domain string) (bool, error) {
	var n int64
	err := a.svc.db.WithContext(ctx).Model(&Zone{}).Where("user_id = ? AND domain = ? AND verified", userID, domain).Count(&n).Error
	return n > 0, err
}

func (a mailAdapter) ApplyMail(ctx context.Context, userID int64, domain string, recs []mailhost.DNSMailRecord) error {
	out := make([]MailRecord, 0, len(recs))
	for _, r := range recs {
		out = append(out, MailRecord{Kind: r.Kind, Type: r.Type, Name: r.Name, Value: r.Value, Priority: r.Priority})
	}
	err := a.svc.ApplyMail(ctx, userID, domain, out)
	if errors.Is(err, ErrNotDelegated) {
		return mailhost.ErrNotDelegated
	}
	return err
}
