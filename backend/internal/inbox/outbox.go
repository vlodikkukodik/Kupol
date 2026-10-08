package inbox

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"

	"kupol/internal/i18n"
	"kupol/internal/mail"
)

// Mailer — отправка писем из очереди. Enabled сообщает, настроен ли SMTP прямо сейчас: настройки читает
// вызывающий (mail.Dynamic спрашивает базу, которую правит Директорат), очередь их не хранит.
type Mailer interface {
	Enabled(ctx context.Context) bool
	Send(ctx context.Context, msg mail.Message) error
}

const (
	outboxBatch    = 10                    // сколько писем за один проход
	outboxAttempts = 5                     // после стольких неудач остаётся только записка в ящике
	outboxKeep     = 7 * 24 * time.Hour    // недоставленное старше недели удаляется
	outboxEvery    = 5 * time.Second       // как часто пускать проход
	outboxSendWait = 20 * time.Second      // таймаут одного письма
)

type outboxRow struct {
	ID        int64
	UserID    int64
	Kind      string
	Params    []byte `gorm:"column:params"`
	Attempts  int
	CreatedAt time.Time
	Email     *string
	Lang      string
	Login     string
	Prefs     []byte `gorm:"column:email_prefs"`
}

// Outbox — фоновая отправка писем-копий записок: читает очередь, собирает письмо на языке читателя
// тем же текстом, что он видит в ящике, и шлёт его текущими настройками SMTP.
type Outbox struct {
	db         *gorm.DB
	mailer     Mailer
	siteOrigin string
	log        *slog.Logger
	now        func() time.Time
}

// NewOutbox создаёт очередь. now == nil — настоящее время.
func NewOutbox(db *gorm.DB, mailer Mailer, siteOrigin string, log *slog.Logger, now func() time.Time) *Outbox {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &Outbox{db: db, mailer: mailer, siteOrigin: siteOrigin, log: log, now: now}
}

// Run — фоновая очередь: работает, пока контекст жив (см. cmd/kupol).
func (o *Outbox) Run(ctx context.Context) {
	t := time.NewTicker(outboxEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if _, err := o.Drain(ctx); err != nil {
				o.log.Warn("почта: очередь писем", "err", err)
			}
		}
	}
}

// Drain — один проход очереди: возвращает, сколько писем ушло. Пока почта выключена, очередь копится
// (и недоставленное старше недели удаляется) — письма старой давности никому не нужны.
func (o *Outbox) Drain(ctx context.Context) (int, error) {
	now := o.now()
	err := o.db.WithContext(ctx).Exec(
		`DELETE FROM email_outbox WHERE created_at < ? OR attempts >= ?`, now.Add(-outboxKeep), outboxAttempts).Error
	if err != nil {
		return 0, err
	}
	if !o.mailer.Enabled(ctx) {
		return 0, nil
	}
	var rows []outboxRow
	err = o.db.WithContext(ctx).Raw(`
		SELECT o.id, o.user_id, o.kind, o.params, o.attempts, o.created_at,
		       u.email, u.lang, u.login::text AS login, u.email_prefs
		FROM email_outbox o
		JOIN users u ON u.id = o.user_id
		ORDER BY o.id
		LIMIT ?`, outboxBatch).Scan(&rows).Error
	if err != nil {
		return 0, err
	}

	type job struct {
		id   int64
		msg  mail.Message
		skip bool // получателю такое письмо не нужно — строку просто убираем
	}
	jobs := make([]job, len(rows))
	for i, r := range rows {
		jobs[i].id = r.ID
		prefs := ParseEmailPrefs(r.Prefs)
		if r.Email == nil || !prefs.Wants(Kind(r.Kind)) {
			jobs[i].skip = true
			continue
		}
		lang := langOf(r.Lang)
		item := render(row{ID: r.ID, Kind: Kind(r.Kind), Params: r.Params, CreatedAt: r.CreatedAt}, lang)
		msg := mail.Notification(lang, item.Title, item.Body, o.link(item.Link))
		msg.To = *r.Email
		jobs[i].msg = msg
	}

	// отправка вне транзакции и в фоне: медленный SMTP не должен держать соединения базы
	failed := make([]bool, len(jobs))
	var wg sync.WaitGroup
	for i := range jobs {
		if jobs[i].skip {
			continue
		}
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			sctx, cancel := context.WithTimeout(ctx, outboxSendWait)
			defer cancel()
			if err := o.mailer.Send(sctx, jobs[i].msg); err != nil {
				failed[i] = true
				o.log.Warn("почта: не удалось отправить уведомление", "to", jobs[i].msg.To, "err", err)
			}
		}(i)
	}
	wg.Wait()

	// итоги — по одному, чтобы не дёргать соединения базы из горутин
	var sent int
	for i, j := range jobs {
		if j.skip {
			o.drop(ctx, j.id)
			continue
		}
		if failed[i] {
			o.retry(ctx, j.id)
			continue
		}
		o.drop(ctx, j.id)
		sent++
	}
	return sent, nil
}

// link — абсолютная ссылка на то, куда ведёт записка; пустой origin — ссылки нет, как и кнопки в письме.
func (o *Outbox) link(itemLink string) string {
	if itemLink == "" || o.siteOrigin == "" {
		return ""
	}
	return strings.TrimRight(o.siteOrigin, "/") + itemLink
}

// drop убирает письмо из очереди (отправлено или получателю не нужно).
func (o *Outbox) drop(ctx context.Context, id int64) {
	if err := o.db.WithContext(ctx).Exec(`DELETE FROM email_outbox WHERE id = ?`, id).Error; err != nil {
		o.log.Warn("почта: не удалось убрать письмо из очереди", "id", id, "err", err)
	}
}

// retry отмечает неудачу; письмо, которое не ушло столько раз, удаляется при следующем проходе.
func (o *Outbox) retry(ctx context.Context, id int64) {
	if err := o.db.WithContext(ctx).Exec(`UPDATE email_outbox SET attempts = attempts + 1 WHERE id = ?`, id).Error; err != nil {
		o.log.Warn("почта: не удалось отметить неудачу", "id", id, "err", err)
	}
}

func langOf(s string) i18n.Lang {
	l := i18n.Lang(s)
	if !l.Valid() {
		return i18n.Default
	}
	return l
}
