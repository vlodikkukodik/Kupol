package httpapi_test

import (
	"fmt"
	"strings"
	"testing"

	"vladhost/internal/config"
	"vladhost/internal/httpapi"
	"vladhost/internal/tickets"
)

func (e *env) withSupport() {
	e.t.Helper()
	e.r = httpapi.New(e.svc, e.sites, config.Config{JWTSecret: []byte(strings.Repeat("s", 32)), AuthPerMinute: 10000, AuthBurst: 10000}, httpapi.WithTickets(tickets.New(e.db, nil)))
}

func TestSupportHiddenWhenNotConfigured(t *testing.T) {
	e := newEnv(t)
	adm, _ := e.admin()
	tok := e.user(adm, "john")
	for _, tc := range [][2]string{{"GET", "/api/tickets"}, {"POST", "/api/tickets"}, {"GET", "/api/tickets/summary"}, {"GET", "/api/tickets/1"}, {"POST", "/api/tickets/1/messages"}, {"POST", "/api/tickets/1/close"}, {"POST", "/api/tickets/1/reopen"}, {"GET", "/api/admin/tickets"}} {
		if w := e.do(tc[0], tc[1], map[string]any{}, adm); w.Code != 404 {
			t.Errorf("%s %s: %d", tc[0], tc[1], w.Code)
		}
	}
	_ = tok
}

func TestSupportThroughAPI(t *testing.T) {
	e := newEnv(t)
	e.withSupport()
	adm, _ := e.admin()
	john, mary := e.user(adm, "john"), e.user(adm, "mary")
	if w := e.do("GET", "/api/tickets", nil, ""); w.Code != 401 {
		t.Fatalf("без входа: %d", w.Code)
	}
	type item struct {
		ID       int64  `json:"id"`
		Subject  string `json:"subject"`
		Status   string `json:"status"`
		Username string `json:"username"`
		Messages int    `json:"messages"`
	}
	type list struct {
		Tickets    []item   `json:"tickets"`
		Categories []string `json:"categories"`
		MaxOpen    int      `json:"max_open"`
	}
	l := decode[list](t, e.do("GET", "/api/tickets", nil, john))
	if l.Tickets == nil || len(l.Tickets) != 0 || len(l.Categories) != 7 || l.MaxOpen != 5 {
		t.Fatalf("%+v", l)
	}

	// ошибки формы приходят на языке пользователя и указывают на поле
	if w := e.do("POST", "/api/tickets", map[string]string{"subject": "ab", "category": "sites", "message": "x"}, john); w.Code != 422 || decode[errBody](t, w).Error.Code != "validation.ticket_subject" {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if w := e.do("POST", "/api/tickets", map[string]string{"subject": "Проблема", "category": "nope", "message": "x"}, john); w.Code != 422 || decode[errBody](t, w).Error.Code != "validation.ticket_category" {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	w := e.do("POST", "/api/tickets", map[string]string{"subject": "Не открывается сайт", "category": "sites", "message": "Сайт отдаёт 502"}, john)
	if w.Code != 201 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	id := decode[struct {
		Ticket item `json:"ticket"`
	}](t, w).Ticket.ID
	base := fmt.Sprintf("/api/tickets/%d", id)

	// чужой пользователь не видит и не пишет; администратор видит в очереди
	for _, tc := range [][2]string{{"GET", base}, {"POST", base + "/messages"}, {"POST", base + "/close"}, {"POST", base + "/reopen"}} {
		if w := e.do(tc[0], tc[1], map[string]string{"message": "я тут"}, mary); w.Code != 404 {
			t.Errorf("чужой %s %s: %d", tc[0], tc[1], w.Code)
		}
	}
	if w := e.do("GET", "/api/admin/tickets", nil, john); w.Code != 403 {
		t.Fatalf("очередь только администратору: %d", w.Code)
	}
	q := decode[list](t, e.do("GET", "/api/admin/tickets?status=open&q=john", nil, adm))
	if len(q.Tickets) != 1 || q.Tickets[0].ID != id || q.Tickets[0].Username != "john" {
		t.Fatalf("%+v", q)
	}
	if decode[struct{ Waiting int }](t, e.do("GET", "/api/tickets/summary", nil, adm)).Waiting != 1 {
		t.Fatal("администратору ждёт одно обращение")
	}

	// ответ поддержки
	type view struct {
		Ticket struct {
			Status   string `json:"status"`
			Email    string `json:"email"`
			CanReply bool   `json:"can_reply"`
			Thread   []struct {
				Staff  bool   `json:"staff"`
				Author string `json:"author"`
				Body   string `json:"body"`
			} `json:"thread"`
		} `json:"ticket"`
	}
	w = e.do("POST", base+"/messages", map[string]string{"message": "Посмотрите журнал ошибок"}, adm)
	if w.Code != 201 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if v := decode[view](t, w); v.Ticket.Status != "answered" || len(v.Ticket.Thread) != 2 || !v.Ticket.Thread[1].Staff || v.Ticket.Email == "" {
		t.Fatalf("администратору: %+v", v)
	}
	v := decode[view](t, e.do("GET", base, nil, john))
	if v.Ticket.Status != "answered" || v.Ticket.Email != "" || v.Ticket.Thread[1].Author != "" || !v.Ticket.CanReply {
		t.Fatalf("владельцу: %+v", v)
	}
	if decode[struct{ Waiting int }](t, e.do("GET", "/api/tickets/summary", nil, john)).Waiting != 1 {
		t.Fatal("у владельца ждёт ответ")
	}
	if w := e.do("POST", base+"/messages", map[string]string{"message": "  "}, john); w.Code != 422 || decode[errBody](t, w).Error.Code != "validation.ticket_body" {
		t.Fatalf("пустое сообщение: %d %s", w.Code, w.Body)
	}
	if w := e.do("POST", base+"/messages", map[string]string{"message": "Помогло, спасибо"}, john); w.Code != 201 || decode[view](t, w).Ticket.Status != "open" {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if w := e.do("POST", base+"/close", nil, john); w.Code != 200 || decode[view](t, w).Ticket.Status != "closed" {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if w := e.do("POST", base+"/close", nil, john); w.Code != 409 {
		t.Fatalf("повторное закрытие: %d", w.Code)
	}
	if w := e.do("POST", base+"/reopen", nil, john); w.Code != 200 || decode[view](t, w).Ticket.Status != "open" {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	// итальянский пользователь получает сообщение об ошибке на своём языке
	if w := e.doLang("it", "POST", "/api/tickets", map[string]string{"subject": "ab", "category": "sites", "message": "x"}, john); !strings.Contains(w.Body.String(), "Oggetto") {
		t.Fatalf("%s", w.Body)
	}
	if w := e.do("GET", "/api/tickets/abc", nil, john); w.Code != 404 {
		t.Fatalf("плохой номер: %d", w.Code)
	}
}
