package userdb

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gorm.io/gorm"

	"vladhost/internal/auth"
	"vladhost/internal/testdb"
)

type svcEnv struct {
	t    *testing.T
	svc  *Service
	db   *gorm.DB
	auth *auth.Service
	pg   *engineCase
	my   *engineCase
	now  atomic.Int64 // смещение «часов» службы в секундах
}

func newSvcEnv(t *testing.T, mutate func(*Config)) *svcEnv {
	t.Helper()
	db := testdb.Open(t)
	pg, my := pgCase(t), mariaCase(t)
	cfg := Config{
		PerEngine: 3, SizeLimit: 10 << 20, Host: "db.example.test", Ports: map[Engine]int{Postgres: 5433, MariaDB: 3306},
		External: map[Engine]bool{Postgres: true, MariaDB: true}, WebURL: "https://db.example.test",
		WebServers: map[Engine]string{Postgres: "127.0.0.1:5433", MariaDB: "localhost"}, WebTTL: time.Hour,
	}
	if mutate != nil {
		mutate(&cfg)
	}
	e := &svcEnv{t: t, db: db, pg: pg, my: my, auth: auth.NewService(db, []byte(strings.Repeat("s", 32)), time.Minute, time.Hour)}
	e.svc = NewService(db, map[Engine]Backend{Postgres: pg.be, MariaDB: my.be}, cfg)
	e.svc.now = func() time.Time { return time.Now().Add(time.Duration(e.now.Load()) * time.Second) }
	return e
}

// user заводит пользователя панели с уникальным именем.
func (e *svcEnv) user(prefix string) auth.User {
	e.t.Helper()
	name := randName(prefix)
	u, err := e.auth.CreateAdmin(bg, name+"@example.com", name, "password123")
	if err != nil {
		e.t.Fatal(err)
	}
	return *u
}

func (e *svcEnv) create(u auth.User, engine Engine, name string) (*Database, string) {
	e.t.Helper()
	d, pw, err := e.svc.Create(bg, u, engine, name)
	if err != nil {
		e.t.Fatalf("Create %s/%s: %v", engine, name, err)
	}
	e.t.Cleanup(func() { _ = e.svc.backends[engine].Drop(context.WithoutCancel(bg), d.Name, d.ID) })
	return d, pw
}

func (e *svcEnv) conn(d *Database, user, pass string) (func(string) error, func(string) (string, error), error) {
	if d.Engine == Postgres {
		return e.pg.connect(e.t, user, pass, d.Name)
	}
	return e.my.connect(e.t, user, pass, d.Name)
}

func TestServiceCreateWorksOnBothEngines(t *testing.T) {
	e := newSvcEnv(t, nil)
	u := e.user("cr")
	for _, eng := range []Engine{Postgres, MariaDB} {
		d, pw := e.create(u, eng, "blog")
		if d.Name != u.Username+"_blog" || d.Status != StatusActive || d.Engine != eng || d.Addrs == nil || len(pw) != 24 {
			t.Fatalf("%s: %+v", eng, d)
		}
		exec, _, err := e.conn(d, d.Name, pw)
		if err != nil {
			t.Fatalf("%s: подключение с выданным паролем: %v", eng, err)
		}
		if err := exec("CREATE TABLE t (v int)"); err != nil {
			t.Fatalf("%s: %v", eng, err)
		}
	}
	// То же имя у другого пользователя — другая база; тот же пользователь той же СУБД — отказ.
	other := e.user("cr2")
	e.create(other, Postgres, "blog")
	if _, _, err := e.svc.Create(bg, u, Postgres, "blog"); !errors.Is(err, ErrTaken) {
		t.Fatalf("повтор имени: %v", err)
	}
	list, _ := e.svc.List(bg, u.ID)
	if len(list) != 2 {
		t.Fatalf("список: %+v", list)
	}
}

func TestServiceCreateValidation(t *testing.T) {
	e := newSvcEnv(t, nil)
	u := e.user("va")
	for _, bad := range []string{"", "Blog", "my_db", "my-db", "a b", "имя", "x;drop database y", "a`b", strings.Repeat("a", 21)} {
		if _, _, err := e.svc.Create(bg, u, Postgres, bad); !errors.Is(err, ErrNameInvalid) {
			t.Errorf("имя %q: %v", bad, err)
		}
	}
	if _, _, err := e.svc.Create(bg, u, Engine("oracle"), "x"); !errors.Is(err, ErrEngineUnavailable) {
		t.Errorf("неизвестная СУБД: %v", err)
	}
	only := NewService(e.db, map[Engine]Backend{Postgres: e.pg.be}, Config{})
	if _, _, err := only.Create(bg, u, MariaDB, "x"); !errors.Is(err, ErrEngineUnavailable) {
		t.Errorf("выключенная СУБД: %v", err)
	}
	if n, _ := e.svc.List(bg, u.ID); len(n) != 0 {
		t.Fatalf("отказы оставили записи: %+v", n)
	}
}

func TestServiceLimitPerEngineAndFreedBySlotOnDelete(t *testing.T) {
	e := newSvcEnv(t, nil)
	u := e.user("li")
	var first *Database
	for _, n := range []string{"a", "b", "c"} {
		d, _ := e.create(u, Postgres, n)
		if first == nil {
			first = d
		}
	}
	if _, _, err := e.svc.Create(bg, u, Postgres, "d"); !errors.Is(err, ErrLimit) {
		t.Fatalf("лимит: %v", err)
	}
	// Лимит считается по СУБД отдельно.
	e.create(u, MariaDB, "a")
	// Удаление освобождает место.
	if err := e.svc.Delete(bg, u.ID, first.ID); err != nil {
		t.Fatal(err)
	}
	e.create(u, Postgres, "d")
}

func TestServiceLimitCannotBeRacedAround(t *testing.T) {
	e := newSvcEnv(t, nil)
	u := e.user("ra")
	var ok atomic.Int32
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Go(func() {
			d, _, err := e.svc.Create(bg, u, MariaDB, fmt.Sprintf("r%d", i))
			if err == nil {
				ok.Add(1)
				t.Cleanup(func() { _ = e.my.be.Drop(context.WithoutCancel(bg), d.Name, d.ID) })
			} else if !errors.Is(err, ErrLimit) {
				t.Errorf("неожиданная ошибка: %v", err)
			}
		})
	}
	wg.Wait()
	if ok.Load() != 3 {
		t.Fatalf("создано %d баз при лимите 3", ok.Load())
	}
	if n, _ := e.svc.List(bg, u.ID); len(n) != 3 {
		t.Fatalf("в списке %d", len(n))
	}
}

// failing подменяет операции сервера, чтобы проверить откаты.
type failing struct {
	Backend
	create, access, drop, temp bool
}

func (f *failing) Create(ctx context.Context, n, p string) error {
	if f.create {
		return errors.New("сервер отказал")
	}
	return f.Backend.Create(ctx, n, p)
}

func (f *failing) SetAccess(ctx context.Context, n string, id int64, a []string, fr bool) error {
	if f.access {
		return errors.New("сервер отказал")
	}
	return f.Backend.SetAccess(ctx, n, id, a, fr)
}

func (f *failing) Drop(ctx context.Context, n string, id int64) error {
	if f.drop {
		return errors.New("сервер отказал")
	}
	return f.Backend.Drop(ctx, n, id)
}

func (f *failing) TempAccount(ctx context.Context, n string, x time.Time, fr bool) (string, string, error) {
	if f.temp {
		return "", "", errors.New("сервер отказал")
	}
	return f.Backend.TempAccount(ctx, n, x, fr)
}

func TestServiceCreateRollsBackWhenServerFails(t *testing.T) {
	e := newSvcEnv(t, nil)
	f := &failing{Backend: e.pg.be, create: true}
	e.svc.backends[Postgres] = f
	u := e.user("rb")
	if _, _, err := e.svc.Create(bg, u, Postgres, "x"); err == nil {
		t.Fatal("сбой сервера не замечен")
	}
	if n, _ := e.svc.List(bg, u.ID); len(n) != 0 {
		t.Fatalf("после сбоя осталась запись: %+v", n)
	}
	// Слот не потерян: после починки база создаётся.
	f.create = false
	e.create(u, Postgres, "x")
}

func TestServiceOwnershipIsEnforced(t *testing.T) {
	e := newSvcEnv(t, nil)
	john, mary := e.user("jo"), e.user("ma")
	d, _ := e.create(john, Postgres, "secret")
	if _, err := e.svc.Get(bg, mary.ID, d.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get чужой: %v", err)
	}
	if err := e.svc.Delete(bg, mary.ID, d.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("Delete чужой: %v", err)
	}
	if _, _, err := e.svc.ResetPassword(bg, mary.ID, d.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("ResetPassword чужой: %v", err)
	}
	if _, err := e.svc.SetAddrs(bg, mary.ID, d.ID, []string{"203.0.113.5"}); !errors.Is(err, ErrNotFound) {
		t.Errorf("SetAddrs чужой: %v", err)
	}
	if _, err := e.svc.OpenWebClient(bg, mary.ID, d.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("OpenWebClient чужой: %v", err)
	}
	if _, err := e.svc.Check(bg, mary.ID, d.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("Check чужой: %v", err)
	}
	if list, _ := e.svc.List(bg, mary.ID); len(list) != 0 {
		t.Errorf("чужие базы в списке: %+v", list)
	}
	// База john цела.
	if _, err := e.svc.Get(bg, john.ID, d.ID); err != nil {
		t.Fatal(err)
	}
}

func TestServiceDeleteRemovesServerDatabaseAndKeepsRowOnFailure(t *testing.T) {
	e := newSvcEnv(t, nil)
	f := &failing{Backend: e.my.be}
	e.svc.backends[MariaDB] = f
	u := e.user("de")
	d, pw := e.create(u, MariaDB, "gone")
	f.drop = true
	if err := e.svc.Delete(bg, u.ID, d.ID); err == nil {
		t.Fatal("сбой сервера не замечен")
	}
	if _, err := e.svc.Get(bg, u.ID, d.ID); err != nil {
		t.Fatal("запись пропала, хотя база на сервере осталась")
	}
	f.drop = false
	if err := e.svc.Delete(bg, u.ID, d.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := e.conn(d, d.Name, pw); err == nil {
		t.Fatal("база осталась на сервере")
	}
	if _, err := e.svc.Get(bg, u.ID, d.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("запись осталась")
	}
	// Пользователь удалён из панели — его базы уходят из учёта каскадом.
	d2, _ := e.create(u, MariaDB, "cascade")
	if err := e.db.Exec("DELETE FROM users WHERE id = ?", u.ID).Error; err != nil {
		t.Fatal(err)
	}
	var n int64
	e.db.Model(&Database{}).Where("id = ?", d2.ID).Count(&n)
	if n != 0 {
		t.Fatal("учёт базы остался после удаления пользователя")
	}
}

func TestServiceResetPassword(t *testing.T) {
	e := newSvcEnv(t, nil)
	u := e.user("rp")
	for _, eng := range []Engine{Postgres, MariaDB} {
		d, old := e.create(u, eng, "pw")
		_, fresh, err := e.svc.ResetPassword(bg, u.ID, d.ID)
		if err != nil || fresh == old || len(fresh) != 24 {
			t.Fatalf("%s: %q %v", eng, fresh, err)
		}
		if _, _, err := e.conn(d, d.Name, old); err == nil {
			t.Errorf("%s: старый пароль работает", eng)
		}
		if _, _, err := e.conn(d, d.Name, fresh); err != nil {
			t.Errorf("%s: новый пароль не работает: %v", eng, err)
		}
	}
}

func TestServiceSetAddrs(t *testing.T) {
	e := newSvcEnv(t, nil)
	u := e.user("ad")
	d, _ := e.create(u, Postgres, "ext")
	got, err := e.svc.SetAddrs(bg, u.ID, d.ID, []string{"203.0.113.9", " 203.0.113.5 ", "203.0.113.5/32", "198.51.100.77/24"})
	if err != nil || strings.Join(got.Addrs, ",") != "198.51.100.0/24,203.0.113.5,203.0.113.9" {
		t.Fatalf("%+v %v", got, err)
	}
	if again, _ := e.svc.Get(bg, u.ID, d.ID); strings.Join(again.Addrs, ",") != strings.Join(got.Addrs, ",") {
		t.Fatalf("адреса не сохранились: %+v", again.Addrs)
	}
	// Файл pg_hba записан.
	files, _ := readDir(e.pg.hbaDir)
	if len(files) != 1 {
		t.Fatalf("файлов правил %v", files)
	}
	// Ошибки не меняют сохранённый список.
	for name, in := range map[string][]string{"мусор": {"junk"}, "широкая сеть": {"10.0.0.0/8"}, "loopback": {"127.0.0.1"}} {
		if _, err := e.svc.SetAddrs(bg, u.ID, d.ID, in); !errors.Is(err, ErrAddrInvalid) {
			t.Errorf("%s: %v", name, err)
		}
	}
	var many []string
	for i := 1; i <= MaxIPs+1; i++ {
		many = append(many, fmt.Sprintf("203.0.113.%d", i))
	}
	if _, err := e.svc.SetAddrs(bg, u.ID, d.ID, many); !errors.Is(err, ErrAddrLimit) {
		t.Errorf("слишком много: %v", err)
	}
	if again, _ := e.svc.Get(bg, u.ID, d.ID); len(again.Addrs) != 3 {
		t.Fatalf("список изменился после отказов: %v", again.Addrs)
	}
	// Закрытие доступа.
	cleared, err := e.svc.SetAddrs(bg, u.ID, d.ID, nil)
	if err != nil || len(cleared.Addrs) != 0 || cleared.Addrs == nil {
		t.Fatalf("%+v %v", cleared, err)
	}
	if files, _ = readDir(e.pg.hbaDir); len(files) != 0 {
		t.Fatalf("правила остались: %v", files)
	}
}

func TestServiceSetAddrsRollsBackOnServerFailure(t *testing.T) {
	e := newSvcEnv(t, nil)
	f := &failing{Backend: e.pg.be}
	e.svc.backends[Postgres] = f
	u := e.user("ar")
	d, _ := e.create(u, Postgres, "x")
	if _, err := e.svc.SetAddrs(bg, u.ID, d.ID, []string{"203.0.113.5"}); err != nil {
		t.Fatal(err)
	}
	f.access = true
	if _, err := e.svc.SetAddrs(bg, u.ID, d.ID, []string{"203.0.113.6"}); err == nil {
		t.Fatal("сбой сервера не замечен")
	}
	if got, _ := e.svc.Get(bg, u.ID, d.ID); len(got.Addrs) != 1 || got.Addrs[0] != "203.0.113.5" {
		t.Fatalf("список в панели изменился, хотя сервер отказал: %v", got.Addrs)
	}
}

func TestServiceExternalAccessCanBeDisabledPerEngine(t *testing.T) {
	e := newSvcEnv(t, func(c *Config) { c.External = map[Engine]bool{Postgres: false, MariaDB: true} })
	u := e.user("nx")
	pg, _ := e.create(u, Postgres, "a")
	if _, err := e.svc.SetAddrs(bg, u.ID, pg.ID, []string{"203.0.113.5"}); !errors.Is(err, ErrExternalOff) {
		t.Fatalf("%v", err)
	}
	my, _ := e.create(u, MariaDB, "a")
	if _, err := e.svc.SetAddrs(bg, u.ID, my.ID, []string{"203.0.113.5"}); err != nil {
		t.Fatalf("MariaDB: %v", err)
	}
	info := e.svc.Info()
	if info.External[Postgres] || !info.External[MariaDB] || len(info.Engines) != 2 || info.PerEngine != 3 || info.SizeLimit != 10<<20 || info.MaxAddrs != MaxIPs {
		t.Fatalf("%+v", info)
	}
}

func TestServiceWebClientTokensAreOneTimeAndShortLived(t *testing.T) {
	e := newSvcEnv(t, nil)
	u := e.user("wc")
	for _, eng := range []Engine{Postgres, MariaDB} {
		d, pw := e.create(u, eng, "web")
		link, err := e.svc.OpenWebClient(bg, u.ID, d.ID)
		if err != nil || !strings.HasPrefix(link, "https://db.example.test/?vhtoken=") {
			t.Fatalf("%s: %q %v", eng, link, err)
		}
		token := strings.TrimPrefix(link, "https://db.example.test/?vhtoken=")
		sess, err := e.svc.Redeem(token)
		if err != nil {
			t.Fatalf("%s: %v", eng, err)
		}
		wantDriver := map[Engine]string{Postgres: "pgsql", MariaDB: "server"}[eng]
		if sess.Driver != wantDriver || sess.DB != d.Name || sess.Password == pw || sess.Username == d.Name || !strings.HasPrefix(sess.Username, "tmp_") {
			t.Fatalf("%s: сессия %+v (пароль базы веб-клиенту передаваться не должен)", eng, sess)
		}
		if sess.Server != e.svc.cfg.WebServers[eng] {
			t.Errorf("%s: сервер %q", eng, sess.Server)
		}
		if _, _, err := e.conn(d, sess.Username, sess.Password); err != nil {
			t.Fatalf("%s: временная запись не входит: %v", eng, err)
		}
		if _, err := e.svc.Redeem(token); !errors.Is(err, ErrBadSession) {
			t.Fatalf("%s: токен сработал дважды: %v", eng, err)
		}
		// Токен хранится хешем.
		for k := range e.svc.tokens {
			if k == token {
				t.Fatal("токен лежит открытым текстом")
			}
		}
	}
	for _, bad := range []string{"", "nope", strings.Repeat("a", 100)} {
		if _, err := e.svc.Redeem(bad); !errors.Is(err, ErrBadSession) {
			t.Errorf("токен %q: %v", bad, err)
		}
	}
	// Токен, который не успели использовать, протухает.
	d, _ := e.create(u, Postgres, "late")
	link, _ := e.svc.OpenWebClient(bg, u.ID, d.ID)
	e.now.Add(3 * 60)
	if _, err := e.svc.Redeem(strings.TrimPrefix(link, "https://db.example.test/?vhtoken=")); !errors.Is(err, ErrBadSession) {
		t.Fatalf("просроченный токен: %v", err)
	}
}

func TestServiceWebClientDisabledOrFailing(t *testing.T) {
	off := newSvcEnv(t, func(c *Config) { c.WebURL = "" })
	u := off.user("wo")
	d, _ := off.create(u, Postgres, "a")
	if _, err := off.svc.OpenWebClient(bg, u.ID, d.ID); !errors.Is(err, ErrWebOff) {
		t.Fatalf("%v", err)
	}
	if off.svc.Info().WebClient {
		t.Fatal("Info: веб-клиента нет")
	}
	e := newSvcEnv(t, nil)
	f := &failing{Backend: e.pg.be, temp: true}
	e.svc.backends[Postgres] = f
	u2 := e.user("wf")
	d2, _ := e.create(u2, Postgres, "a")
	if _, err := e.svc.OpenWebClient(bg, u2.ID, d2.ID); err == nil {
		t.Fatal("сбой сервера не замечен")
	}
	var n int64
	e.db.Model(&tempAccount{}).Count(&n)
	if n != 0 {
		t.Fatal("после сбоя осталась запись о временной учётной записи")
	}
}

func TestServiceCleanupDropsExpiredTemporaryAccounts(t *testing.T) {
	e := newSvcEnv(t, nil)
	u := e.user("cl")
	var sessions []Session
	var dbs []*Database
	for _, eng := range []Engine{Postgres, MariaDB} {
		d, _ := e.create(u, eng, "tmp")
		link, err := e.svc.OpenWebClient(bg, u.ID, d.ID)
		if err != nil {
			t.Fatal(err)
		}
		s, _ := e.svc.Redeem(strings.TrimPrefix(link, "https://db.example.test/?vhtoken="))
		sessions, dbs = append(sessions, s), append(dbs, d)
	}
	if err := e.svc.Cleanup(bg); err != nil {
		t.Fatal(err)
	}
	for i, d := range dbs {
		if _, _, err := e.conn(d, sessions[i].Username, sessions[i].Password); err != nil {
			t.Fatalf("действующая временная запись удалена раньше срока: %v", err)
		}
	}
	e.now.Add(int64(2 * time.Hour / time.Second))
	if err := e.svc.Cleanup(bg); err != nil {
		t.Fatal(err)
	}
	for i, d := range dbs {
		if _, _, err := e.conn(d, sessions[i].Username, sessions[i].Password); err == nil {
			t.Errorf("%s: временная запись пережила срок", d.Engine)
		}
	}
	var n int64
	e.db.Model(&tempAccount{}).Count(&n)
	if n != 0 {
		t.Fatalf("записей о временных учётных записях: %d", n)
	}
}

// fill наполняет базу данными примерно на mb мегабайт.
func (e *svcEnv) fill(d *Database, user, pass string, mb int) {
	e.t.Helper()
	exec, _, err := e.conn(d, user, pass)
	if err != nil {
		e.t.Fatal(err)
	}
	if err := exec("CREATE TABLE IF NOT EXISTS bulk (v varchar(1000))"); err != nil {
		e.t.Fatal(err)
	}
	rows := mb * 1100
	q := fmt.Sprintf("INSERT INTO bulk SELECT repeat('z', 900) FROM generate_series(1, %d)", rows)
	if d.Engine == MariaDB {
		_ = exec("CREATE TABLE IF NOT EXISTS seq (n int)")
		_ = exec("DELETE FROM seq")
		if err := exec("INSERT INTO seq VALUES (1),(2),(3),(4),(5),(6),(7),(8),(9),(10)"); err != nil {
			e.t.Fatal(err)
		}
		q = fmt.Sprintf("INSERT INTO bulk SELECT repeat('z', 900) FROM seq a, seq b, seq c, seq d LIMIT %d", rows)
	}
	if err := exec(q); err != nil {
		e.t.Fatal(err)
	}
}

func TestServiceFreezesOversizedDatabaseAndUnfreezesAfterCleanup(t *testing.T) {
	e := newSvcEnv(t, nil) // лимит 10 МБ
	u := e.user("fz")
	for _, eng := range []Engine{Postgres, MariaDB} {
		d, pw := e.create(u, eng, "big")
		if err := e.svc.CheckSizes(bg); err != nil {
			t.Fatal(err)
		}
		if got, _ := e.svc.Get(bg, u.ID, d.ID); got.Status != StatusActive || got.SizeCheckedAt == nil {
			t.Fatalf("%s: пустая база: %+v", eng, got)
		}
		e.fill(d, d.Name, pw, 12)
		if err := e.svc.CheckSizes(bg); err != nil {
			t.Fatal(err)
		}
		got, _ := e.svc.Get(bg, u.ID, d.ID)
		if got.Status != StatusFrozen || got.FrozenAt == nil || got.SizeBytes <= 10<<20 {
			t.Fatalf("%s: база выше лимита не заморожена: %+v", eng, got)
		}
		exec, _, err := e.conn(d, d.Name, pw)
		if err != nil {
			t.Fatal(err)
		}
		if err := exec("INSERT INTO bulk VALUES ('x')"); err == nil {
			t.Fatalf("%s: запись в замороженную базу прошла", eng)
		}
		// Повторная проверка ничего не ломает, пока данные на месте.
		if err := e.svc.CheckSizes(bg); err != nil {
			t.Fatal(err)
		}
		if got, _ = e.svc.Get(bg, u.ID, d.ID); got.Status != StatusFrozen {
			t.Fatalf("%s: заморозка слетела: %+v", eng, got)
		}
		// Пользователь освободил место: «Проверить» сжимает базу и размораживает её.
		if err := exec("DELETE FROM bulk"); err != nil {
			t.Fatal(err)
		}
		got, err = e.svc.Check(bg, u.ID, d.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.Status != StatusActive || got.FrozenAt != nil || got.SizeBytes > 9<<20 {
			t.Fatalf("%s: база не разморожена после очистки: %+v", eng, got)
		}
		exec2, _, err := e.conn(d, d.Name, pw)
		if err != nil {
			t.Fatal(err)
		}
		if err := exec2("INSERT INTO bulk VALUES ('again')"); err != nil {
			t.Fatalf("%s: запись после разморозки: %v", eng, err)
		}
	}
}

func TestServiceKeepsFrozenBetweenThresholds(t *testing.T) {
	e := newSvcEnv(t, nil)
	u := e.user("hy")
	d, pw := e.create(u, MariaDB, "edge")
	e.fill(d, d.Name, pw, 12)
	_ = e.svc.CheckSizes(bg)
	// Лимит поднят так, что база помещается, но выше порога разморозки (0,9): остаётся замороженной, чтобы не «дребезжать».
	size, _ := e.my.be.Size(bg, d.Name)
	e.svc.cfg.SizeLimit = size + size/20
	e.svc.compacts[d.ID] = time.Now() // сжатие только что было: иначе оно уменьшило бы базу и оправдало разморозку
	_ = e.svc.CheckSizes(bg)
	if got, _ := e.svc.Get(bg, u.ID, d.ID); got.Status != StatusFrozen {
		t.Fatalf("между порогами база должна оставаться замороженной: %+v", got)
	}
	e.svc.cfg.SizeLimit = size * 2
	_ = e.svc.CheckSizes(bg)
	if got, _ := e.svc.Get(bg, u.ID, d.ID); got.Status != StatusActive {
		t.Fatalf("при запасе больше 10%% база должна разморозиться: %+v", got)
	}
}

func TestServiceFrozenDatabaseGivesReadOnlyWebClient(t *testing.T) {
	e := newSvcEnv(t, nil)
	u := e.user("fw")
	d, pw := e.create(u, Postgres, "ro")
	e.fill(d, d.Name, pw, 12)
	_ = e.svc.CheckSizes(bg)
	link, err := e.svc.OpenWebClient(bg, u.ID, d.ID)
	if err != nil {
		t.Fatal(err)
	}
	sess, _ := e.svc.Redeem(strings.TrimPrefix(link, "https://db.example.test/?vhtoken="))
	exec, _, err := e.conn(d, sess.Username, sess.Password)
	if err != nil {
		t.Fatal(err)
	}
	if err := exec("INSERT INTO bulk VALUES ('x')"); err == nil {
		t.Fatal("веб-клиент замороженной базы позволил писать")
	}
}

func TestServiceSkipsDatabasesOfDisabledEngineAndHandlesMissingRows(t *testing.T) {
	e := newSvcEnv(t, nil)
	u := e.user("sk")
	d, _ := e.create(u, MariaDB, "x")
	only := NewService(e.db, map[Engine]Backend{Postgres: e.pg.be}, Config{})
	if err := only.CheckSizes(bg); err != nil {
		t.Fatalf("база выключенной СУБД не должна ломать проверку: %v", err)
	}
	if err := only.Cleanup(bg); err != nil {
		t.Fatal(err)
	}
	if got, _ := only.Get(bg, u.ID, d.ID); got == nil {
		t.Fatal("запись пропала")
	}
	if (&Service{}).Enabled() || (*Service)(nil).Enabled() {
		t.Fatal("пустая служба должна быть выключенной")
	}
}

func readDir(dir string) ([]string, error) {
	ents, err := osReadDir(dir)
	return append([]string(nil), ents...), err
}

func osReadDir(dir string) ([]string, error) {
	ents, err := os.ReadDir(dir)
	var out []string
	for _, e := range ents {
		out = append(out, e.Name())
	}
	return out, err
}
