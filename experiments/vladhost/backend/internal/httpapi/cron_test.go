package httpapi_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"vladhost/internal/config"
	"vladhost/internal/cronjobs"
	"vladhost/internal/httpapi"
)

type cronJobView struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	Schedule  string `json:"schedule"`
	Enabled   bool   `json:"enabled"`
	NextRunAt string `json:"next_run_at"`
}

type cronList struct {
	Jobs []cronJobView `json:"jobs"`
	Info struct {
		MaxJobs         int  `json:"max_jobs"`
		MinIntervalMin  int  `json:"min_interval_min"`
		TimeoutSec      int  `json:"timeout_sec"`
		CommandsEnabled bool `json:"commands_enabled"`
	} `json:"info"`
}

func (e *env) withCron() *cronjobs.Service {
	e.t.Helper()
	svc := cronjobs.New(e.db, e.sites, cronjobs.Config{AllowPrivate: true, Timeout: 5 * time.Second})
	e.r = httpapi.New(e.svc, e.sites, config.Config{JWTSecret: []byte(strings.Repeat("s", 32)), AuthPerMinute: 10000, AuthBurst: 10000},
		httpapi.WithCron(svc))
	return svc
}

func TestCronHiddenWithoutService(t *testing.T) {
	e := newEnv(t)
	adm, _ := e.admin()
	tok := e.user(adm, "john")
	for _, tc := range [][2]string{{"GET", "/api/cron"}, {"POST", "/api/cron"}, {"DELETE", "/api/cron/1"}, {"POST", "/api/cron/1/run"}} {
		if w := e.do(tc[0], tc[1], nil, tok); w.Code != 404 {
			t.Errorf("%s %s: %d", tc[0], tc[1], w.Code)
		}
	}
}

func TestCronThroughAPI(t *testing.T) {
	pong := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = fmt.Fprint(w, "pong") }))
	defer pong.Close()
	e := newEnv(t)
	svc := e.withCron()
	adm, _ := e.admin()
	john, mary := e.user(adm, "john"), e.user(adm, "mary")

	if w := e.do("GET", "/api/cron", nil, ""); w.Code != 401 {
		t.Fatalf("без входа: %d", w.Code)
	}
	empty := decode[cronList](t, e.do("GET", "/api/cron", nil, john))
	if empty.Jobs == nil || len(empty.Jobs) != 0 || empty.Info.MaxJobs != 5 || empty.Info.MinIntervalMin != 5 || empty.Info.CommandsEnabled {
		t.Fatalf("пустой список: %+v", empty)
	}

	body := map[string]any{"name": "ping", "kind": "http", "schedule": "*/10 * * * *", "url": pong.URL, "enabled": true}
	w := e.do("POST", "/api/cron", body, john)
	if w.Code != 201 {
		t.Fatalf("создание: %d %s", w.Code, w.Body)
	}
	job := decode[struct{ Job cronJobView }](t, w).Job
	if job.Name != "ping" || !job.Enabled || job.NextRunAt == "" {
		t.Fatalf("задача: %+v", job)
	}

	// Ошибки на двух языках и привязка к полю.
	bad := map[string]any{"name": "x", "kind": "http", "schedule": "* * * * *", "url": pong.URL}
	w = e.doLang("ru", "POST", "/api/cron", bad, john)
	if w.Code != 422 || !strings.Contains(w.Body.String(), "не чаще раза в 5 мин") || !strings.Contains(w.Body.String(), `"field":"schedule"`) {
		t.Fatalf("ru: %d %s", w.Code, w.Body)
	}
	w = e.doLang("it", "POST", "/api/cron", bad, john)
	if w.Code != 422 || !strings.Contains(w.Body.String(), "una volta ogni 5 min") {
		t.Fatalf("it: %d %s", w.Code, w.Body)
	}
	w = e.do("POST", "/api/cron", map[string]any{"name": "c", "kind": "command", "schedule": "0 3 * * *", "command": "true", "site_id": 1}, john)
	if w.Code != 409 || !strings.Contains(w.Body.String(), "cron_commands_off") {
		t.Fatalf("команды без исполнителя: %d %s", w.Code, w.Body)
	}

	// Чужая задача недоступна во всех операциях.
	id := fmt.Sprint(job.ID)
	if w := e.do("GET", "/api/cron/"+id+"/runs", nil, mary); w.Code != 404 {
		t.Errorf("чужой журнал: %d", w.Code)
	}
	if w := e.do("POST", "/api/cron/"+id+"/run", nil, mary); w.Code != 404 {
		t.Errorf("чужой запуск: %d", w.Code)
	}
	if w := e.do("PUT", "/api/cron/"+id, body, mary); w.Code != 404 {
		t.Errorf("чужая правка: %d", w.Code)
	}
	if w := e.do("DELETE", "/api/cron/"+id, nil, mary); w.Code != 404 {
		t.Errorf("чужое удаление: %d", w.Code)
	}
	if got := decode[cronList](t, e.do("GET", "/api/cron", nil, mary)); len(got.Jobs) != 0 {
		t.Fatalf("список чужих задач: %+v", got)
	}

	// Ручной запуск попадает в журнал; повтор в ту же минуту — отказ.
	if w := e.do("POST", "/api/cron/"+id+"/run", nil, john); w.Code != 202 {
		t.Fatalf("запуск: %d %s", w.Code, w.Body)
	}
	svc.Wait()
	runs := decode[struct {
		Runs []struct {
			Status string `json:"status"`
			Code   int    `json:"code"`
			Output string `json:"output"`
		}
	}](t, e.do("GET", "/api/cron/"+id+"/runs", nil, john)).Runs
	if len(runs) != 1 || runs[0].Status != "ok" || runs[0].Code != 200 || !strings.Contains(runs[0].Output, "pong") {
		t.Fatalf("журнал: %+v", runs)
	}
	if w := e.do("POST", "/api/cron/"+id+"/run", nil, john); w.Code != 429 {
		t.Fatalf("повторный запуск: %d", w.Code)
	}

	// Правка и отключение.
	body["enabled"], body["schedule"] = false, "0 4 * * *"
	w = e.do("PUT", "/api/cron/"+id, body, john)
	upd := decode[struct{ Job cronJobView }](t, w).Job
	if w.Code != 200 || upd.Enabled || upd.NextRunAt != "" || upd.Schedule != "0 4 * * *" {
		t.Fatalf("правка: %d %s", w.Code, w.Body)
	}
	if w := e.do("DELETE", "/api/cron/"+id, nil, john); w.Code != 204 {
		t.Fatalf("удаление: %d", w.Code)
	}
	if w := e.do("GET", "/api/cron/"+id+"/runs", nil, john); w.Code != 404 {
		t.Fatalf("журнал удалённой: %d", w.Code)
	}
}
