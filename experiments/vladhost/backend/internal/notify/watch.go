package notify

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"vladhost/internal/auth"
	"vladhost/internal/cronjobs"
	"vladhost/internal/sites"
	"vladhost/internal/userdb"
)

const (
	// DiskWarnPercent — с какой заполненности диска идёт письмо; заново оно пойдёт, только когда занятое место упадёт ниже DiskClearPercent.
	DiskWarnPercent  = 90
	DiskClearPercent = 80
	// MailboxWarnPercent и MailboxClearPercent — то же для почтового ящика.
	MailboxWarnPercent  = 90
	MailboxClearPercent = 80
	// CertWarnDays — за сколько суток до окончания сертификата идёт первое письмо (автопродление к этому сроку должно было сработать);
	// второе, срочное, — за CertUrgentDays.
	CertWarnDays   = 14
	CertUrgentDays = 3
)

// Watch раз в every проверяет сайты пользователей и отправляет письма о проблемах, пока не отменён ctx.
func (s *Service) Watch(ctx context.Context, siteSvc *sites.Service, every time.Duration) {
	if !s.Enabled() {
		return
	}
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		if err := s.Scan(ctx, siteSvc); err != nil && ctx.Err() == nil {
			log.Printf("уведомления: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// Scan — один проход проверок. Письма получают только пользователи с подтверждённым адресом и включённым согласием.
func (s *Service) Scan(ctx context.Context, siteSvc *sites.Service) error {
	var users []auth.User
	if err := s.db.WithContext(ctx).Where("notify_email = true AND email_verified_at IS NOT NULL").Find(&users).Error; err != nil {
		return err
	}
	if len(users) == 0 {
		return nil
	}
	byID := make(map[int64]auth.User, len(users))
	for _, u := range users {
		byID[u.ID] = u
	}
	var list []sites.Site
	if err := s.db.WithContext(ctx).Find(&list).Error; err != nil {
		return err
	}
	var domains []sites.Domain
	if err := s.db.WithContext(ctx).Find(&domains).Error; err != nil {
		return err
	}
	domainsBySite := map[int64][]sites.Domain{}
	for _, d := range domains {
		domainsBySite[d.SiteID] = append(domainsBySite[d.SiteID], d)
	}

	used := map[int64]int64{}
	var firstErr error
	note := func(err error) {
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}
	for _, st := range list {
		used[st.UserID] += st.DiskBytes
		u, ok := byID[st.UserID]
		if !ok {
			continue
		}
		link := s.Link(fmt.Sprintf("/sites/%d/ssl", st.ID))
		switch st.CertStatus {
		case sites.CertFailed:
			note(s.once(ctx, u, KindCertFailed, st.Host+"|"+unix(st.CertRequestedAt), Data{Host: st.Host, Reason: st.CertError, Link: link}))
		case sites.CertActive:
			note(s.expiring(ctx, siteSvc, u, st.Host, link))
		}
		for _, d := range domainsBySite[st.ID] {
			switch d.Status {
			case sites.DomainFailed:
				note(s.once(ctx, u, KindCertFailed, d.Host+"|"+unix(d.CertRequestedAt), Data{Host: d.Host, Reason: d.Error, Link: link}))
			case sites.DomainActive:
				note(s.expiring(ctx, siteSvc, u, d.Host, link))
			}
		}
	}

	// Базы данных, переведённые в режим чтения: письмо один раз на каждое замораживание.
	var frozen []userdb.Database
	if err := s.db.WithContext(ctx).Where("status = ?", userdb.StatusFrozen).Find(&frozen).Error; err != nil {
		note(err)
	}
	for _, d := range frozen {
		if u, ok := byID[d.UserID]; ok {
			size := d.SizeBytes
			note(s.once(ctx, u, KindDBFrozen, fmt.Sprintf("db|%d|%s", d.ID, unix(d.FrozenAt)),
				Data{Host: d.Name, Used: humanSize(size), Total: humanSize(s.dbLimit), Link: s.Link("/databases")}))
		}
	}

	// Задачи планировщика, упавшие несколько раз подряд: письмо один раз на серию неудач.
	var failing []cronjobs.Job
	if err := s.db.WithContext(ctx).Where("enabled AND fail_streak >= ?", cronjobs.FailNotifyStreak).Find(&failing).Error; err != nil {
		note(err)
	}
	for _, j := range failing {
		if u, ok := byID[j.UserID]; ok {
			var last cronjobs.Run
			_ = s.db.WithContext(ctx).Where("job_id = ? AND status <> ?", j.ID, cronjobs.RunRunning).Order("id DESC").Limit(1).Find(&last).Error
			note(s.once(ctx, u, KindCronFailed, fmt.Sprintf("cron|%d|%s", j.ID, unix(j.FailingSince)),
				Data{Host: j.Name, Days: j.FailStreak, Reason: shorten(strings.TrimSpace(last.Output+" "+last.Reason), 300), Link: s.Link("/cron")}))
		}
	}

	// Почтовые ящики: письмо, когда ящик заполнен на MailboxWarnPercent и больше; заново — после того, как заполненность упала ниже MailboxClearPercent.
	s.fillMu.RLock()
	fill := s.mailFill
	s.fillMu.RUnlock()
	if fill != nil {
		rows, err := fill(ctx)
		if err != nil {
			note(err)
		}
		for _, r := range rows {
			u, ok := byID[r.UserID]
			if !ok || r.Quota <= 0 {
				continue
			}
			pct := int(r.Used * 100 / r.Quota)
			key := r.Address + "|warn"
			switch {
			case pct >= MailboxWarnPercent:
				note(s.once(ctx, u, KindMailboxFull, key, Data{Host: r.Address, Percent: pct, Used: humanSize(r.Used), Total: humanSize(r.Quota), Link: s.Link("/mail")}))
			case pct < MailboxClearPercent:
				note(s.db.WithContext(ctx).Where("user_id = ? AND kind = ? AND dedupe_key = ?", u.ID, KindMailboxFull, key).Delete(&notification{}).Error)
			}
		}
	}

	quota := siteSvc.Limits().DiskQuotaBytes
	if quota > 0 {
		for _, u := range users {
			pct := int(used[u.ID] * 100 / quota)
			switch {
			case pct >= DiskWarnPercent:
				note(s.once(ctx, u, KindDiskFull, "warn", Data{Percent: pct, Used: humanSize(used[u.ID]), Total: humanSize(quota), Link: s.Link("/sites")}))
			case pct < DiskClearPercent:
				// Место освободили: следующее заполнение снова заслуживает письма.
				note(s.db.WithContext(ctx).Where("user_id = ? AND kind = ?", u.ID, KindDiskFull).Delete(&notification{}).Error)
			}
		}
	}
	return firstErr
}

// expiring: письмо, когда работающему сертификату осталось CertWarnDays суток или меньше, и второе — при CertUrgentDays.
func (s *Service) expiring(ctx context.Context, siteSvc *sites.Service, u auth.User, host, link string) error {
	c := siteSvc.CertInfo(host)
	if c == nil {
		return nil
	}
	days := c.DaysLeft(s.now())
	if days > CertWarnDays {
		return nil
	}
	stamp := c.NotAfter.UTC().Format("2006-01-02")
	if days <= CertUrgentDays {
		// Срочное письмо заменяет обычное: если пользователь уже получил первое, второе всё равно нужно.
		return s.once(ctx, u, KindCertExpiring, host+"|"+stamp+"|urgent", Data{Host: host, Days: days, Link: link})
	}
	return s.once(ctx, u, KindCertExpiring, host+"|"+stamp+"|warn", Data{Host: host, Days: days, Link: link})
}

type notification struct {
	ID        int64 `gorm:"primaryKey"`
	UserID    int64
	Kind      string
	DedupeKey string
	CreatedAt time.Time
}

func (notification) TableName() string { return "notifications" }

// once отправляет уведомление, если такое (пользователь, вид, ключ) ещё не отправлялось. Запись в журнал и письмо в очередь —
// одной транзакцией: не будет ни потерянного письма, ни повторного.
func (s *Service) once(ctx context.Context, u auth.User, kind, key string, d Data) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Clauses(clause.OnConflict{DoNothing: true}).
			Create(&notification{UserID: u.ID, Kind: kind, DedupeKey: key, CreatedAt: s.now()})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return nil
		}
		return s.send(ctx, tx, u, kind, d)
	})
}

// shorten: первая часть вывода задачи в одну-две строки, для письма.
func shorten(s string, n int) string {
	s = strings.TrimSpace(s)
	if r := []rune(s); len(r) > n {
		s = string(r[:n]) + "…"
	}
	if s == "" {
		s = "—"
	}
	return s
}

func unix(t *time.Time) string {
	if t == nil {
		return "0"
	}
	return strconv.FormatInt(t.Unix(), 10)
}

func humanSize(n int64) string {
	const mb = 1 << 20
	if n >= 1<<30 {
		return fmt.Sprintf("%.1f GB", float64(n)/(1<<30)) // единицы одинаковы для обоих языков писем
	}
	return fmt.Sprintf("%d MB", n/mb)
}
