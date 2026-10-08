package userdb

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// PGConfig — подключение к серверу PostgreSQL для пользовательских баз.
type PGConfig struct {
	// AdminURL — служебная учётная запись панели (суперпользователь ИМЕННО ЭТОГО кластера: в нём живут только пользовательские базы).
	AdminURL string
	// HBADir — папка для правил pg_hba.conf внешнего доступа (подключается директивой include_dir); пусто — внешнего доступа нет.
	HBADir string
	// DBPrefix — префикс служебных ролей заморозки; задаётся тестам, чтобы не пересекаться с чужими ролями.
	FrozenRolePrefix string
}

// PG — Backend для PostgreSQL.
type PG struct{ cfg PGConfig }

// NewPG проверяет настройки и создаёт Backend.
func NewPG(cfg PGConfig) (*PG, error) {
	if _, err := pgx.ParseConfig(cfg.AdminURL); err != nil {
		return nil, fmt.Errorf("userdb: PostgreSQL address: %w", err)
	}
	if cfg.FrozenRolePrefix == "" {
		cfg.FrozenRolePrefix = "vh_frz_"
	}
	if cfg.HBADir != "" {
		if err := os.MkdirAll(cfg.HBADir, 0o750); err != nil {
			return nil, fmt.Errorf("userdb: access rules directory: %w", err)
		}
	}
	return &PG{cfg: cfg}, nil
}

func ident(s string) string { return pgx.Identifier{s}.Sanitize() }

// lit — строковый литерал. Пароли и даты генерирует сама панель из безопасных символов, но экранируем всё равно.
func lit(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }

// connect открывает соединение с сервером; db — база (пусто: служебная postgres).
func (p *PG) connect(ctx context.Context, db string) (*pgx.Conn, error) {
	cc, err := pgx.ParseConfig(p.cfg.AdminURL)
	if err != nil {
		return nil, err
	}
	if db != "" {
		cc.Database = db
	}
	// Пароли в DDL не должны попасть в журнал запросов.
	cc.RuntimeParams["application_name"] = "vladhost-userdb"
	conn, err := pgx.ConnectConfig(ctx, cc)
	if err != nil {
		return nil, err
	}
	_, _ = conn.Exec(ctx, "SET log_statement = 'none'")
	return conn, nil
}

func (p *PG) exec(ctx context.Context, db string, stmts ...string) error {
	conn, err := p.connect(ctx, db)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close(context.WithoutCancel(ctx)) }()
	for _, s := range stmts {
		if _, err := conn.Exec(ctx, s); err != nil {
			return err
		}
	}
	return nil
}

func (p *PG) Ping(ctx context.Context) error { return p.exec(ctx, "", "SELECT 1") }

func (p *PG) frozenRole(id int64) string { return p.cfg.FrozenRolePrefix + strconv.FormatInt(id, 10) }

func (p *PG) Create(ctx context.Context, name, password string) error {
	// Роль без прав, кроме входа; ограничение соединений и «зависших» транзакций защищает сервер от одной базы.
	if err := p.exec(ctx, "",
		"CREATE ROLE "+ident(name)+" LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION CONNECTION LIMIT 10 PASSWORD "+lit(password),
		"ALTER ROLE "+ident(name)+" SET idle_in_transaction_session_timeout = '10min'",
		"ALTER ROLE "+ident(name)+" SET statement_timeout = '5min'",
	); err != nil {
		return err
	}
	// CREATE DATABASE нельзя выполнять внутри транзакции, поэтому отдельным вызовом.
	if err := p.exec(ctx, "", "CREATE DATABASE "+ident(name)+" OWNER "+ident(name)+" TEMPLATE template0 ENCODING 'UTF8'"); err != nil {
		_ = p.exec(context.WithoutCancel(ctx), "", "DROP ROLE IF EXISTS "+ident(name))
		return err
	}
	// Чужие роли базу не видят: по умолчанию CONNECT выдан всем (PUBLIC).
	if err := p.exec(ctx, "", "REVOKE ALL ON DATABASE "+ident(name)+" FROM PUBLIC"); err != nil {
		_ = p.Drop(context.WithoutCancel(ctx), name, 0)
		return err
	}
	return nil
}

func (p *PG) Drop(ctx context.Context, name string, id int64) error {
	conn, err := p.connect(ctx, "")
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close(context.WithoutCancel(ctx)) }()
	// Закрываем сессии всех учётных записей базы, затем удаляем базу и роли (обычную, временные, роль заморозки).
	if _, err := conn.Exec(ctx, "SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = $1 AND pid <> pg_backend_pid()", name); err != nil {
		return err
	}
	if _, err := conn.Exec(ctx, "DROP DATABASE IF EXISTS "+ident(name)+" WITH (FORCE)"); err != nil {
		return err
	}
	rows, err := conn.Query(ctx, "SELECT rolname FROM pg_roles WHERE rolname = $1 OR rolname LIKE $2 OR rolname = $3", name, "tmp\\_"+tempTag(name)+"\\_%", p.frozenRole(id))
	if err != nil {
		return err
	}
	var roles []string
	for rows.Next() {
		var r string
		if err := rows.Scan(&r); err != nil {
			rows.Close()
			return err
		}
		roles = append(roles, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, r := range roles {
		if _, err := conn.Exec(ctx, "DROP ROLE IF EXISTS "+ident(r)); err != nil {
			return err
		}
	}
	if err := p.removeHBA(id); err != nil {
		return err
	}
	return p.reload(ctx, conn)
}

// tempTag — метка временных ролей базы: tmp_{метка}_{случайное}. Метка короткая и без «_» и «-», так что шаблон LIKE точен.
func tempTag(name string) string {
	h := uint32(2166136261)
	for _, c := range []byte(name) {
		h = (h ^ uint32(c)) * 16777619
	}
	return strconv.FormatUint(uint64(h), 36)
}

func (p *PG) SetPassword(ctx context.Context, name, password string) error {
	return p.exec(ctx, "", "ALTER ROLE "+ident(name)+" PASSWORD "+lit(password))
}

func (p *PG) Size(ctx context.Context, name string) (int64, error) {
	conn, err := p.connect(ctx, "")
	if err != nil {
		return 0, err
	}
	defer func() { _ = conn.Close(context.WithoutCancel(ctx)) }()
	var size int64
	err = conn.QueryRow(ctx, "SELECT pg_database_size($1::name)", name).Scan(&size)
	return size, err
}

// Freeze: владение объектами базы переходит служебной роли, поэтому пользователь больше не может ни создавать таблицы, ни менять
// права на существующие; ему остаются SELECT, DELETE и TRUNCATE — освободить место можно, писать новое нельзя.
func (p *PG) Freeze(ctx context.Context, name string, id int64) error {
	frz := p.frozenRole(id)
	conn, err := p.connect(ctx, "")
	if err != nil {
		return err
	}
	var exists bool
	if err := conn.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = $1)", frz).Scan(&exists); err != nil {
		_ = conn.Close(ctx)
		return err
	}
	if !exists {
		if _, err := conn.Exec(ctx, "CREATE ROLE "+ident(frz)+" NOLOGIN"); err != nil {
			_ = conn.Close(ctx)
			return err
		}
	}
	// Обрываем текущие сессии базы (владельца и временных записей веб-клиента): открытая транзакция иначе держала бы прежние права.
	_, _ = conn.Exec(ctx, "SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = $1 AND (usename = $2 OR usename LIKE $3) AND pid <> pg_backend_pid()",
		name, name, "tmp\\_"+tempTag(name)+"\\_%")
	_ = conn.Close(ctx)
	return p.exec(ctx, name,
		"REASSIGN OWNED BY "+ident(name)+" TO "+ident(frz),
		"GRANT CONNECT ON DATABASE "+ident(name)+" TO "+ident(name),
		"GRANT USAGE ON SCHEMA public TO "+ident(name),
		"GRANT SELECT, DELETE, TRUNCATE ON ALL TABLES IN SCHEMA public TO "+ident(name),
		"GRANT SELECT ON ALL SEQUENCES IN SCHEMA public TO "+ident(name),
	)
}

func (p *PG) Unfreeze(ctx context.Context, name string, id int64) error {
	frz := p.frozenRole(id)
	conn, err := p.connect(ctx, "")
	if err != nil {
		return err
	}
	var exists bool
	err = conn.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = $1)", frz).Scan(&exists)
	_ = conn.Close(ctx)
	if err != nil || !exists {
		return err
	}
	if err := p.exec(ctx, name, "REASSIGN OWNED BY "+ident(frz)+" TO "+ident(name)); err != nil {
		return err
	}
	return p.exec(ctx, "", "DROP ROLE IF EXISTS "+ident(frz))
}

func (p *PG) Compact(ctx context.Context, name string) error {
	// VACUUM нельзя выполнять внутри транзакции: exec использует простой запрос без неявной транзакции.
	return p.exec(ctx, name, "VACUUM (FULL)")
}

func (p *PG) hbaPath(id int64) string {
	return filepath.Join(p.cfg.HBADir, "db"+strconv.FormatInt(id, 10)+".conf")
}

func (p *PG) removeHBA(id int64) error {
	if p.cfg.HBADir == "" || id == 0 {
		return nil
	}
	if err := os.Remove(p.hbaPath(id)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func (p *PG) reload(ctx context.Context, conn *pgx.Conn) error {
	_, err := conn.Exec(ctx, "SELECT pg_reload_conf()")
	return err
}

// hbaLines строит правила pg_hba для базы: только TLS-соединения (hostssl), только её учётная запись, только с разрешённых адресов.
func hbaLines(name string, addrs []string) string {
	var b strings.Builder
	for _, a := range addrs {
		cidr := a
		if !strings.Contains(a, "/") {
			cidr += map[bool]string{true: "/128", false: "/32"}[strings.Contains(a, ":")]
		}
		fmt.Fprintf(&b, "hostssl %s %s %s scram-sha-256\n", `"`+name+`"`, `"`+name+`"`, cidr)
	}
	return b.String()
}

func (p *PG) SetAccess(ctx context.Context, name string, id int64, addrs []string, _ bool) error {
	if p.cfg.HBADir == "" {
		return ErrNoExternalAccess
	}
	if len(addrs) == 0 {
		if err := p.removeHBA(id); err != nil {
			return err
		}
	} else {
		// Файл заменяется целиком: сервер читает либо прежний, либо новый список, но не половину.
		tmp, err := os.CreateTemp(p.cfg.HBADir, ".tmp-*")
		if err != nil {
			return err
		}
		_, werr := tmp.WriteString("# managed by the Vladhost panel: do not edit by hand\n" + hbaLines(name, addrs))
		if cerr := tmp.Close(); werr == nil {
			werr = cerr
		}
		if werr == nil {
			werr = os.Chmod(tmp.Name(), 0o640)
		}
		if werr != nil {
			_ = os.Remove(tmp.Name())
			return werr
		}
		if err := os.Rename(tmp.Name(), p.hbaPath(id)); err != nil {
			_ = os.Remove(tmp.Name())
			return err
		}
	}
	conn, err := p.connect(ctx, "")
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close(context.WithoutCancel(ctx)) }()
	return p.reload(ctx, conn)
}

func (p *PG) TempAccount(ctx context.Context, name string, expires time.Time, _ bool) (string, string, error) {
	pw, err := NewPassword()
	if err != nil {
		return "", "", err
	}
	suffix, err := NewPassword()
	if err != nil {
		return "", "", err
	}
	account := "tmp_" + tempTag(name) + "_" + strings.ToLower(suffix[:8])
	// Учётная запись входит как обычная роль базы (SET ROLE при подключении): объекты, созданные в веб-клиенте, принадлежат ей,
	// а удаление временной записи ничего не теряет.
	if err := p.exec(ctx, "",
		"CREATE ROLE "+ident(account)+" LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION CONNECTION LIMIT 3 PASSWORD "+lit(pw)+
			" VALID UNTIL "+lit(expires.UTC().Format("2006-01-02 15:04:05+00"))+" IN ROLE "+ident(name),
		"ALTER ROLE "+ident(account)+" SET role = "+lit(name),
	); err != nil {
		return "", "", err
	}
	return account, pw, nil
}

func (p *PG) DropTemp(ctx context.Context, name, account string) error {
	if !strings.HasPrefix(account, "tmp_"+tempTag(name)+"_") {
		return fmt.Errorf("userdb: %q is not a temporary account of database %q", account, name)
	}
	conn, err := p.connect(ctx, "")
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close(context.WithoutCancel(ctx)) }()
	var exists bool
	if err := conn.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = $1)", account).Scan(&exists); err != nil || !exists {
		return err
	}
	_, _ = conn.Exec(ctx, "SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE usename = $1", account)
	// Всё, что успела создать временная запись, остаётся за базой.
	_ = p.exec(ctx, name, "REASSIGN OWNED BY "+ident(account)+" TO "+ident(name), "DROP OWNED BY "+ident(account))
	_, err = conn.Exec(ctx, "DROP ROLE IF EXISTS "+ident(account))
	return err
}
