package dnszones

import (
	"context"

	"gorm.io/gorm"
)

// MailRecord — запись, которую почта просит поставить в зону домена (имя полное: «vh1._domainkey.example.com»).
type MailRecord struct {
	Kind     string // mx, spf, dkim, dmarc
	Type     string // MX или TXT
	Name     string
	Value    string
	Priority int
}

// ManagedMail — метка записей, созданных кнопкой автоматической настройки почты.
const ManagedMail = "mail"

// ZoneStatus — есть ли у домена наша зона и делегирован ли он на наши серверы имён.
type ZoneStatus struct {
	Zone       bool       `json:"zone"`
	Delegation Delegation `json:"delegation"`
	Delegated  bool       `json:"delegated"`
}

// Status: зона домена у пользователя и состояние делегирования. Домена без зоны нет смысла проверять в DNS.
func (s *Service) Status(ctx context.Context, userID int64, domain string) (ZoneStatus, error) {
	var n int64
	if err := s.db.WithContext(ctx).Model(&Zone{}).Where("user_id = ? AND domain = ? AND verified", userID, domain).Count(&n).Error; err != nil {
		return ZoneStatus{}, err
	}
	st := ZoneStatus{Zone: n > 0, Delegation: Delegation{State: DelegationNone, Found: []string{}, Expected: s.NS()}}
	if st.Zone {
		st.Delegation = s.Check(ctx, domain)
		st.Delegated = st.Delegation.Delegated()
	}
	return st, nil
}

// ApplyMail ставит записи почты в зону домена: прежние записи с тем же назначением (MX домена, SPF, ключ DKIM, DMARC) заменяются.
// Зона должна принадлежать пользователю и быть делегирована на наши серверы имён.
func (s *Service) ApplyMail(ctx context.Context, userID int64, domain string, recs []MailRecord) error {
	var z Zone
	if err := s.db.WithContext(ctx).Where("user_id = ? AND domain = ? AND verified", userID, domain).Take(&z).Error; err != nil {
		return ErrNotFound
	}
	if !s.Check(ctx, domain).Delegated() {
		return ErrNotDelegated
	}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(?)", z.ID+8_000_000).Error; err != nil {
			return err
		}
		for _, r := range recs {
			rel, ok := relativeName(r.Name, domain)
			if !ok {
				return ErrName
			}
			in, err := normalize(RecordInput{Name: rel, Type: r.Type, Value: r.Value, Priority: r.Priority, TTL: 300}, domain)
			if err != nil {
				return err
			}
			// что заменяем: у MX — все MX домена, у TXT — запись того же назначения (SPF — та, что начинается с v=spf1, DMARC — v=dmarc1)
			del := tx.Where("zone_id = ? AND name = ? AND type = ?", z.ID, in.Name, in.Type)
			switch r.Kind {
			case "spf":
				del = del.Where("lower(value) LIKE ?", "v=spf1%")
			case "dmarc":
				del = del.Where("lower(value) LIKE ?", "v=dmarc1%")
			case "dkim":
				del = del.Where("lower(value) LIKE ?", "v=dkim1%")
			}
			if err := del.Delete(&Record{}).Error; err != nil {
				return err
			}
			// CNAME на этом имени мешает (правило DNS): убираем и его, иначе зона не соберётся
			if err := tx.Where("zone_id = ? AND name = ? AND type = ?", z.ID, in.Name, "CNAME").Delete(&Record{}).Error; err != nil {
				return err
			}
			rec := Record{ZoneID: z.ID, Name: in.Name, Type: in.Type, Value: in.Value, Priority: in.Priority, TTL: in.TTL, Managed: ManagedMail, CreatedAt: s.now()}
			if err := tx.Create(&rec).Error; err != nil {
				return err
			}
		}
		var total int64
		if err := tx.Model(&Record{}).Where("zone_id = ?", z.ID).Count(&total).Error; err != nil {
			return err
		}
		if total > MaxRecords {
			return ErrRecordLimit.With(MaxRecords)
		}
		return s.bump(tx, z.ID)
	})
	if err != nil {
		return err
	}
	return s.syncAfter(ctx)
}
