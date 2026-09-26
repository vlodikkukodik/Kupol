package userdb

import (
	"context"
	"database/sql"
	"fmt"
	"net/netip"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
)

// MariaConfig — подключение к серверу MariaDB для пользовательских баз.
type MariaConfig struct {
	// AdminDSN — служебная учётная запись панели, например "vhadmin:пароль@tcp(127.0.0.1:3306)/".
	AdminDSN string
	// ExternalAccess — принимает ли сервер подключения снаружи (настроены TLS и адрес прослушивания); без этого адреса не добавляются.
	ExternalAccess bool
	// LocalHost — хост основной и временных учётных записей: тот, с которого сервер видит панель и веб-клиент. По умолчанию
	// «localhost» (соединение по сокету). Тестам в Docker приходится задавать «%»: клиент виден с адреса моста.
	LocalHost string
}

// Maria — Backend для MariaDB.
type Maria struct {
	cfg MariaConfig
	db  *sql.DB
}

// NewMaria открывает пул соединений со служебной учётной записью.
func NewMaria(cfg MariaConfig) (*Maria, error) {
	c, err := mysql.ParseDSN(cfg.AdminDSN)
	if err != nil {
		return nil, fmt.Errorf("userdb: DSN MariaDB: %w", err)
	}
	if cfg.LocalHost == "" {
		cfg.LocalHost = "localhost"
	}
	c.MultiStatements = false
	c.InterpolateParams = false
	c.ParseTime = true
	c.Timeout = 10 * time.Second
	c.ReadTimeout = 5 * time.Minute // OPTIMIZE TABLE у больших таблиц долгий
	c.WriteTimeout = time.Minute
	db, err := sql.Open("mysql", c.FormatDSN())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(4)
	db.SetConnMaxLifetime(5 * time.Minute)
	return &Maria{cfg: cfg, db: db}, nil
}

// Close закрывает пул.
func (m *Maria) Close() error { return m.db.Close() }

func (m *Maria) Ping(ctx context.Context) error { return m.db.PingContext(ctx) }

// bt — идентификатор в обратных кавычках.
func bt(s string) string { return "`" + strings.ReplaceAll(s, "`", "``") + "`" }

// sq — строковый литерал (MariaDB по умолчанию понимает обратный слэш как экранирование).
func sq(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// grantDB — имя базы для GRANT/REVOKE: «_» и «%» в GRANT — маски, их обязательно экранируют. Иначе права на базу a_b
// распространились бы на axb (и на любую другую базу под этот шаблон).
func grantDB(name string) string {
	r := strings.NewReplacer(`\`, `\\`, `_`, `\_`, `%`, `\%`)
	return bt(r.Replace(name))
}

// Полный набор прав пользователя на свою базу; GRANT OPTION, FILE и глобальные права не выдаются никогда.
const fullPrivs = "SELECT, INSERT, UPDATE, DELETE, CREATE, DROP, ALTER, INDEX, REFERENCES, CREATE TEMPORARY TABLES, LOCK TABLES, " +
	"CREATE VIEW, SHOW VIEW, CREATE ROUTINE, ALTER ROUTINE, EXECUTE, TRIGGER"

// Права замороженной базы: читать, удалять данные и таблицы (освободить место), больше ничего.
const frozenPrivs = "SELECT, DELETE, DROP"

func privs(frozen bool) string {
	if frozen {
		return frozenPrivs
	}
	return fullPrivs
}

// hostPattern превращает разрешённый адрес в часть учётной записи 'имя'@'хост'.
func hostPattern(addr string) (string, error) {
	if strings.Contains(addr, "/") {
		p, err := netip.ParsePrefix(addr)
		if err != nil || !p.Addr().Is4() {
			return "", ErrBadAddr
		}
		mask := ^uint32(0) << (32 - p.Bits())
		return fmt.Sprintf("%s/%d.%d.%d.%d", p.Addr(), byte(mask>>24), byte(mask>>16), byte(mask>>8), byte(mask)), nil
	}
	if _, err := netip.ParseAddr(addr); err != nil {
		return "", ErrBadAddr
	}
	return addr, nil
}

func (m *Maria) exec(ctx context.Context, stmts ...string) error {
	for _, s := range stmts {
		if _, err := m.db.ExecContext(ctx, s); err != nil {
			return err
		}
	}
	return nil
}

// hosts возвращает хосты учётной записи имени.
func (m *Maria) hosts(ctx context.Context, user string) ([]string, error) {
	rows, err := m.db.QueryContext(ctx, "SELECT Host FROM mysql.user WHERE User = ?", user)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var h string
		if err := rows.Scan(&h); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

func (m *Maria) passwordHash(ctx context.Context, user string) (string, error) {
	var h string
	err := m.db.QueryRowContext(ctx, "SELECT Password FROM mysql.user WHERE User = ? AND Host = ?", user, m.cfg.LocalHost).Scan(&h)
	return h, err
}

func (m *Maria) grant(ctx context.Context, name, user, host string, frozen bool) error {
	return m.exec(ctx,
		"GRANT "+privs(frozen)+" ON "+grantDB(name)+".* TO "+sq(user)+"@"+sq(host),
	)
}

func (m *Maria) Create(ctx context.Context, name, password string) error {
	if err := m.exec(ctx, "CREATE DATABASE "+bt(name)+" CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci"); err != nil {
		return err
	}
	// Основная учётная запись — localhost: ею пользуется сервер (например, приложение на этом же VPS); внешние адреса добавляются
	// отдельными записями с тем же паролем.
	if err := m.exec(ctx, "CREATE USER "+sq(name)+"@"+sq(m.cfg.LocalHost)+" IDENTIFIED BY "+sq(password)+" WITH MAX_USER_CONNECTIONS 10"); err != nil {
		_ = m.exec(context.WithoutCancel(ctx), "DROP DATABASE IF EXISTS "+bt(name))
		return err
	}
	if err := m.grant(ctx, name, name, m.cfg.LocalHost, false); err != nil {
		_ = m.Drop(context.WithoutCancel(ctx), name, 0)
		return err
	}
	return nil
}

func (m *Maria) Drop(ctx context.Context, name string, _ int64) error {
	hosts, err := m.hosts(ctx, name)
	if err != nil {
		return err
	}
	if err := m.exec(ctx, "DROP DATABASE IF EXISTS "+bt(name)); err != nil {
		return err
	}
	for _, h := range hosts {
		if err := m.exec(ctx, "DROP USER IF EXISTS "+sq(name)+"@"+sq(h)); err != nil {
			return err
		}
	}
	// Временные учётные записи веб-клиента этой базы.
	temps, err := m.tempUsers(ctx, name)
	if err != nil {
		return err
	}
	for _, u := range temps {
		if err := m.exec(ctx, "DROP USER IF EXISTS "+sq(u)+"@"+sq(m.cfg.LocalHost)); err != nil {
			return err
		}
	}
	return nil
}

func (m *Maria) SetPassword(ctx context.Context, name, password string) error {
	hosts, err := m.hosts(ctx, name)
	if err != nil {
		return err
	}
	for _, h := range hosts {
		if err := m.exec(ctx, "ALTER USER "+sq(name)+"@"+sq(h)+" IDENTIFIED BY "+sq(password)); err != nil {
			return err
		}
	}
	return nil
}

// spaceName — имя базы в названиях табличных пространств InnoDB: служебные символы кодируются как @00xx («-» → @002d).
func spaceName(db string) string {
	var b strings.Builder
	for _, c := range []byte(db) {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '_':
			b.WriteByte(c)
		default:
			fmt.Fprintf(&b, "@%04x", c)
		}
	}
	return b.String()
}

// Size — занятое базой место. Статистика information_schema.tables у InnoDB обновляется с задержкой (после вставки миллиона строк
// она может показывать килобайты), поэтому берётся большее из неё и реального размера файлов табличных пространств.
func (m *Maria) Size(ctx context.Context, name string) (int64, error) {
	var stats sql.NullInt64
	if err := m.db.QueryRowContext(ctx,
		"SELECT SUM(data_length + index_length) FROM information_schema.tables WHERE table_schema = ?", name).Scan(&stats); err != nil {
		return 0, err
	}
	var files sql.NullInt64
	like := strings.NewReplacer(`\`, `\\`, `_`, `\_`, `%`, `\%`).Replace(spaceName(name)) + "/%"
	if err := m.db.QueryRowContext(ctx,
		"SELECT SUM(allocated_size) FROM information_schema.innodb_sys_tablespaces WHERE name LIKE ?", like).Scan(&files); err != nil {
		return 0, err
	}
	return max(stats.Int64, files.Int64), nil
}

// tempUsers — временные учётные записи веб-клиента этой базы.
func (m *Maria) tempUsers(ctx context.Context, name string) ([]string, error) {
	rows, err := m.db.QueryContext(ctx, "SELECT DISTINCT User FROM mysql.user WHERE User LIKE ?", "tmp\\_"+tempTag(name)+"\\_%")
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var u string
		if err := rows.Scan(&u); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// kill обрывает все открытые соединения учётной записи. Права на базу в MariaDB применяются к уже открытым соединениям только
// после следующего USE, поэтому без этого пользователь с открытым соединением продолжил бы писать после заморозки.
func (m *Maria) kill(ctx context.Context, user string) {
	rows, err := m.db.QueryContext(ctx, "SELECT id FROM information_schema.processlist WHERE user = ?", user)
	if err != nil {
		return
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	_ = rows.Err() // неполный список только оставит часть сессий: их обрежет следующая проверка
	_ = rows.Close()
	for _, id := range ids {
		_ = m.exec(ctx, fmt.Sprintf("KILL %d", id))
	}
}

// setPrivs заменяет права всех учётных записей базы (основной, внешних адресов и временных веб-клиента) и обрывает их сессии.
func (m *Maria) setPrivs(ctx context.Context, name string, frozen bool) error {
	hosts, err := m.hosts(ctx, name)
	if err != nil {
		return err
	}
	for _, h := range hosts {
		if err := m.exec(ctx,
			"REVOKE ALL PRIVILEGES, GRANT OPTION FROM "+sq(name)+"@"+sq(h),
			"GRANT "+privs(frozen)+" ON "+grantDB(name)+".* TO "+sq(name)+"@"+sq(h),
		); err != nil {
			return err
		}
	}
	temps, err := m.tempUsers(ctx, name)
	if err != nil {
		return err
	}
	for _, u := range temps {
		if err := m.exec(ctx,
			"REVOKE ALL PRIVILEGES, GRANT OPTION FROM "+sq(u)+"@"+sq(m.cfg.LocalHost),
			"GRANT "+privs(frozen)+" ON "+grantDB(name)+".* TO "+sq(u)+"@"+sq(m.cfg.LocalHost),
		); err != nil {
			return err
		}
	}
	for _, u := range append([]string{name}, temps...) {
		m.kill(ctx, u)
	}
	return nil
}

func (m *Maria) Freeze(ctx context.Context, name string, _ int64) error {
	return m.setPrivs(ctx, name, true)
}

func (m *Maria) Unfreeze(ctx context.Context, name string, _ int64) error {
	return m.setPrivs(ctx, name, false)
}

func (m *Maria) Compact(ctx context.Context, name string) error {
	rows, err := m.db.QueryContext(ctx, "SELECT table_name FROM information_schema.tables WHERE table_schema = ? AND table_type = 'BASE TABLE' LIMIT 500", name)
	if err != nil {
		return err
	}
	var tables []string
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			_ = rows.Close()
			return err
		}
		tables = append(tables, t)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	_ = rows.Close()
	for _, t := range tables {
		// OPTIMIZE возвращает результат-таблицу, поэтому Query, а не Exec.
		r, err := m.db.QueryContext(ctx, "OPTIMIZE TABLE "+bt(name)+"."+bt(t))
		if err != nil {
			return err
		}
		_ = r.Close()
	}
	return nil
}

func (m *Maria) SetAccess(ctx context.Context, name string, _ int64, addrs []string, frozen bool) error {
	if len(addrs) > 0 && !m.cfg.ExternalAccess {
		return ErrNoExternalAccess
	}
	want := map[string]bool{}
	for _, a := range addrs {
		h, err := hostPattern(a)
		if err != nil {
			return err
		}
		want[h] = true
	}
	have, err := m.hosts(ctx, name)
	if err != nil {
		return err
	}
	exists := map[string]bool{}
	for _, h := range have {
		exists[h] = true
		if h != m.cfg.LocalHost && !want[h] {
			if err := m.exec(ctx, "DROP USER IF EXISTS "+sq(name)+"@"+sq(h)); err != nil {
				return err
			}
		}
	}
	if !exists[m.cfg.LocalHost] {
		return fmt.Errorf("userdb: database %q has no main account", name)
	}
	hash, err := m.passwordHash(ctx, name)
	if err != nil {
		return err
	}
	for h := range want {
		if exists[h] {
			continue
		}
		// Пароль копируется как хеш основной записи: пароль в открытом виде панель не хранит.
		if err := m.exec(ctx,
			"CREATE USER "+sq(name)+"@"+sq(h)+" IDENTIFIED BY PASSWORD "+sq(hash)+" REQUIRE SSL WITH MAX_USER_CONNECTIONS 10",
			"GRANT "+privs(frozen)+" ON "+grantDB(name)+".* TO "+sq(name)+"@"+sq(h),
		); err != nil {
			return err
		}
	}
	return nil
}

func (m *Maria) TempAccount(ctx context.Context, name string, _ time.Time, frozen bool) (string, string, error) {
	pw, err := NewPassword()
	if err != nil {
		return "", "", err
	}
	suffix, err := NewPassword()
	if err != nil {
		return "", "", err
	}
	account := "tmp_" + tempTag(name) + "_" + strings.ToLower(suffix[:8])
	if err := m.exec(ctx,
		"CREATE USER "+sq(account)+"@"+sq(m.cfg.LocalHost)+" IDENTIFIED BY "+sq(pw)+" WITH MAX_USER_CONNECTIONS 3",
		"GRANT "+privs(frozen)+" ON "+grantDB(name)+".* TO "+sq(account)+"@"+sq(m.cfg.LocalHost),
	); err != nil {
		return "", "", err
	}
	return account, pw, nil
}

func (m *Maria) DropTemp(ctx context.Context, name, account string) error {
	if !strings.HasPrefix(account, "tmp_"+tempTag(name)+"_") {
		return fmt.Errorf("userdb: %q is not a temporary account of database %q", account, name)
	}
	// Закрываем открытые сессии учётной записи, затем удаляем её.
	rows, err := m.db.QueryContext(ctx, "SELECT id FROM information_schema.processlist WHERE user = ?", account)
	if err == nil {
		var ids []int64
		for rows.Next() {
			var id int64
			if rows.Scan(&id) == nil {
				ids = append(ids, id)
			}
		}
		_ = rows.Err()
		_ = rows.Close()
		for _, id := range ids {
			_ = m.exec(ctx, fmt.Sprintf("KILL %d", id))
		}
	}
	return m.exec(ctx, "DROP USER IF EXISTS "+sq(account)+"@"+sq(m.cfg.LocalHost))
}
