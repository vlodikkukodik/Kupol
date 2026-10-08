package tickets

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"vladhost/internal/apperr"
	"vladhost/internal/auth"
	"vladhost/internal/testdb"
)

var bg = context.Background()

type mail struct {
	kind, subject, body string
	to                  []string
	ticket              int64
	followUp            bool
}

type fakeNotifier struct {
	mu   sync.Mutex
	sent []mail
}

func (f *fakeNotifier) TicketNew(_ context.Context, admins []auth.User, author auth.User, id int64, subject, body string, followUp bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var to []string
	for _, a := range admins {
		to = append(to, a.Username)
	}
	f.sent = append(f.sent, mail{kind: "new:" + author.Username, subject: subject, body: body, to: to, ticket: id, followUp: followUp})
}

func (f *fakeNotifier) TicketReply(_ context.Context, owner auth.User, id int64, subject, body string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, mail{kind: "reply", subject: subject, body: body, to: []string{owner.Username}, ticket: id})
}

type env struct {
	t     *testing.T
	svc   *Service
	auth  *auth.Service
	n     *fakeNotifier
	admin auth.User
	john  auth.User
	mary  auth.User
}

func newEnv(t *testing.T) *env {
	t.Helper()
	db := testdb.Open(t)
	e := &env{t: t, auth: auth.NewService(db, []byte(strings.Repeat("s", 32)), time.Minute, time.Hour), n: &fakeNotifier{}}
	e.svc = New(db, e.n)
	mk := func(prefix string, admin bool) auth.User {
		name := fmt.Sprintf("%s%d", prefix, time.Now().UnixNano()%1_000_000_000)
		u, err := e.auth.CreateAdmin(bg, name+"@example.com", name, "password123")
		if err != nil {
			t.Fatal(err)
		}
		if !admin {
			db.Exec("UPDATE users SET role = 'user' WHERE id = ?", u.ID)
			u.Role = auth.RoleUser
		}
		return *u
	}
	e.admin, e.john, e.mary = mk("adm", true), mk("jo", false), mk("ma", false)
	return e
}

func code(err error) string {
	var ae *apperr.Error
	if errors.As(err, &ae) {
		return ae.Code
	}
	return ""
}

func TestTicketLifecycleWithSupport(t *testing.T) {
	e := newEnv(t)
	tk, err := e.svc.Create(bg, e.john, "  Не открывается сайт ", "sites", "Здравствуйте!\r\nСайт отдаёт 502.")
	if err != nil || tk.Status != StatusOpen || tk.Subject != "Не открывается сайт" {
		t.Fatalf("%+v %v", tk, err)
	}
	// администраторы получили письмо о новом обращении, автор — нет
	if len(e.n.sent) != 1 || e.n.sent[0].kind != "new:"+e.john.Username || e.n.sent[0].followUp || len(e.n.sent[0].to) != 1 || e.n.sent[0].to[0] != e.admin.Username {
		t.Fatalf("%+v", e.n.sent)
	}
	v, err := e.svc.Get(bg, e.john, tk.ID)
	if err != nil || len(v.Thread) != 1 || v.Thread[0].Body != "Здравствуйте!\nСайт отдаёт 502." || v.Email != "" || !v.CanReply {
		t.Fatalf("%+v %v", v, err)
	}

	// ответ поддержки: статус «отвечен», владелец получает письмо, имя администратора ему не показывается
	if _, err := e.svc.Reply(bg, e.admin, tk.ID, "Посмотрите журнал ошибок сайта."); err != nil {
		t.Fatal(err)
	}
	v, _ = e.svc.Get(bg, e.john, tk.ID)
	if v.Status != StatusAnswered || len(v.Thread) != 2 || !v.Thread[1].Staff || v.Thread[1].Author != "" {
		t.Fatalf("владелец: %+v", v)
	}
	av, _ := e.svc.Get(bg, e.admin, tk.ID)
	if av.Email != e.john.Email || av.Username != e.john.Username || av.Thread[1].Author != e.admin.Username {
		t.Fatalf("администратор: %+v", av)
	}
	last := e.n.sent[len(e.n.sent)-1]
	if last.kind != "reply" || last.to[0] != e.john.Username || last.ticket != tk.ID || last.body != "Посмотрите журнал ошибок сайта." {
		t.Fatalf("%+v", last)
	}
	if n, _ := e.svc.Waiting(bg, e.john); n != 1 {
		t.Fatalf("у пользователя ждёт ответ: %d", n)
	}
	if n, _ := e.svc.Waiting(bg, e.admin); n != 0 {
		t.Fatalf("у администратора ждущих нет: %d", n)
	}

	// ответ пользователя возвращает тикет в очередь поддержки и оповещает администраторов
	if _, err := e.svc.Reply(bg, e.john, tk.ID, "Спасибо, помогло!"); err != nil {
		t.Fatal(err)
	}
	v, _ = e.svc.Get(bg, e.john, tk.ID)
	if v.Status != StatusOpen {
		t.Fatalf("%+v", v)
	}
	if l := e.n.sent[len(e.n.sent)-1]; !l.followUp || l.subject != "Не открывается сайт" {
		t.Fatalf("%+v", l)
	}
	if n, _ := e.svc.Waiting(bg, e.admin); n != 1 {
		t.Fatalf("очередь администратора: %d", n)
	}

	// закрытие и повторное открытие; ответ в закрытом тикете открывает его снова
	if err := e.svc.Close(bg, e.john, tk.ID); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.Close(bg, e.john, tk.ID); code(err) != "ticket_closed" {
		t.Fatalf("повторное закрытие: %v", err)
	}
	if err := e.svc.Reopen(bg, e.john, tk.ID); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.Reopen(bg, e.john, tk.ID); code(err) != "ticket_not_closed" {
		t.Fatalf("открытый тикет не открывают: %v", err)
	}
	_ = e.svc.Close(bg, e.admin, tk.ID)
	if _, err := e.svc.Reply(bg, e.admin, tk.ID, "Ещё вопрос по домену"); err != nil {
		t.Fatal(err)
	}
	v, _ = e.svc.Get(bg, e.john, tk.ID)
	if v.Status != StatusAnswered || v.ClosedAt != nil {
		t.Fatalf("%+v", v)
	}
}

func TestOwnershipAndAdminQueue(t *testing.T) {
	e := newEnv(t)
	tj, _ := e.svc.Create(bg, e.john, "Вопрос про почту", "mail", "Как настроить?")
	tm, _ := e.svc.Create(bg, e.mary, "Вопрос про DNS", "dns", "Как делегировать?")

	// чужой тикет недоступен ни на чтение, ни на запись
	if _, err := e.svc.Get(bg, e.mary, tj.ID); code(err) != "ticket_not_found" {
		t.Fatalf("%v", err)
	}
	if _, err := e.svc.Reply(bg, e.mary, tj.ID, "я тут"); code(err) != "ticket_not_found" {
		t.Fatalf("%v", err)
	}
	if err := e.svc.Close(bg, e.mary, tj.ID); code(err) != "ticket_not_found" {
		t.Fatalf("%v", err)
	}
	if err := e.svc.Reopen(bg, e.mary, tj.ID); code(err) != "ticket_not_found" {
		t.Fatalf("%v", err)
	}
	mine, _ := e.svc.List(bg, e.john.ID)
	if len(mine) != 1 || mine[0].ID != tj.ID || mine[0].Messages != 1 {
		t.Fatalf("%+v", mine)
	}
	// очередь видна только администратору
	if _, err := e.svc.AdminList(bg, e.john, "", ""); code(err) != "ticket_not_found" {
		t.Fatalf("%v", err)
	}
	all, err := e.svc.AdminList(bg, e.admin, "", "")
	if err != nil || len(all) < 2 {
		t.Fatalf("%v %+v", err, all)
	}
	byName, _ := e.svc.AdminList(bg, e.admin, "", strings.ToUpper(e.mary.Username))
	if len(byName) != 1 || byName[0].ID != tm.ID || byName[0].Username != e.mary.Username {
		t.Fatalf("поиск по имени: %+v", byName)
	}
	if bySubject, _ := e.svc.AdminList(bg, e.admin, StatusOpen, "про почту"); len(bySubject) != 1 || bySubject[0].ID != tj.ID {
		t.Fatalf("поиск по теме: %+v", bySubject)
	}
	if wild, _ := e.svc.AdminList(bg, e.admin, "", "%"); len(wild) != 0 {
		t.Fatalf("символы шаблона экранируются: %+v", wild)
	}
	_ = e.svc.Close(bg, e.admin, tm.ID)
	if closed, _ := e.svc.AdminList(bg, e.admin, StatusClosed, e.mary.Username); len(closed) != 1 || closed[0].Status != StatusClosed {
		t.Fatalf("%+v", closed)
	}
	// администратор может писать и в свой тикет — как обычный автор
	ta, err := e.svc.Create(bg, e.admin, "Мой вопрос", "general", "Проверка")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Reply(bg, e.admin, ta.ID, "Дополнение"); err != nil {
		t.Fatal(err)
	}
	if v, _ := e.svc.Get(bg, e.admin, ta.ID); v.Status != StatusOpen || v.Thread[1].Staff {
		t.Fatalf("%+v", v)
	}
}

func TestValidationAndLimits(t *testing.T) {
	e := newEnv(t)
	cases := []struct {
		name, subject, category, body, want string
	}{
		{"короткая тема", "ab", "general", "текст", "validation.ticket_subject"},
		{"длинная тема", strings.Repeat("я", MaxSubject+1), "general", "текст", "validation.ticket_subject"},
		{"тема с переводом строки", "тема\nвторая", "general", "текст", "validation.ticket_subject"},
		{"пустой текст", "Тема", "general", "  \n ", "validation.ticket_body"},
		{"длинный текст", "Тема", "general", strings.Repeat("я", MaxBody+1), "validation.ticket_body"},
		{"управляющий символ", "Тема", "general", "a\x00b", "validation.ticket_body"},
		{"категория", "Тема", "hack", "текст", "validation.ticket_category"},
	}
	for _, tc := range cases {
		if _, err := e.svc.Create(bg, e.john, tc.subject, tc.category, tc.body); code(err) != tc.want {
			t.Errorf("%s: %v (код %q, ждали %q)", tc.name, err, code(err), tc.want)
		}
	}
	for _, c := range Categories {
		if _, err := e.svc.Create(bg, e.mary, "Тема "+c, c, "текст"); code(err) == "validation.ticket_category" {
			t.Errorf("категория %s должна быть допустима", c)
		}
	}
	if list, _ := e.svc.List(bg, e.john.ID); len(list) != 0 {
		t.Fatal("отказанные обращения не сохраняются")
	}
}

func TestOpenTicketLimit(t *testing.T) {
	e := newEnv(t)
	var first int64
	for i := 0; i < MaxOpenPerUser; i++ {
		tk, err := e.svc.Create(bg, e.john, fmt.Sprintf("Обращение %d", i), "general", "текст")
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			first = tk.ID
		}
	}
	if _, err := e.svc.Create(bg, e.john, "Ещё одно", "general", "текст"); code(err) != "ticket_limit" {
		t.Fatalf("%v", err)
	}
	// закрытое освобождает место, а открыть его снова можно, только если место есть
	if err := e.svc.Close(bg, e.john, first); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Create(bg, e.john, "Ещё одно", "general", "текст"); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.Reopen(bg, e.john, first); code(err) != "ticket_limit" {
		t.Fatalf("%v", err)
	}
	if err := e.svc.Reopen(bg, e.admin, first); err != nil {
		t.Fatalf("администратор может открыть и сверх предела: %v", err)
	}
	// параллельные создания не превышают предел
	e2 := newEnv(t)
	var wg sync.WaitGroup
	var ok int
	var mu sync.Mutex
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if _, err := e2.svc.Create(bg, e2.mary, fmt.Sprintf("Гонка %d", i), "general", "текст"); err == nil {
				mu.Lock()
				ok++
				mu.Unlock()
			}
		}(i)
	}
	wg.Wait()
	if ok != MaxOpenPerUser {
		t.Fatalf("создано %d, предел %d", ok, MaxOpenPerUser)
	}
}

func TestMessageCapAndRate(t *testing.T) {
	e := newEnv(t)
	tk, _ := e.svc.Create(bg, e.john, "Длинная переписка", "general", "старт")
	// поддержка может отвечать сколько нужно: ограничение — только на число сообщений в тикете
	for i := 0; i < MaxMessages-1; i++ {
		if _, err := e.svc.Reply(bg, e.admin, tk.ID, fmt.Sprintf("ответ %d", i)); err != nil {
			t.Fatalf("ответ %d: %v", i, err)
		}
	}
	if _, err := e.svc.Reply(bg, e.admin, tk.ID, "лишний"); code(err) != "ticket_full" {
		t.Fatalf("%v", err)
	}
	if _, err := e.svc.Reply(bg, e.john, tk.ID, "лишний"); code(err) != "ticket_full" {
		t.Fatalf("%v", err)
	}
	if v, _ := e.svc.Get(bg, e.john, tk.ID); v.CanReply || len(v.Thread) != MaxMessages {
		t.Fatalf("%v %d", v.CanReply, len(v.Thread))
	}
	// частота сообщений пользователя
	e2 := newEnv(t)
	tk2, _ := e2.svc.Create(bg, e2.mary, "Спам", "general", "1")
	var got error
	for i := 0; i < MaxUserPerHour+2 && got == nil; i++ {
		_, got = e2.svc.Reply(bg, e2.mary, tk2.ID, fmt.Sprintf("сообщение %d", i))
	}
	if code(got) != "ticket_rate" {
		t.Fatalf("%v", got)
	}
}

func TestNoNotifierIsFine(t *testing.T) {
	e := newEnv(t)
	svc := New(e.svc.db, nil)
	tk, err := svc.Create(bg, e.john, "Без писем", "general", "текст")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Reply(bg, e.admin, tk.ID, "ответ"); err != nil {
		t.Fatal(err)
	}
}
