package inbox

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"

	"kupol/internal/mail"
	"kupol/internal/testutil"
)

// fakeMailer — записывает письма вместо отправки; очередь шлёт их из горутин, поэтому список под замком.
type fakeMailer struct {
	mu      sync.Mutex
	enabled bool
	sent    []mail.Message
}

func (f *fakeMailer) Enabled(context.Context) bool { return f.enabled }

func (f *fakeMailer) Send(_ context.Context, m mail.Message) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, m)
	return nil
}

func (f *fakeMailer) messages() []mail.Message {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]mail.Message(nil), f.sent...)
}

// newTestUser — читатель с почтой (или без) и языком писем.
func newTestUser(t *testing.T, db *gorm.DB, login string, email *string, lang string) int64 {
	t.Helper()
	now := time.Now().UTC()
	var id int64
	if err := db.Raw(
		`INSERT INTO users (login, password_hash, backup_code_hash, created_at, password_changed_at, email, lang)
		 VALUES (?, 'x', 'y', ?, ?, ?, ?) RETURNING id`, login, now, now, email, lang).Scan(&id).Error; err != nil {
		t.Fatal(err)
	}
	return id
}

func outboxCount(t *testing.T, db *gorm.DB) int64 {
	t.Helper()
	var n int64
	if err := db.Raw(`SELECT count(*) FROM email_outbox`).Scan(&n).Error; err != nil {
		t.Fatal(err)
	}
	return n
}

// Send кладёт копию записки в очередь писем в той же транзакции, что и записку; письмо о повышении
// уровня уходит не отсюда — его шлёт accounts своим шаблоном (см. EmailPrefs, mail.LevelUp).
func TestSendQueuesEmailCopy(t *testing.T) {
	ctx := context.Background()
	db := testutil.NewMigratedDB(t)
	mailTo := "reader@example.org"
	uid := newTestUser(t, db, "reader", &mailTo, "ru")
	now := time.Now().UTC()

	if err := Send(ctx, db, uid, KindNote, map[string]any{"title": "Заголовок", "body": "Текст записки"}, now); err != nil {
		t.Fatal(err)
	}
	if n := outboxCount(t, db); n != 1 {
		t.Fatalf("в очереди %d писем, ожидалось 1", n)
	}

	// уровень: записка в ящик нужна, копия письма — нет (её шлёт accounts)
	if err := Send(ctx, db, uid, KindLevelUp, map[string]any{"level": 2}, now); err != nil {
		t.Fatal(err)
	}
	if n := outboxCount(t, db); n != 1 {
		t.Fatalf("после повышения в очереди %d писем, ожидалось 1", n)
	}
	var inboxRows int64
	if err := db.Raw(`SELECT count(*) FROM inbox_messages`).Scan(&inboxRows).Error; err != nil {
		t.Fatal(err)
	}
	if inboxRows != 2 {
		t.Fatalf("в ящике %d записок, ожидалось 2", inboxRows)
	}
}

// Drain отправляет копии записок тем, кто их просил, и убирает из очереди то, что отправлено
// или чему получатель не рад; пока почта выключена, очередь копится.
func TestDrainSendsOnlyToThoseWhoWant(t *testing.T) {
	ctx := context.Background()
	db := testutil.NewMigratedDB(t)
	wants, refuses := "wants@example.org", "refuses@example.org"
	wantsID := newTestUser(t, db, "wants", &wants, "ru")
	refusesID := newTestUser(t, db, "refuses", &refuses, "ru")
	if err := EmailPrefsSet(ctx, db, refusesID, EmailPrefs{Enabled: true}); err != nil { // остальные виды — false
		t.Fatal(err)
	}
	now := time.Now().UTC()
	for _, uid := range []int64{wantsID, refusesID} {
		if err := Send(ctx, db, uid, KindNote, map[string]any{"title": "Извещение", "body": "Текст"}, now); err != nil {
			t.Fatal(err)
		}
	}

	m := &fakeMailer{enabled: false}
	o := NewOutbox(db, m, "https://kupol.test", testutil.Logger(), nil)
	if sent, err := o.Drain(ctx); err != nil || sent != 0 {
		t.Fatalf("почта выключена: ушло %d, ошибка %v", sent, err)
	}
	if n := outboxCount(t, db); n != 2 {
		t.Fatalf("выключенная почта не должна трогать очередь: %d", n)
	}

	m.enabled = true
	sent, err := o.Drain(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if sent != 1 {
		t.Fatalf("ушло %d писем, ожидалось 1", sent)
	}
	msgs := m.messages()
	if len(msgs) != 1 {
		t.Fatalf("получено %d писем, ожидалось 1", len(msgs))
	}
	if msgs[0].To != wants {
		t.Errorf("письмо ушло не туда: %q", msgs[0].To)
	}
	if !strings.Contains(msgs[0].Subject, "Извещение") {
		t.Errorf("тема письма: %q", msgs[0].Subject)
	}
	// заметка без ссылки на архив — важен сам текст уведомления в письме
	if !strings.Contains(msgs[0].HTML, "Текст") {
		t.Errorf("в письме нет текста уведомления: %q", msgs[0].HTML)
	}
	if n := outboxCount(t, db); n != 0 {
		t.Fatalf("после отправки в очереди осталось %d писем", n)
	}
}

// Язык писма берётся из настроек читателя: уведомление, которое уходит без его запроса, — на его языке.
func TestDrainUsesReaderLanguage(t *testing.T) {
	ctx := context.Background()
	db := testutil.NewMigratedDB(t)
	mailTo := "lettore@example.org"
	uid := newTestUser(t, db, "lettore", &mailTo, "it")
	if err := Send(ctx, db, uid, KindAchievement, map[string]any{"name": "Посвящение"}, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}

	m := &fakeMailer{enabled: true}
	o := NewOutbox(db, m, "https://kupol.test", testutil.Logger(), nil)
	sent, err := o.Drain(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if sent != 1 {
		t.Fatalf("ушло %d писем, ожидалось 1", sent)
	}
	msgs := m.messages()
	if !strings.Contains(msgs[0].Subject, "Nuovo attestato") {
		t.Errorf("письмо не на языке читателя: %q", msgs[0].Subject)
	}
}

// SendNote — записка Директората: письма уходят всем, кому она ушла.
func TestSendNoteQueuesForRecipients(t *testing.T) {
	ctx := context.Background()
	db := testutil.NewMigratedDB(t)
	a, b := "a@example.org", "b@example.org"
	newTestUser(t, db, "reader_a", &a, "ru")
	newTestUser(t, db, "reader_b", &b, "ru")
	now := time.Now().UTC()

	n := Note{Title: "Извещение Директората", Body: "Текст записки"}
	if got, err := SendNote(ctx, db, "", n, now); err != nil || got != 2 {
		t.Fatalf("всем: %d записок, ошибка %v", got, err)
	}
	if n := outboxCount(t, db); n != 2 {
		t.Fatalf("в очереди %d писем, ожидалось 2", n)
	}
	if got, err := SendNote(ctx, db, "reader_a", n, now); err != nil || got != 1 {
		t.Fatalf("одному: %d записок, ошибка %v", got, err)
	}
	if n := outboxCount(t, db); n != 3 {
		t.Fatalf("в очереди %d писем, ожидалось 3", n)
	}
}
