package userdb

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

var bg = context.Background()

func randName(prefix string) string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return prefix + hex.EncodeToString(b)
}

// engineCase — общий набор проверок для обоих серверов: Backend обязан вести себя одинаково.
type engineCase struct {
	name string
	be   Backend
	// connect открывает соединение от имени учётной записи; закрывается сам.
	connect func(t *testing.T, user, pass, db string) (exec func(q string) error, query func(q string) (string, error), err error)
	hbaDir  string
	maria   *Maria
	pg      *PG
}

func pgCase(t *testing.T) *engineCase {
	t.Helper()
	admin := os.Getenv("VLADHOST_TEST_DATABASE_URL")
	if admin == "" {
		t.Fatal("VLADHOST_TEST_DATABASE_URL не задан (make db-up, затем make test-back)")
	}
	hba := t.TempDir()
	// Роль заморозки с уникальным префиксом: параллельные прогоны не пересекаются.
	be, err := NewPG(PGConfig{AdminURL: admin, HBADir: hba, FrozenRolePrefix: randName("frz") + "_"})
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(admin)
	return &engineCase{name: "postgres", be: be, hbaDir: hba, pg: be, connect: func(t *testing.T, user, pass, db string) (func(string) error, func(string) (string, error), error) {
		cu := url.URL{Scheme: "postgres", User: url.UserPassword(user, pass), Host: u.Host, Path: "/" + db, RawQuery: "sslmode=disable"}
		c, err := pgx.Connect(bg, cu.String())
		if err != nil {
			return nil, nil, err
		}
		t.Cleanup(func() { _ = c.Close(bg) })
		return func(q string) error { _, err := c.Exec(bg, q); return err },
			func(q string) (string, error) {
				var out string
				err := c.QueryRow(bg, q).Scan(&out)
				return out, err
			}, nil
	}}
}

// mariaAdminDSN — служебное подключение к MariaDB из docker-compose (root; на бою у панели своя учётная запись).
func mariaAdminDSN(t *testing.T) string {
	t.Helper()
	addr := os.Getenv("VLADHOST_TEST_MARIADB_ADDR")
	if addr == "" {
		t.Fatal("VLADHOST_TEST_MARIADB_ADDR не задан (make db-up, затем make test-back)")
	}
	return "root:vladhost@tcp(" + addr + ")/"
}

func mariaCase(t *testing.T) *engineCase {
	t.Helper()
	dsn := mariaAdminDSN(t)
	be, err := NewMaria(MariaConfig{AdminDSN: dsn, ExternalAccess: true, LocalHost: "%"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = be.Close() })
	hostport := dsn[strings.Index(dsn, "@")+1:]
	return &engineCase{name: "mariadb", be: be, maria: be, connect: func(t *testing.T, user, pass, db string) (func(string) error, func(string) (string, error), error) {
		d, err := sql.Open("mysql", fmt.Sprintf("%s:%s@%s%s", user, pass, hostport[:strings.Index(hostport, ")")+1], "/"+db))
		if err != nil {
			return nil, nil, err
		}
		if err := d.PingContext(bg); err != nil {
			_ = d.Close()
			return nil, nil, err
		}
		t.Cleanup(func() { _ = d.Close() })
		return func(q string) error { _, err := d.ExecContext(bg, q); return err },
			func(q string) (string, error) {
				var out sql.NullString
				err := d.QueryRowContext(bg, q).Scan(&out)
				return out.String, err
			}, nil
	}}
}

func forEachEngine(t *testing.T, fn func(t *testing.T, e *engineCase)) {
	t.Helper()
	for _, mk := range []func(*testing.T) *engineCase{pgCase, mariaCase} {
		e := mk(t)
		t.Run(e.name, func(t *testing.T) { fn(t, e) })
	}
}

// newDB создаёт базу и удаляет её после теста.
func (e *engineCase) newDB(t *testing.T, base string) (name, pass string, id int64) {
	t.Helper()
	name = randName(base)
	id = int64(time.Now().UnixNano() % 1_000_000_000)
	pass, _ = NewPassword()
	if err := e.be.Create(bg, name, pass); err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { _ = e.be.Drop(context.WithoutCancel(bg), name, id) })
	return
}

func must(t *testing.T, err error, what string) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s: %v", what, err)
	}
}

func TestCreateConnectAndWork(t *testing.T) {
	forEachEngine(t, func(t *testing.T, e *engineCase) {
		name, pass, _ := e.newDB(t, "w")
		exec, query, err := e.connect(t, name, pass, name)
		must(t, err, "подключение владельца")
		must(t, exec("CREATE TABLE t (id int, v text)"), "CREATE TABLE")
		must(t, exec("INSERT INTO t VALUES (1, 'привет')"), "INSERT")
		got, err := query("SELECT v FROM t WHERE id = 1")
		if err != nil || got != "привет" {
			t.Fatalf("SELECT: %q %v", got, err)
		}
		// Неверный пароль не подходит.
		if _, _, err := e.connect(t, name, pass+"x", name); err == nil {
			t.Fatal("вход с неверным паролем прошёл")
		}
		if err := e.be.Ping(bg); err != nil {
			t.Fatal(err)
		}
	})
}

func TestCreateFailsForExistingAndCleansUp(t *testing.T) {
	forEachEngine(t, func(t *testing.T, e *engineCase) {
		name, _, id := e.newDB(t, "dup")
		if err := e.be.Create(bg, name, "another-Pass-1234"); err == nil {
			t.Fatal("повторное создание должно отказать")
		}
		// Первая база не пострадала.
		must(t, e.be.Drop(bg, name, id), "Drop")
		must(t, e.be.Drop(bg, name, id), "повторный Drop не должен падать")
	})
}

func TestDatabasesAreIsolated(t *testing.T) {
	forEachEngine(t, func(t *testing.T, e *engineCase) {
		a, apass, _ := e.newDB(t, "isoa")
		b, bpass, _ := e.newDB(t, "isob")
		execA, _, err := e.connect(t, a, apass, a)
		must(t, err, "подключение A")
		must(t, execA("CREATE TABLE secret (v int)"), "таблица A")
		// B не может подключиться к базе A и войти в неё.
		if _, _, err := e.connect(t, b, bpass, a); err == nil {
			t.Fatal("пользователь B вошёл в базу A")
		}
		// A не видит базу B.
		if _, _, err := e.connect(t, a, apass, b); err == nil {
			t.Fatal("пользователь A вошёл в базу B")
		}
		if e.maria != nil {
			execB, _, err := e.connect(t, b, bpass, "")
			must(t, err, "B без базы")
			if err := execB("SELECT * FROM " + bt(a) + ".secret"); err == nil {
				t.Fatal("B прочитал таблицу A")
			}
		}
	})
}

// «_» в GRANT — маска: права на базу a_b не должны распространяться на axb.
func TestMariaGrantUnderscoreIsNotAWildcard(t *testing.T) {
	e := mariaCase(t)
	tag := randName("wc")[:6]
	real, other := tag+"_x1", tag+"Zx1" // real совпало бы с other, если бы «_» работало как маска
	rpass, _ := NewPassword()
	opass, _ := NewPassword()
	must(t, e.be.Create(bg, real, rpass), "Create real")
	t.Cleanup(func() { _ = e.be.Drop(context.WithoutCancel(bg), real, 0) })
	must(t, e.be.Create(bg, other, opass), "Create other")
	t.Cleanup(func() { _ = e.be.Drop(context.WithoutCancel(bg), other, 0) })
	execOther, _, err := e.connect(t, other, opass, other)
	must(t, err, "подключение other")
	must(t, execOther("CREATE TABLE private (v int)"), "таблица other")
	execReal, _, err := e.connect(t, real, rpass, "")
	must(t, err, "подключение real")
	if err := execReal("SELECT * FROM " + bt(other) + ".private"); err == nil {
		t.Fatal("права базы с «_» в имени распространились на соседнюю базу (маска в GRANT)")
	}
}

func TestHyphenatedUserNames(t *testing.T) {
	forEachEngine(t, func(t *testing.T, e *engineCase) {
		name := "my-user_" + randName("d")[:6]
		pass, _ := NewPassword()
		must(t, e.be.Create(bg, name, pass), "Create")
		t.Cleanup(func() { _ = e.be.Drop(context.WithoutCancel(bg), name, 0) })
		exec, _, err := e.connect(t, name, pass, name)
		must(t, err, "подключение")
		must(t, exec("CREATE TABLE t (v int)"), "работа с базой")
	})
}

func TestSetPassword(t *testing.T) {
	forEachEngine(t, func(t *testing.T, e *engineCase) {
		name, old, id := e.newDB(t, "pw")
		// С внешним адресом: пароль должен смениться у всех учётных записей базы.
		must(t, e.be.SetAccess(bg, name, id, []string{"203.0.113.9"}, false), "SetAccess")
		fresh, _ := NewPassword()
		must(t, e.be.SetPassword(bg, name, fresh), "SetPassword")
		if _, _, err := e.connect(t, name, old, name); err == nil {
			t.Fatal("старый пароль продолжает работать")
		}
		if _, _, err := e.connect(t, name, fresh, name); err != nil {
			t.Fatalf("новый пароль не подходит: %v", err)
		}
		if e.maria != nil {
			rows, err := e.maria.db.Query("SELECT Host, Password FROM mysql.user WHERE User = ?", name)
			must(t, err, "учётные записи")
			hashes := map[string]bool{}
			for rows.Next() {
				var h, p string
				_ = rows.Scan(&h, &p)
				hashes[p] = true
			}
			_ = rows.Close()
			if len(hashes) != 1 {
				t.Fatalf("у учётных записей базы разные пароли: %v", hashes)
			}
		}
	})
}

func TestSizeGrowsWithData(t *testing.T) {
	forEachEngine(t, func(t *testing.T, e *engineCase) {
		name, pass, _ := e.newDB(t, "sz")
		before, err := e.be.Size(bg, name)
		must(t, err, "Size")
		exec, _, err := e.connect(t, name, pass, name)
		must(t, err, "подключение")
		must(t, exec("CREATE TABLE big (v varchar(1000))"), "CREATE")
		gen := "INSERT INTO big SELECT repeat('x', 900) FROM generate_series(1, 3000)"
		if e.maria != nil {
			must(t, exec("CREATE TABLE seq (n int)"), "seq")
			must(t, exec("INSERT INTO seq VALUES (1),(2),(3),(4),(5),(6),(7),(8),(9),(10)"), "seq data")
			gen = "INSERT INTO big SELECT repeat('x', 900) FROM seq a, seq b, seq c, seq d"
		}
		must(t, exec(gen), "наполнение")
		after, err := e.be.Size(bg, name)
		must(t, err, "Size после")
		if after <= before || after < 1<<20 {
			t.Fatalf("размер не вырос: %d → %d", before, after)
		}
	})
}

func TestFreezeAndUnfreeze(t *testing.T) {
	forEachEngine(t, func(t *testing.T, e *engineCase) {
		name, pass, id := e.newDB(t, "frz")
		exec, _, err := e.connect(t, name, pass, name)
		must(t, err, "подключение")
		must(t, exec("CREATE TABLE t (id int, v text)"), "CREATE")
		must(t, exec("INSERT INTO t VALUES (1, 'a'), (2, 'b')"), "INSERT")
		must(t, e.be.Freeze(bg, name, id), "Freeze")

		// Заморозка обрывает старые сессии: подключаемся заново.
		exec, query, err := e.connect(t, name, pass, name)
		must(t, err, "подключение к замороженной базе")
		if got, err := query("SELECT count(*) FROM t"); err != nil || got != "2" {
			t.Fatalf("чтение замороженной базы: %q %v", got, err)
		}
		for _, q := range []string{
			"INSERT INTO t VALUES (3, 'c')", "UPDATE t SET v = 'z'", "CREATE TABLE t2 (x int)", "ALTER TABLE t ADD COLUMN w int",
		} {
			if err := exec(q); err == nil {
				t.Errorf("в замороженной базе прошло: %s", q)
			}
		}
		if e.pg != nil {
			// PostgreSQL: пользователь больше не владелец, права вернуть себе не может и схему создать не может.
			// GRANT без права выдачи в PostgreSQL даёт предупреждение, а не ошибку: важен результат.
			_ = exec("GRANT INSERT, UPDATE ON t TO " + ident(name))
			if err := exec("INSERT INTO t VALUES (9, 'x')"); err == nil {
				t.Error("PostgreSQL: пользователь вернул себе право записи командой GRANT")
			}
			for _, q := range []string{"CREATE SCHEMA mine", "ALTER TABLE t OWNER TO " + ident(name), "DROP TABLE t"} {
				if err := exec(q); err == nil {
					t.Errorf("PostgreSQL: обход заморозки: %s", q)
				}
			}
		}
		// Освободить место можно.
		must(t, exec("DELETE FROM t WHERE id = 2"), "DELETE в замороженной базе")
		if e.maria != nil {
			must(t, exec("DROP TABLE t"), "DROP TABLE в замороженной базе")
			must(t, exec("SELECT 1"), "SELECT 1")
		} else {
			must(t, exec("TRUNCATE t"), "TRUNCATE в замороженной базе")
		}

		must(t, e.be.Unfreeze(bg, name, id), "Unfreeze")
		exec, _, err = e.connect(t, name, pass, name)
		must(t, err, "подключение после разморозки")
		must(t, exec("CREATE TABLE after (x int)"), "CREATE после разморозки")
		must(t, exec("INSERT INTO after VALUES (1)"), "INSERT после разморозки")
		if e.pg != nil {
			// Владение вернулось: пользователь снова может менять свои таблицы, в том числе созданные до заморозки.
			must(t, exec("ALTER TABLE t ADD COLUMN w int"), "ALTER своей таблицы после разморозки")
		}
	})
}

func TestFreezeIsRepeatableAndUnfreezeOfActiveIsHarmless(t *testing.T) {
	forEachEngine(t, func(t *testing.T, e *engineCase) {
		name, pass, id := e.newDB(t, "rep")
		must(t, e.be.Unfreeze(bg, name, id), "Unfreeze активной базы")
		must(t, e.be.Freeze(bg, name, id), "Freeze")
		must(t, e.be.Freeze(bg, name, id), "повторный Freeze")
		must(t, e.be.Unfreeze(bg, name, id), "Unfreeze")
		must(t, e.be.Unfreeze(bg, name, id), "повторный Unfreeze")
		exec, _, err := e.connect(t, name, pass, name)
		must(t, err, "подключение")
		must(t, exec("CREATE TABLE ok (x int)"), "работа после циклов")
	})
}

func TestCompactReclaimsSpaceAfterDelete(t *testing.T) {
	forEachEngine(t, func(t *testing.T, e *engineCase) {
		name, pass, _ := e.newDB(t, "cmp")
		base, _ := e.be.Size(bg, name) // пустая база тоже занимает место (каталоги сервера)
		exec, _, err := e.connect(t, name, pass, name)
		must(t, err, "подключение")
		must(t, exec("CREATE TABLE big (v varchar(1000))"), "CREATE")
		gen := "INSERT INTO big SELECT repeat('y', 900) FROM generate_series(1, 4000)"
		if e.maria != nil {
			must(t, exec("CREATE TABLE seq (n int)"), "seq")
			must(t, exec("INSERT INTO seq VALUES (1),(2),(3),(4),(5),(6),(7),(8),(9),(10)"), "seq data")
			gen = "INSERT INTO big SELECT repeat('y', 900) FROM seq a, seq b, seq c, seq d"
		}
		must(t, exec(gen), "наполнение")
		full, _ := e.be.Size(bg, name)
		must(t, exec("DELETE FROM big"), "DELETE")
		must(t, e.be.Compact(bg, name), "Compact")
		small, err := e.be.Size(bg, name)
		must(t, err, "Size")
		if full-base < 2<<20 || small-base > (full-base)/4 {
			t.Fatalf("после удаления и Compact размер %d (пустая база %d, была заполнена до %d)", small, base, full)
		}
	})
}

func TestTempAccounts(t *testing.T) {
	forEachEngine(t, func(t *testing.T, e *engineCase) {
		name, pass, _ := e.newDB(t, "tmp")
		exec, _, err := e.connect(t, name, pass, name)
		must(t, err, "подключение владельца")
		must(t, exec("CREATE TABLE owned (v int)"), "CREATE")

		acct, tpass, err := e.be.TempAccount(bg, name, time.Now().Add(time.Hour), false)
		must(t, err, "TempAccount")
		if !strings.HasPrefix(acct, "tmp_") || len(tpass) < 20 {
			t.Fatalf("учётная запись: %q", acct)
		}
		texec, tquery, err := e.connect(t, acct, tpass, name)
		must(t, err, "вход временной записью")
		if got, err := tquery("SELECT count(*) FROM owned"); err != nil || got != "0" {
			t.Fatalf("временная запись видит таблицы базы: %q %v", got, err)
		}
		must(t, texec("CREATE TABLE made_in_web (v int)"), "создание таблицы в веб-клиенте")
		// Другая база временной записи недоступна.
		other, opass, _ := e.newDB(t, "tmpo")
		if _, _, err := e.connect(t, acct, tpass, other); err == nil {
			t.Fatal("временная запись открыла чужую базу")
		}
		_ = opass

		// Чужую учётную запись через DropTemp другой базы удалить нельзя.
		if err := e.be.DropTemp(bg, other, acct); err == nil {
			t.Fatal("DropTemp принял учётную запись другой базы")
		}
		must(t, e.be.DropTemp(bg, name, acct), "DropTemp")
		must(t, e.be.DropTemp(bg, name, acct), "повторный DropTemp")
		if _, _, err := e.connect(t, acct, tpass, name); err == nil {
			t.Fatal("удалённая временная запись продолжает входить")
		}
		// Созданное в веб-клиенте осталось у владельца базы.
		exec2, q2, err := e.connect(t, name, pass, name)
		must(t, err, "владелец после удаления временной записи")
		if got, err := q2("SELECT count(*) FROM made_in_web"); err != nil || got != "0" {
			t.Fatalf("таблица из веб-клиента пропала: %q %v", got, err)
		}
		must(t, exec2("INSERT INTO made_in_web VALUES (1)"), "владелец пишет в таблицу из веб-клиента")
	})
}

func TestTempAccountOfFrozenDatabaseCannotWrite(t *testing.T) {
	forEachEngine(t, func(t *testing.T, e *engineCase) {
		name, pass, id := e.newDB(t, "tfz")
		exec, _, err := e.connect(t, name, pass, name)
		must(t, err, "подключение")
		must(t, exec("CREATE TABLE t (v int)"), "CREATE")
		must(t, e.be.Freeze(bg, name, id), "Freeze")
		acct, tpass, err := e.be.TempAccount(bg, name, time.Now().Add(time.Hour), true)
		must(t, err, "TempAccount")
		texec, _, err := e.connect(t, acct, tpass, name)
		must(t, err, "вход")
		if err := texec("INSERT INTO t VALUES (1)"); err == nil {
			t.Fatal("веб-клиент замороженной базы позволил писать")
		}
		if err := texec("CREATE TABLE more (x int)"); err == nil {
			t.Fatal("веб-клиент замороженной базы позволил создать таблицу")
		}
		must(t, texec("DELETE FROM t"), "удаление данных разрешено")
	})
}

func TestDropRemovesEverything(t *testing.T) {
	forEachEngine(t, func(t *testing.T, e *engineCase) {
		name, pass, id := e.newDB(t, "drp")
		must(t, e.be.SetAccess(bg, name, id, []string{"203.0.113.7"}, false), "SetAccess")
		acct, tpass, err := e.be.TempAccount(bg, name, time.Now().Add(time.Hour), false)
		must(t, err, "TempAccount")
		// Открытая сессия не мешает удалению.
		_, _, err = e.connect(t, name, pass, name)
		must(t, err, "сессия владельца")
		must(t, e.be.Freeze(bg, name, id), "Freeze")
		must(t, e.be.Drop(bg, name, id), "Drop")
		for _, c := range [][2]string{{name, pass}, {acct, tpass}} {
			if _, _, err := e.connect(t, c[0], c[1], name); err == nil {
				t.Fatalf("учётная запись %s осталась после удаления базы", c[0])
			}
		}
		if e.pg != nil {
			if _, err := os.Stat(filepath.Join(e.hbaDir, fmt.Sprintf("db%d.conf", id))); err == nil {
				t.Fatal("правила pg_hba удалённой базы остались")
			}
			var n int
			must(t, e.pg.exec(bg, "", "SELECT 1"), "связь")
			conn, err := e.pg.connect(bg, "")
			must(t, err, "служебное подключение")
			defer func() { _ = conn.Close(bg) }()
			must(t, conn.QueryRow(bg, "SELECT count(*) FROM pg_roles WHERE rolname = $1 OR rolname = $2 OR rolname = $3", name, acct, e.pg.frozenRole(id)).Scan(&n), "роли")
			if n != 0 {
				t.Fatalf("после удаления остались роли: %d", n)
			}
			must(t, conn.QueryRow(bg, "SELECT count(*) FROM pg_database WHERE datname = $1", name).Scan(&n), "базы")
			if n != 0 {
				t.Fatal("база осталась")
			}
		} else {
			var n int
			must(t, e.maria.db.QueryRow("SELECT count(*) FROM mysql.user WHERE User IN (?, ?)", name, acct).Scan(&n), "учётные записи")
			if n != 0 {
				t.Fatalf("остались учётные записи: %d", n)
			}
		}
	})
}

func TestExternalAccessPostgresHBA(t *testing.T) {
	e := pgCase(t)
	name, _, id := e.newDB(t, "hba")
	file := filepath.Join(e.hbaDir, fmt.Sprintf("db%d.conf", id))
	must(t, e.be.SetAccess(bg, name, id, []string{"203.0.113.7", "198.51.100.0/24", "2001:db8::5"}, false), "SetAccess")
	data, err := os.ReadFile(file)
	must(t, err, "файл правил")
	want := []string{
		fmt.Sprintf(`hostssl "%s" "%s" 203.0.113.7/32 scram-sha-256`, name, name),
		fmt.Sprintf(`hostssl "%s" "%s" 198.51.100.0/24 scram-sha-256`, name, name),
		fmt.Sprintf(`hostssl "%s" "%s" 2001:db8::5/128 scram-sha-256`, name, name),
	}
	for _, w := range want {
		if !strings.Contains(string(data), w) {
			t.Errorf("нет правила %q в:\n%s", w, data)
		}
	}
	if strings.Contains(string(data), "\nhost ") || strings.Contains(string(data), "trust") || strings.Contains(string(data), "all") {
		t.Errorf("правила слишком широкие:\n%s", data)
	}
	fi, _ := os.Stat(file)
	if fi.Mode().Perm() != 0o640 {
		t.Errorf("права файла %v", fi.Mode())
	}
	// Замена списка целиком и закрытие доступа.
	must(t, e.be.SetAccess(bg, name, id, []string{"203.0.113.8"}, false), "SetAccess 2")
	data, _ = os.ReadFile(file)
	if strings.Contains(string(data), "203.0.113.7") || !strings.Contains(string(data), "203.0.113.8/32") {
		t.Errorf("список не заменился:\n%s", data)
	}
	must(t, e.be.SetAccess(bg, name, id, nil, false), "SetAccess пустой")
	if _, err := os.Stat(file); err == nil {
		t.Fatal("после закрытия доступа файл правил остался")
	}
	if ents, _ := os.ReadDir(e.hbaDir); len(ents) != 0 {
		t.Fatalf("остались временные файлы: %v", ents)
	}
}

func TestPostgresWithoutHBADirRefusesExternalAccess(t *testing.T) {
	admin := os.Getenv("VLADHOST_TEST_DATABASE_URL")
	be, err := NewPG(PGConfig{AdminURL: admin})
	must(t, err, "NewPG")
	if err := be.SetAccess(bg, "x", 1, []string{"203.0.113.7"}, false); err != ErrNoExternalAccess {
		t.Fatalf("%v", err)
	}
	m, err := NewMaria(MariaConfig{AdminDSN: mariaAdminDSN(t), LocalHost: "%"})
	must(t, err, "NewMaria")
	defer func() { _ = m.Close() }()
	if err := m.SetAccess(bg, "x", 1, []string{"203.0.113.7"}, false); err != ErrNoExternalAccess {
		t.Fatalf("MariaDB без внешнего доступа: %v", err)
	}
}

func TestExternalAccessMariaDBAccounts(t *testing.T) {
	e := mariaCase(t)
	name, pass, id := e.newDB(t, "ext")
	accounts := func() map[string]string {
		rows, err := e.maria.db.Query("SELECT Host, ssl_type FROM mysql.user WHERE User = ?", name)
		must(t, err, "учётные записи")
		defer func() { _ = rows.Close() }()
		out := map[string]string{}
		for rows.Next() {
			var h, s string
			_ = rows.Scan(&h, &s)
			out[h] = s
		}
		return out
	}
	must(t, e.be.SetAccess(bg, name, id, []string{"203.0.113.7", "198.51.100.0/24", "2001:db8::5"}, false), "SetAccess")
	got := accounts()
	for _, h := range []string{"%", "203.0.113.7", "198.51.100.0/255.255.255.0", "2001:db8::5"} {
		if _, ok := got[h]; !ok {
			t.Errorf("нет учётной записи для %q: %v", h, got)
		}
		if h != "%" && got[h] != "ANY" {
			t.Errorf("учётная запись %q без REQUIRE SSL: %q", h, got[h])
		}
	}
	// У внешних записей тот же пароль, что у основной.
	var same int
	must(t, e.maria.db.QueryRow("SELECT count(DISTINCT Password) FROM mysql.user WHERE User = ?", name).Scan(&same), "хеши")
	if same != 1 {
		t.Errorf("пароли внешних записей отличаются от основной: %d вариантов", same)
	}
	// Замена: старые адреса исчезают, новые появляются, основная запись остаётся.
	must(t, e.be.SetAccess(bg, name, id, []string{"203.0.113.8"}, false), "SetAccess 2")
	got = accounts()
	if len(got) != 2 || got["203.0.113.8"] != "ANY" {
		t.Errorf("после замены: %v", got)
	}
	if _, ok := got["203.0.113.7"]; ok {
		t.Error("прежний адрес остался")
	}
	// Повтор безопасен; закрытие доступа оставляет только основную.
	must(t, e.be.SetAccess(bg, name, id, []string{"203.0.113.8"}, false), "SetAccess повтор")
	must(t, e.be.SetAccess(bg, name, id, nil, false), "SetAccess пустой")
	if got = accounts(); len(got) != 1 {
		t.Errorf("после закрытия доступа: %v", got)
	}
	if _, _, err := e.connect(t, name, pass, name); err != nil {
		t.Fatalf("основная учётная запись перестала работать: %v", err)
	}
	// Новые внешние записи замороженной базы тоже без прав на запись.
	must(t, e.be.Freeze(bg, name, id), "Freeze")
	must(t, e.be.SetAccess(bg, name, id, []string{"203.0.113.9"}, true), "SetAccess при заморозке")
	rows, err := e.maria.db.Query("SHOW GRANTS FOR " + sq(name) + "@" + sq("203.0.113.9"))
	must(t, err, "права")
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var g string
		_ = rows.Scan(&g)
		if strings.Contains(g, "INSERT") || strings.Contains(g, "CREATE") {
			t.Errorf("внешняя запись замороженной базы с правом записи: %s", g)
		}
	}
}

func TestHostPatterns(t *testing.T) {
	for in, want := range map[string]string{
		"203.0.113.7": "203.0.113.7", "10.1.2.0/24": "10.1.2.0/255.255.255.0", "10.1.2.128/25": "10.1.2.128/255.255.255.128", "2001:db8::1": "2001:db8::1",
	} {
		if got, err := hostPattern(in); err != nil || got != want {
			t.Errorf("%s → %q %v, ожидали %q", in, got, err, want)
		}
	}
	if _, err := hostPattern("junk"); err == nil {
		t.Error("мусор принят")
	}
}

// Заморозка действует и на уже открытые соединения: MariaDB применяет права на базу к ним только после USE, а PostgreSQL держит
// права открытой транзакции. Без обрыва сессий пользователь с открытым соединением продолжил бы писать.
func TestFreezeCutsOpenSessionsOfOwnerAndTempAccounts(t *testing.T) {
	forEachEngine(t, func(t *testing.T, e *engineCase) {
		name, pass, id := e.newDB(t, "cut")
		owner, _, err := e.connect(t, name, pass, name)
		must(t, err, "подключение владельца")
		must(t, owner("CREATE TABLE t (v int)"), "CREATE")
		acct, tpass, err := e.be.TempAccount(bg, name, time.Now().Add(time.Hour), false)
		must(t, err, "TempAccount")
		web, _, err := e.connect(t, acct, tpass, name)
		must(t, err, "вход временной записью")
		must(t, web("INSERT INTO t VALUES (1)"), "запись до заморозки")

		must(t, e.be.Freeze(bg, name, id), "Freeze")
		if err := owner("INSERT INTO t VALUES (2)"); err == nil {
			t.Error("открытое соединение владельца продолжило писать после заморозки")
		}
		if err := web("INSERT INTO t VALUES (3)"); err == nil {
			t.Error("открытое соединение временной записи продолжило писать после заморозки")
		}
		// Временная запись, созданная ДО заморозки, при новом входе тоже только читает.
		web2, _, err := e.connect(t, acct, tpass, name)
		must(t, err, "новый вход временной записи")
		if err := web2("INSERT INTO t VALUES (4)"); err == nil {
			t.Error("временная запись, выданная до заморозки, сохранила право записи")
		}
		must(t, web2("DELETE FROM t"), "удаление данных разрешено")
	})
}
