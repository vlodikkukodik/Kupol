package mailhost

import (
	"context"
	"strings"

	"vladhost/internal/domainproof"
)

// DomainView — домен со всем, что нужно интерфейсу.
type DomainView struct {
	Domain
	Mailboxes []Mailbox `json:"mailboxes"`
	Aliases   []Alias   `json:"aliases"`
	Records   []Record  `json:"records,omitempty"`
	// Для неподтверждённого домена: какую TXT-запись создать у текущего DNS-провайдера.
	VerifyName  string `json:"verify_name,omitempty"`
	VerifyValue string `json:"verify_value,omitempty"`
}

// Info — сведения для интерфейса: настройки клиентов и лимиты.
type Info struct {
	Host       string   `json:"host"`
	IMAPPort   int      `json:"imap_port"`
	POP3Port   int      `json:"pop3_port"`
	SMTPPorts  []int    `json:"smtp_ports"`
	MaxDomains int      `json:"max_domains"`
	MaxBoxes   int      `json:"max_mailboxes"`
	MaxAliases int      `json:"max_aliases"`
	MinQuota   int      `json:"min_quota_mb"`
	MaxQuota   int      `json:"max_quota_mb"`
	DefaultQ   int      `json:"default_quota_mb"`
	MinPass    int      `json:"min_password"`
	Eligible   []string `json:"eligible_domains"`
	WebmailURL string   `json:"webmail_url"`
}

// Info возвращает настройки для пользователя.
func (s *Service) Info(ctx context.Context, userID int64) (*Info, error) {
	el, err := s.Eligible(ctx, userID)
	if err != nil {
		return nil, err
	}
	return &Info{Host: s.cfg.Host, IMAPPort: 993, POP3Port: 995, SMTPPorts: []int{465, 587}, MaxDomains: MaxDomains, MaxBoxes: MaxMailboxes, MaxAliases: MaxAliases,
		MinQuota: MinQuotaMB, MaxQuota: MaxQuotaMB, DefaultQ: DefaultQuotaMB, MinPass: MinPassword, Eligible: el, WebmailURL: s.cfg.WebmailURL}, nil
}

// List возвращает домены пользователя с ящиками и алиасами (без проверки DNS: она идёт отдельным запросом).
func (s *Service) List(ctx context.Context, userID int64) ([]DomainView, error) {
	var doms []Domain
	if err := s.db.WithContext(ctx).Where("user_id = ?", userID).Order("id").Find(&doms).Error; err != nil {
		return nil, err
	}
	out := make([]DomainView, 0, len(doms))
	if len(doms) == 0 {
		return out, nil
	}
	ids := make([]int64, len(doms))
	for i, d := range doms {
		ids[i] = d.ID
	}
	var boxes []Mailbox
	if err := s.db.WithContext(ctx).Where("domain_id IN ?", ids).Order("id").Find(&boxes).Error; err != nil {
		return nil, err
	}
	var aliases []Alias
	if err := s.db.WithContext(ctx).Where("domain_id IN ?", ids).Order("id").Find(&aliases).Error; err != nil {
		return nil, err
	}
	usage := s.usage()
	for _, d := range doms {
		v := DomainView{Domain: d, Mailboxes: []Mailbox{}, Aliases: []Alias{}}
		if !d.Verified {
			v.VerifyName, v.VerifyValue = domainproof.RecordName(d.Domain), domainproof.RecordValue(d.Token)
		}
		for _, b := range boxes {
			if b.DomainID == d.ID {
				b.UsedBytes = usage[d.Domain][b.LocalPart]
				v.Mailboxes = append(v.Mailboxes, b)
			}
		}
		for _, a := range aliases {
			if a.DomainID == d.ID {
				a.To = strings.Split(a.Destinations, "\n")
				v.Aliases = append(v.Aliases, a)
			}
		}
		out = append(out, v)
	}
	return out, nil
}

// CheckDNS проверяет DNS-записи домена.
func (s *Service) CheckDNS(ctx context.Context, userID, id int64) ([]Record, error) {
	d, err := s.domain(ctx, userID, id)
	if err != nil {
		return nil, err
	}
	return s.Records(ctx, d), nil
}

// Fill — заполненность ящика (для писем о том, что место заканчивается).
type Fill struct {
	UserID  int64
	Address string
	Used    int64
	Quota   int64
}

// Fill возвращает заполненность всех ящиков по последней синхронизации.
func (s *Service) Fill(ctx context.Context) ([]Fill, error) {
	type row struct {
		UserID    int64
		Domain    string
		LocalPart string
		QuotaMB   int
	}
	var rows []row
	err := s.db.WithContext(ctx).Table("mailboxes b").Select("d.user_id AS user_id, d.domain AS domain, b.local_part AS local_part, b.quota_mb AS quota_mb").
		Joins("JOIN mail_domains d ON d.id = b.domain_id").Where("d.enabled AND d.verified").Order("b.id").Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	usage := s.usage()
	out := make([]Fill, 0, len(rows))
	for _, r := range rows {
		out = append(out, Fill{UserID: r.UserID, Address: r.LocalPart + "@" + r.Domain, Used: usage[r.Domain][r.LocalPart], Quota: int64(r.QuotaMB) << 20})
	}
	return out, nil
}
