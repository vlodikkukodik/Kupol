package database_test

import (
	"bytes"
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"kupol/internal/database"
	"kupol/internal/testutil"
)

func TestMigrateUpDownUp(t *testing.T) {
	ctx := context.Background()
	log := testutil.Logger()
	db := testutil.NewDB(t)

	exists := func(query, name string) bool {
		var n int64
		if err := db.Raw(query, name).Scan(&n).Error; err != nil {
			t.Fatal(err)
		}
		return n == 1
	}
	extExists := func(name string) bool {
		return exists("SELECT count(*) FROM pg_extension WHERE extname = ?", name)
	}
	tableExists := func(name string) bool {
		return exists("SELECT count(*) FROM information_schema.tables WHERE table_schema = 'public' AND table_name = ?", name)
	}
	version := func() int64 {
		v, err := database.SchemaVersion(ctx, db, log)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	accountTables := []string{"users", "sessions", "captcha_challenges"}
	documentTables := []string{"documents", "document_reads"}
	roleTables := []string{"user_roles"}
	versionTables := []string{"document_versions", "document_locks"}
	auditTables := []string{"audit_events"}
	reviewTables := []string{"review_comments", "review_events"}
	templateTables := []string{"templates"}
	glossaryTables := []string{"glossary_terms"}
	totpTables := []string{"totp_recovery_codes"}
	searchTables := []string{"document_search", "document_search_state"}
	linkTables := []string{"document_links"}
	timelineTables := []string{"timeline_events"}
	siteTables := []string{"site_settings"}
	xpTables := []string{"xp_events"}
	emailTables := []string{"email_confirmations"}
	remarkTables := []string{"remarks", "remark_reports"}
	ratingTables := []string{"document_ratings"}
	suggestionTables := []string{"suggestions"}
	collationExists := func() bool {
		return exists("SELECT count(*) FROM pg_collation WHERE collname = ? AND collnamespace = 'public'::regnamespace", "kupol_natural")
	}

	if err := database.MigrateUp(ctx, db, log); err != nil {
		t.Fatal(err)
	}
	if v := version(); v != 27 {
		t.Fatalf("версия схемы %d, ожидалась 27", v)
	}
	columnExists := func(table, column string) bool {
		var n int64
		if err := db.Raw("SELECT count(*) FROM information_schema.columns WHERE table_schema = 'public' AND table_name = ? AND column_name = ?", table, column).Scan(&n).Error; err != nil {
			t.Fatal(err)
		}
		return n == 1
	}
	for _, col := range []string{"totp_pending", "totp_secret", "totp_enabled_at", "totp_last_step"} {
		if !columnExists("users", col) {
			t.Errorf("колонка users.%s не создана", col)
		}
	}
	for _, col := range []string{"xp", "login_streak", "last_xp_day"} {
		if !columnExists("users", col) {
			t.Errorf("колонка users.%s не создана", col)
		}
	}
	for _, col := range []string{"email", "pending_email"} {
		if !columnExists("users", col) {
			t.Errorf("колонка users.%s не создана", col)
		}
	}
	for _, e := range []string{"citext", "pg_trgm"} {
		if !extExists(e) {
			t.Errorf("расширение %s не создано", e)
		}
	}
	for _, tbl := range slices.Concat(accountTables, documentTables, roleTables, versionTables, auditTables, reviewTables, templateTables, glossaryTables, totpTables, searchTables, linkTables, timelineTables, siteTables, xpTables, emailTables, remarkTables, ratingTables, suggestionTables) {
		if !tableExists(tbl) {
			t.Errorf("таблица %s не создана", tbl)
		}
	}
	if !collationExists() {
		t.Error("сортировка kupol_natural не создана")
	}

	// Повторный Up ничего не ломает.
	if err := database.MigrateUp(ctx, db, log); err != nil {
		t.Fatalf("повторный up: %v", err)
	}

	var out bytes.Buffer
	if err := database.MigrateStatus(ctx, db, log, &out); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"0001_extensions.sql", "0002_accounts.sql", "0003_documents.sql", "0004_roles.sql", "0005_document_versions.sql", "0006_audit.sql", "0007_review.sql", "0008_templates.sql", "0009_glossary.sql", "0010_totp.sql", "0011_search.sql", "0012_links.sql", "0013_timeline.sql", "0014_site_settings.sql", "0015_xp.sql", "0016_email.sql", "0017_remarks.sql", "0018_ratings.sql", "0019_suggestions.sql", "0020_document_translations.sql", "0021_secret_codes.sql", "0022_achievements.sql", "0023_inbox.sql", "0024_petitions.sql", "0025_sanctions.sql", "0026_invitations.sql", "0027_uploads.sql", "применена"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("status не содержит %q: %q", want, out.String())
		}
	}

	// Откат — по одной миграции: грамоты, скрытые коды, перевод дел, предложения, оценки, пометки на полях, почта, XP, настройки сайта, хронология, связи,
	// поиск, код из приложения, глоссарий, шаблоны, рецензия, журнал, версии и замки, роли, документы, аккаунты, расширения.
	if err := database.MigrateDown(ctx, db, log); err != nil {
		t.Fatal(err)
	}
	if v := version(); v != 26 {
		t.Fatalf("после отката 0027 версия %d, ожидалась 26", v)
	}
	if tableExists("uploads") {
		t.Error("таблица uploads осталась после отката 0027")
	}

	if err := database.MigrateDown(ctx, db, log); err != nil {
		t.Fatal(err)
	}
	if v := version(); v != 25 {
		t.Fatalf("после отката 0026 версия %d, ожидалась 25", v)
	}
	if tableExists("invitations") {
		t.Error("таблица invitations осталась после отката 0026")
	}

	if err := database.MigrateDown(ctx, db, log); err != nil {
		t.Fatal(err)
	}
	if v := version(); v != 24 {
		t.Fatalf("после отката 0025 версия %d, ожидалась 24", v)
	}
	if tableExists("sanctions") || columnExists("users", "banned_at") {
		t.Error("наказания остались после отката 0025")
	}

	if err := database.MigrateDown(ctx, db, log); err != nil {
		t.Fatal(err)
	}
	if v := version(); v != 23 {
		t.Fatalf("после отката 0024 версия %d, ожидалась 23", v)
	}
	if tableExists("petitions") {
		t.Error("таблица petitions осталась после отката 0024")
	}

	if err := database.MigrateDown(ctx, db, log); err != nil {
		t.Fatal(err)
	}
	if v := version(); v != 22 {
		t.Fatalf("после отката 0023 версия %d, ожидалась 22", v)
	}
	if tableExists("inbox_messages") {
		t.Error("таблица inbox_messages осталась после отката 0023")
	}

	if err := database.MigrateDown(ctx, db, log); err != nil {
		t.Fatal(err)
	}
	if v := version(); v != 21 {
		t.Fatalf("после отката 0022 версия %d, ожидалась 21", v)
	}
	if tableExists("achievements") {
		t.Error("таблица грамот осталась после отката 0022")
	}

	if err := database.MigrateDown(ctx, db, log); err != nil {
		t.Fatal(err)
	}
	if v := version(); v != 20 {
		t.Fatalf("после отката 0021 версия %d, ожидалась 20", v)
	}
	if tableExists("secret_codes") || tableExists("secret_code_redemptions") {
		t.Error("таблицы скрытых кодов остались после отката 0021")
	}

	if err := database.MigrateDown(ctx, db, log); err != nil {
		t.Fatal(err)
	}
	if v := version(); v != 19 {
		t.Fatalf("после отката 0020 версия %d, ожидалась 19", v)
	}
	if columnExists("documents", "translations") {
		t.Error("колонка documents.translations осталась после отката 0020")
	}

	if err := database.MigrateDown(ctx, db, log); err != nil {
		t.Fatal(err)
	}
	if v := version(); v != 18 {
		t.Fatalf("после отката 0019 версия %d, ожидалась 18", v)
	}
	for _, tbl := range suggestionTables {
		if tableExists(tbl) {
			t.Errorf("таблица %s осталась после отката 0019", tbl)
		}
	}
	if !tableExists("document_ratings") || !tableExists("remarks") || !tableExists("users") {
		t.Error("откат 0019 не должен трогать оценки, пометки и аккаунты")
	}

	if err := database.MigrateDown(ctx, db, log); err != nil {
		t.Fatal(err)
	}
	if v := version(); v != 17 {
		t.Fatalf("после отката 0018 версия %d, ожидалась 17", v)
	}
	if tableExists("document_ratings") {
		t.Error("таблица document_ratings осталась после отката 0018")
	}
	if !tableExists("remarks") || !tableExists("users") {
		t.Error("откат 0018 не должен трогать пометки и аккаунты")
	}

	if err := database.MigrateDown(ctx, db, log); err != nil {
		t.Fatal(err)
	}
	if v := version(); v != 16 {
		t.Fatalf("после отката 0017 версия %d, ожидалась 16", v)
	}
	for _, tbl := range remarkTables {
		if tableExists(tbl) {
			t.Errorf("таблица %s осталась после отката 0017", tbl)
		}
	}
	if !tableExists("users") || !tableExists("documents") {
		t.Error("откат 0017 не должен трогать аккаунты и документы")
	}

	if err := database.MigrateDown(ctx, db, log); err != nil {
		t.Fatal(err)
	}
	if v := version(); v != 15 {
		t.Fatalf("после отката 0016 версия %d, ожидалась 15", v)
	}
	for _, tbl := range emailTables {
		if tableExists(tbl) {
			t.Errorf("таблица %s осталась после отката 0016", tbl)
		}
	}
	for _, col := range []string{"email", "pending_email"} {
		if columnExists("users", col) {
			t.Errorf("колонка users.%s осталась после отката 0016", col)
		}
	}
	if !tableExists("xp_events") || !tableExists("users") {
		t.Error("откат 0016 не должен трогать XP и аккаунты")
	}

	if err := database.MigrateDown(ctx, db, log); err != nil {
		t.Fatal(err)
	}
	if v := version(); v != 14 {
		t.Fatalf("после отката 0015 версия %d, ожидалась 14", v)
	}
	for _, tbl := range xpTables {
		if tableExists(tbl) {
			t.Errorf("таблица %s осталась после отката 0015", tbl)
		}
	}
	for _, col := range []string{"xp", "login_streak", "last_xp_day"} {
		if columnExists("users", col) {
			t.Errorf("колонка users.%s осталась после отката 0015", col)
		}
	}
	if !tableExists("site_settings") || !tableExists("users") {
		t.Error("откат 0015 не должен трогать настройки сайта и аккаунты")
	}

	if err := database.MigrateDown(ctx, db, log); err != nil {
		t.Fatal(err)
	}
	if v := version(); v != 13 {
		t.Fatalf("после отката 0014 версия %d, ожидалась 13", v)
	}
	for _, tbl := range siteTables {
		if tableExists(tbl) {
			t.Errorf("таблица %s осталась после отката 0014", tbl)
		}
	}
	if !tableExists("timeline_events") || !tableExists("users") {
		t.Error("откат 0014 не должен трогать хронологию и аккаунты")
	}

	if err := database.MigrateDown(ctx, db, log); err != nil {
		t.Fatal(err)
	}
	if v := version(); v != 12 {
		t.Fatalf("после отката 0013 версия %d, ожидалась 12", v)
	}
	for _, tbl := range timelineTables {
		if tableExists(tbl) {
			t.Errorf("таблица %s осталась после отката 0013", tbl)
		}
	}
	if !tableExists("document_links") || !tableExists("documents") {
		t.Error("откат 0013 не должен трогать связи и документы")
	}

	if err := database.MigrateDown(ctx, db, log); err != nil {
		t.Fatal(err)
	}
	if v := version(); v != 11 {
		t.Fatalf("после отката 0012 версия %d, ожидалась 11", v)
	}
	for _, tbl := range linkTables {
		if tableExists(tbl) {
			t.Errorf("таблица %s осталась после отката 0012", tbl)
		}
	}
	if !tableExists("document_search") || !tableExists("documents") {
		t.Error("откат 0012 не должен трогать поиск и документы")
	}

	if err := database.MigrateDown(ctx, db, log); err != nil {
		t.Fatal(err)
	}
	if v := version(); v != 10 {
		t.Fatalf("после отката 0011 версия %d, ожидалась 10", v)
	}
	for _, tbl := range searchTables {
		if tableExists(tbl) {
			t.Errorf("таблица %s осталась после отката 0011", tbl)
		}
	}
	if !tableExists("documents") || !tableExists("totp_recovery_codes") {
		t.Error("откат 0011 не должен трогать документы и код из приложения")
	}

	if err := database.MigrateDown(ctx, db, log); err != nil {
		t.Fatal(err)
	}
	if v := version(); v != 9 {
		t.Fatalf("после отката 0010 версия %d, ожидалась 9", v)
	}
	for _, tbl := range totpTables {
		if tableExists(tbl) {
			t.Errorf("таблица %s осталась после отката 0010", tbl)
		}
	}
	for _, col := range []string{"totp_pending", "totp_secret", "totp_enabled_at", "totp_last_step"} {
		if columnExists("users", col) {
			t.Errorf("колонка users.%s осталась после отката 0010", col)
		}
	}
	if !tableExists("users") || !tableExists("glossary_terms") {
		t.Error("откат 0010 не должен трогать аккаунты и глоссарий")
	}

	if err := database.MigrateDown(ctx, db, log); err != nil {
		t.Fatal(err)
	}
	if v := version(); v != 8 {
		t.Fatalf("после отката 0009 версия %d, ожидалась 8", v)
	}
	for _, tbl := range glossaryTables {
		if tableExists(tbl) {
			t.Errorf("таблица %s осталась после отката 0009", tbl)
		}
	}
	if !tableExists("templates") || !tableExists("documents") {
		t.Error("откат 0009 не должен трогать шаблоны и документы")
	}

	if err := database.MigrateDown(ctx, db, log); err != nil {
		t.Fatal(err)
	}
	if v := version(); v != 7 {
		t.Fatalf("после отката 0008 версия %d, ожидалась 7", v)
	}
	for _, tbl := range templateTables {
		if tableExists(tbl) {
			t.Errorf("таблица %s осталась после отката 0008", tbl)
		}
	}
	if !tableExists("review_events") || !tableExists("documents") {
		t.Error("откат 0008 не должен трогать рецензию и документы")
	}

	if err := database.MigrateDown(ctx, db, log); err != nil {
		t.Fatal(err)
	}
	if v := version(); v != 6 {
		t.Fatalf("после отката 0007 версия %d, ожидалась 6", v)
	}
	for _, tbl := range reviewTables {
		if tableExists(tbl) {
			t.Errorf("таблица %s осталась после отката 0007", tbl)
		}
	}
	if !tableExists("audit_events") || !tableExists("documents") {
		t.Error("откат 0007 не должен трогать журнал и документы")
	}

	if err := database.MigrateDown(ctx, db, log); err != nil {
		t.Fatal(err)
	}
	if v := version(); v != 5 {
		t.Fatalf("после отката 0006 версия %d, ожидалась 5", v)
	}
	for _, tbl := range auditTables {
		if tableExists(tbl) {
			t.Errorf("таблица %s осталась после отката 0006", tbl)
		}
	}
	if !tableExists("document_versions") || !tableExists("users") {
		t.Error("откат 0006 не должен трогать версии документов и аккаунты")
	}

	if err := database.MigrateDown(ctx, db, log); err != nil {
		t.Fatal(err)
	}
	if v := version(); v != 4 {
		t.Fatalf("после отката 0005 версия %d, ожидалась 4", v)
	}
	for _, tbl := range versionTables {
		if tableExists(tbl) {
			t.Errorf("таблица %s осталась после отката 0005", tbl)
		}
	}
	if !tableExists("documents") || !tableExists("user_roles") {
		t.Error("откат 0005 не должен трогать документы и роли")
	}

	if err := database.MigrateDown(ctx, db, log); err != nil {
		t.Fatal(err)
	}
	if v := version(); v != 3 {
		t.Fatalf("после отката 0004 версия %d, ожидалась 3", v)
	}
	for _, tbl := range roleTables {
		if tableExists(tbl) {
			t.Errorf("таблица %s осталась после отката 0004", tbl)
		}
	}
	if !tableExists("documents") || !tableExists("users") {
		t.Error("откат 0004 не должен трогать документы и аккаунты")
	}

	if err := database.MigrateDown(ctx, db, log); err != nil {
		t.Fatal(err)
	}
	if v := version(); v != 2 {
		t.Fatalf("после отката 0003 версия %d, ожидалась 2", v)
	}
	for _, tbl := range documentTables {
		if tableExists(tbl) {
			t.Errorf("таблица %s осталась после отката 0003", tbl)
		}
	}
	if collationExists() {
		t.Error("сортировка осталась после отката 0003")
	}
	if !tableExists("users") {
		t.Error("откат 0003 не должен трогать аккаунты")
	}

	if err := database.MigrateDown(ctx, db, log); err != nil {
		t.Fatal(err)
	}
	if v := version(); v != 1 {
		t.Fatalf("после отката 0002 версия %d, ожидалась 1", v)
	}
	for _, tbl := range accountTables {
		if tableExists(tbl) {
			t.Errorf("таблица %s осталась после отката 0002", tbl)
		}
	}
	if !extExists("citext") {
		t.Error("откат 0002 не должен трогать расширения")
	}

	if err := database.MigrateDown(ctx, db, log); err != nil {
		t.Fatal(err)
	}
	for _, e := range []string{"citext", "pg_trgm"} {
		if extExists(e) {
			t.Errorf("расширение %s осталось после отката 0001", e)
		}
	}

	if err := database.MigrateUp(ctx, db, log); err != nil {
		t.Fatalf("up после down: %v", err)
	}
	if v := version(); v != 27 || !tableExists("uploads") || !tableExists("invitations") || !tableExists("sanctions") || !tableExists("petitions") || !tableExists("inbox_messages") || !tableExists("achievements") || !tableExists("secret_codes") || !tableExists("suggestions") || !tableExists("document_ratings") || !tableExists("remarks") || !tableExists("email_confirmations") || !tableExists("xp_events") || !tableExists("site_settings") || !tableExists("timeline_events") || !tableExists("document_links") || !tableExists("document_search") || !tableExists("totp_recovery_codes") || !tableExists("glossary_terms") || !tableExists("templates") || !tableExists("review_events") || !tableExists("audit_events") || !extExists("citext") || !tableExists("users") || !tableExists("documents") || !tableExists("user_roles") || !tableExists("document_versions") {
		t.Errorf("после повторного up: версия %d", v)
	}
}

func TestDocumentVersionsAndLocksSchemaConstraints(t *testing.T) {
	db := testutil.NewMigratedDB(t)
	now := time.Now().UTC()
	var uid, doc int64
	if err := db.Raw(`INSERT INTO users (login, password_hash, backup_code_hash, created_at, password_changed_at)
		VALUES ('anna', 'h', 'b', ?, ?) RETURNING id`, now, now).Scan(&uid).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Raw(`INSERT INTO documents (code, slug, type, title, status, composed_year, author_id, created_at, updated_at)
		VALUES ('МЕМО-1', 'MEMO-1', 'memo', 'Тест', 'draft', 1979, ?, ?, ?) RETURNING id`, uid, now, now).Scan(&doc).Error; err != nil {
		t.Fatal(err)
	}
	version := func(kind, status string, permanent bool, revision int) error {
		return db.Exec(`INSERT INTO document_versions (document_id, revision, kind, status, permanent, author_id, content, created_at)
			VALUES (?, ?, ?, ?, ?, ?, '{"title":"x"}', ?)`, doc, revision, kind, status, permanent, uid, now).Error
	}
	count := func(table string) int64 {
		var n int64
		if err := db.Raw("SELECT count(*) FROM " + table).Scan(&n).Error; err != nil {
			t.Fatal(err)
		}
		return n
	}

	// «хранится всегда» определяется видом снимка: автосохранение не может быть вечным, правка опубликованного — скользящей
	for kind, permanent := range map[string]bool{
		"create": true, "status": true, "edit": true, "import": true, "rollback": true, "autosave": false, "save": false,
	} {
		if err := version(kind, "draft", permanent, 1); err != nil {
			t.Errorf("снимок %s (permanent=%v): %v", kind, permanent, err)
		}
	}
	for kind, permanent := range map[string]bool{"autosave": true, "save": true, "edit": false, "status": false, "create": false} {
		if err := version(kind, "draft", permanent, 1); err == nil {
			t.Errorf("снимок %s с permanent=%v должен отвергаться", kind, permanent)
		}
	}
	for _, bad := range []struct {
		kind, status string
		revision     int
	}{{"fly", "draft", 1}, {"edit", "deleted", 1}, {"edit", "draft", 0}} {
		if err := version(bad.kind, bad.status, true, bad.revision); err == nil {
			t.Errorf("снимок %+v должен отвергаться", bad)
		}
	}
	if err := db.Exec(`INSERT INTO document_versions (document_id, revision, kind, status, permanent, content, created_at)
		VALUES (999999, 1, 'create', 'draft', true, '{}', ?)`, now).Error; err == nil {
		t.Error("снимок несуществующего документа должен отвергаться")
	}

	// замок: один на документ; срок должен быть позже времени взятия
	lock := func(acq, exp time.Time) error {
		return db.Exec(`INSERT INTO document_locks (document_id, user_id, acquired_at, expires_at) VALUES (?, ?, ?, ?)`, doc, uid, acq, exp).Error
	}
	if err := lock(now, now); err == nil {
		t.Error("замок с истёкшим сразу сроком должен отвергаться")
	}
	if err := lock(now, now.Add(15*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := lock(now, now.Add(15*time.Minute)); err == nil {
		t.Error("второй замок на тот же документ должен отвергаться")
	}

	// удаление документа убирает историю и замок; удаление автора оставляет историю без автора
	if err := db.Exec("DELETE FROM users WHERE id = ?", uid).Error; err != nil {
		t.Fatal(err)
	}
	var withAuthor int64
	if err := db.Raw("SELECT count(*) FROM document_versions WHERE author_id IS NOT NULL").Scan(&withAuthor).Error; err != nil || withAuthor != 0 {
		t.Errorf("после удаления автора author_id заполнен у %d снимков, err=%v", withAuthor, err)
	}
	if count("document_versions") == 0 {
		t.Error("история пропала вместе с автором")
	}
	if count("document_locks") != 0 {
		t.Error("замок удалённого пользователя должен исчезнуть")
	}
	if err := db.Exec("DELETE FROM documents WHERE id = ?", doc).Error; err != nil {
		t.Fatal(err)
	}
	if count("document_versions") != 0 {
		t.Error("история осталась после удаления документа")
	}
}

func TestUserRolesSchemaConstraints(t *testing.T) {
	db := testutil.NewMigratedDB(t)
	now := time.Now().UTC()
	user := func(login string) int64 {
		t.Helper()
		var id int64
		if err := db.Raw(`INSERT INTO users (login, password_hash, backup_code_hash, created_at, password_changed_at)
			VALUES (?, 'h', 'b', ?, ?) RETURNING id`, login, now, now).Scan(&id).Error; err != nil {
			t.Fatal(err)
		}
		return id
	}
	grant := func(uid int64, role string, by any) error {
		return db.Exec(`INSERT INTO user_roles (user_id, role, granted_by, granted_at) VALUES (?, ?, ?, ?)`, uid, role, by, now).Error
	}
	count := func() int64 {
		var n int64
		if err := db.Raw("SELECT count(*) FROM user_roles").Scan(&n).Error; err != nil {
			t.Fatal(err)
		}
		return n
	}

	a, b := user("anna"), user("boris")
	for _, role := range []string{"author", "editor", "moderator", "archivist"} {
		if err := grant(a, role, b); err != nil {
			t.Fatalf("роль %s: %v", role, err)
		}
	}
	if err := grant(a, "author", b); err == nil {
		t.Error("одна и та же роль у одного человека дважды — должно отвергаться")
	}
	// «Директорат» — флаг пользователя, а не роль; произвольные названия тоже недопустимы
	for _, bad := range []string{"directorate", "admin", "Author", "", "editor "} {
		if err := grant(b, bad, nil); err == nil {
			t.Errorf("роль %q должна отвергаться", bad)
		}
	}
	if err := grant(999999, "author", nil); err == nil {
		t.Error("роль несуществующему пользователю должна отвергаться")
	}

	// удаление выдавшего оставляет роль, но очищает «кто выдал»
	if err := db.Exec("DELETE FROM users WHERE id = ?", b).Error; err != nil {
		t.Fatal(err)
	}
	var withGranter int64
	if err := db.Raw("SELECT count(*) FROM user_roles WHERE granted_by IS NOT NULL").Scan(&withGranter).Error; err != nil || withGranter != 0 {
		t.Errorf("после удаления выдавшего granted_by заполнен у %d строк, err=%v", withGranter, err)
	}
	if count() != 4 {
		t.Errorf("роли пропали вместе с выдавшим: %d", count())
	}
	// удаление самого пользователя убирает его роли
	if err := db.Exec("DELETE FROM users WHERE id = ?", a).Error; err != nil {
		t.Fatal(err)
	}
	if count() != 0 {
		t.Errorf("после удаления пользователя осталось %d ролей", count())
	}
}

func TestDocumentsSchemaConstraints(t *testing.T) {
	db := testutil.NewMigratedDB(t)
	now := time.Now().UTC()

	// row — допустимый минимальный документ («эталон»); тесты меняют в нём отдельные поля.
	type row map[string]any
	insert := func(overrides row) error {
		f := row{"type": "object", "title": "Т", "composed_year": 1979, "created_at": now, "updated_at": now}
		for k, v := range overrides {
			f[k] = v
		}
		var cols, marks []string
		var args []any
		for k, v := range f {
			cols = append(cols, k)
			marks = append(marks, "?")
			args = append(args, v)
		}
		return db.Exec("INSERT INTO documents ("+strings.Join(cols, ", ")+") VALUES ("+strings.Join(marks, ", ")+")", args...).Error
	}
	ok := func(desc string, o row) {
		t.Helper()
		if err := insert(o); err != nil {
			t.Errorf("%s: %v", desc, err)
		}
	}
	bad := func(desc string, o row) {
		t.Helper()
		if err := insert(o); err == nil {
			t.Errorf("%s: ожидалась ошибка ограничения", desc)
		}
	}

	ok("минимальный объект-черновик без шифра", row{})
	ok("объект со всеми полями", row{
		"code": "О-041", "slug": "O-041", "object_number": 41, "danger_class": 3, "deviation_points": 12,
		"category": "entity", "containment_status": "contained", "discovery_place": "Полигон", "level": 2,
	})

	bad("шифр без slug", row{"code": "О-1"})
	bad("slug без шифра", row{"slug": "O-1"})
	bad("опубликованный документ без шифра", row{"status": "published"})
	ok("опубликованный документ с шифром", row{"status": "published", "code": "О-500", "slug": "O-500", "object_number": 500})
	bad("повтор шифра", row{"code": "О-041", "slug": "O-999"})
	bad("повтор slug", row{"code": "О-999", "slug": "O-041"})
	bad("повтор номера объекта", row{"object_number": 41})
	bad("неизвестный тип", row{"type": "spaceship"})
	bad("неизвестный статус", row{"status": "hidden"})
	bad("уровень 8", row{"level": 8})
	bad("отрицательный уровень", row{"level": -1})
	ok("уровень 7 — только Директорат", row{"level": 7})
	bad("режим прямой ссылки", row{"direct_link": "maybe"})
	bad("год 1899", row{"composed_year": 1899})
	bad("год 2100", row{"composed_year": 2100})
	bad("месяц 13", row{"composed_month": 13})
	bad("день без месяца", row{"composed_day": 5})
	ok("месяц и день", row{"composed_month": 3, "composed_day": 14})
	bad("класс опасности 6", row{"danger_class": 6})
	bad("класс опасности 0", row{"danger_class": 0})
	bad("отрицательные п.о.", row{"deviation_points": -1})
	bad("неизвестная категория", row{"category": "ghost"})
	bad("неизвестный статус содержания", row{"containment_status": "escaped"})
	bad("блоки — не массив", row{"blocks": `{"a":1}`})
	ok("блоки — массив", row{"blocks": `[{"id":"b1"}]`})

	// Поля Объекта у других типов запрещены.
	for col, val := range map[string]any{
		"danger_class": 1, "deviation_points": 1, "category": "person", "containment_status": "lost",
		"discovery_place": "где-то", "object_number": 7,
	} {
		bad("поле "+col+" у приказа", row{"type": "order", col: val})
	}
	ok("приказ без полей объекта", row{"type": "order", "composed_year": 1978, "code": "ПРИКАЗ-1978-12", "slug": "PRIKAZ-1978-12"})

	// Естественная сортировка шифров.
	for _, n := range []string{"2", "12", "3"} {
		ok("приказ "+n, row{"type": "order", "code": "ПРИКАЗ-1979-" + n, "slug": "PRIKAZ-1979-" + n})
	}
	var codes []string
	if err := db.Raw(`SELECT code FROM documents WHERE code LIKE 'ПРИКАЗ-1979-%' ORDER BY code COLLATE kupol_natural`).Scan(&codes).Error; err != nil {
		t.Fatal(err)
	}
	if strings.Join(codes, ",") != "ПРИКАЗ-1979-2,ПРИКАЗ-1979-3,ПРИКАЗ-1979-12" {
		t.Errorf("естественная сортировка: %v", codes)
	}

	// Каскады: удаление автора обнуляет author_id, удаление документа чистит историю чтения.
	var uid int64
	if err := db.Raw(`INSERT INTO users (login, password_hash, backup_code_hash, created_at, password_changed_at)
		VALUES ('reader', 'h', 'b', ?, ?) RETURNING id`, now, now).Scan(&uid).Error; err != nil {
		t.Fatal(err)
	}
	var did int64
	if err := db.Raw(`INSERT INTO documents (type, title, composed_year, created_at, updated_at, author_id)
		VALUES ('object', 'А', 1979, ?, ?, ?) RETURNING id`, now, now, uid).Scan(&did).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO document_reads (user_id, document_id, first_read_at, last_read_at) VALUES (?, ?, ?, ?)`, uid, did, now, now).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`DELETE FROM users WHERE id = ?`, uid).Error; err != nil {
		t.Fatal(err)
	}
	var author *int64
	var reads int64
	db.Raw(`SELECT author_id FROM documents WHERE id = ?`, did).Scan(&author)
	db.Raw(`SELECT count(*) FROM document_reads`).Scan(&reads)
	if author != nil || reads != 0 {
		t.Errorf("после удаления автора: author_id=%v, чтений=%d (ожидались NULL и 0)", author, reads)
	}
	if err := db.Exec(`DELETE FROM documents WHERE id = ?`, did).Error; err != nil {
		t.Fatal(err)
	}
}

func TestAccountsSchemaConstraints(t *testing.T) {
	db := testutil.NewMigratedDB(t)
	now := time.Now().UTC()
	insert := func(login string, level int) error {
		return db.Exec(`INSERT INTO users (login, password_hash, backup_code_hash, level, created_at, password_changed_at)
			VALUES (?, 'h', 'b', ?, ?, ?)`, login, level, now, now).Error
	}

	if err := insert("Куратор", 1); err != nil {
		t.Fatalf("нормальная вставка: %v", err)
	}
	if err := insert("куРАТОР", 1); err == nil {
		t.Error("логин должен быть уникален без учёта регистра")
	}
	for _, bad := range []string{"ab", strings.Repeat("a", 25)} {
		if err := insert(bad, 1); err == nil {
			t.Errorf("логин длиной %d должен отвергаться", len(bad))
		}
	}
	for _, lvl := range []int{0, 7} {
		if err := insert("lvl"+strings.Repeat("x", lvl), lvl); err == nil {
			t.Errorf("уровень %d должен отвергаться", lvl)
		}
	}

	// Каскад: удаление пользователя удаляет его сессии.
	var uid int64
	if err := db.Raw("SELECT id FROM users WHERE login = 'Куратор'").Scan(&uid).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO sessions (token_hash, user_id, created_at, last_seen_at, expires_at, absolute_expires_at)
		VALUES ('\x01', ?, ?, ?, ?, ?)`, uid, now, now, now, now).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("DELETE FROM users WHERE id = ?", uid).Error; err != nil {
		t.Fatal(err)
	}
	var left int64
	if err := db.Raw("SELECT count(*) FROM sessions").Scan(&left).Error; err != nil || left != 0 {
		t.Fatalf("сессий после удаления пользователя: %d, err=%v", left, err)
	}
}

func TestCitextIsCaseInsensitive(t *testing.T) {
	db := testutil.NewMigratedDB(t)
	var eq bool
	if err := db.Raw("SELECT 'Куратор'::citext = 'куратор'::citext").Scan(&eq).Error; err != nil {
		t.Fatal(err)
	}
	if !eq {
		t.Fatal("citext должен сравнивать кириллицу без учёта регистра")
	}
}

func TestPing(t *testing.T) {
	db := testutil.NewDB(t)
	if err := database.Ping(context.Background(), db); err != nil {
		t.Fatal(err)
	}
}

func TestOpenUnreachableFailsAfterDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	start := time.Now()
	_, err := database.Open(ctx, "postgres://kupol:kupol@127.0.0.1:1/none?sslmode=disable", testutil.Logger())
	if err == nil {
		t.Fatal("ожидалась ошибка недоступной БД")
	}
	elapsed := time.Since(start)
	if elapsed < time.Second {
		t.Fatalf("Open сдался за %s, не дожидаясь готовности БД", elapsed)
	}
	if elapsed > 10*time.Second {
		t.Fatalf("ожидание не ограничено контекстом: %s", elapsed)
	}
}
