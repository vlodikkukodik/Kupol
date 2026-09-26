package httpapi_test

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type backupsBody struct {
	Backups []struct {
		ID    int64  `json:"id"`
		Day   string `json:"day"`
		Bytes int64  `json:"bytes"`
		Files int    `json:"files"`
	} `json:"backups"`
}

type backupCreateBody struct {
	Backup struct {
		ID    int64  `json:"id"`
		Day   string `json:"day"`
		Files int    `json:"files"`
	} `json:"backup"`
}

func TestBackupsDisabledWithoutConfig(t *testing.T) {
	e := newEnv(t)
	adm, _ := e.admin()
	john := e.user(adm, "john")
	id, _ := e.createSite(john, "blog")

	w := e.do("GET", "/api/sites/"+itoa(id)+"/backups", nil, john)
	if w.Code != 409 || decode[errBody](t, w).Error.Code != "backups_unavailable" {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	w = e.do("POST", "/api/sites/"+itoa(id)+"/backups", map[string]string{}, john)
	if w.Code != 409 || decode[errBody](t, w).Error.Code != "backups_unavailable" {
		t.Fatalf("создание: %d %s", w.Code, w.Body)
	}
	if !strings.Contains(e.do("GET", "/api/sites", nil, john).Body.String(), `"backups_available":false`) {
		t.Fatal("список сайтов должен сообщать, что резервные копии недоступны")
	}
}

func TestBackupsCreateListRestoreDownload(t *testing.T) {
	e := newEnv(t)
	e.sites.ConfigureBackups(t.TempDir())
	adm, _ := e.admin()
	john, mary := e.user(adm, "john"), e.user(adm, "mary")
	id, host := e.createSite(john, "blog")
	base := "/api/sites/" + itoa(id) + "/backups"

	if !strings.Contains(e.do("GET", "/api/sites", nil, john).Body.String(), `"backups_available":true`) {
		t.Fatal("список сайтов должен сообщать, что резервные копии доступны")
	}

	// Пустой список до первого снимка.
	if got := decode[backupsBody](t, e.do("GET", base, nil, john)); len(got.Backups) != 0 {
		t.Fatalf("ожидали пустой список: %+v", got)
	}

	// Деплой v1, снимок, деплой v2.
	zipV1 := makeZip(t, zipFile{name: "index.html", body: "<h1>v1</h1>"})
	if w := e.upload("/api/sites/"+itoa(id)+"/deploy", john, zipV1); w.Code != 200 {
		t.Fatalf("деплой v1: %d %s", w.Code, w.Body)
	}
	w := e.do("POST", base, map[string]string{}, john)
	if w.Code != 201 {
		t.Fatalf("создание копии: %d %s", w.Code, w.Body)
	}
	created := decode[backupCreateBody](t, w)
	if created.Backup.Files != 1 || created.Backup.Day == "" {
		t.Fatalf("снимок: %+v", created)
	}

	// Повторное создание за сегодня заменяет снимок, но не плодит записи.
	if w := e.do("POST", base, map[string]string{}, john); w.Code != 201 {
		t.Fatalf("повтор: %d %s", w.Code, w.Body)
	}
	list := decode[backupsBody](t, e.do("GET", base, nil, john))
	if len(list.Backups) != 1 {
		t.Fatalf("один снимок в сутки: %+v", list.Backups)
	}
	bid := list.Backups[0].ID

	zipV2 := makeZip(t, zipFile{name: "index.html", body: "<h1>v2</h1>"})
	if w := e.upload("/api/sites/"+itoa(id)+"/deploy", john, zipV2); w.Code != 200 {
		t.Fatalf("деплой v2: %d %s", w.Code, w.Body)
	}
	if got := e.read(host, "index.html"); got != "<h1>v2</h1>" {
		t.Fatalf("после деплоя: %q", got)
	}

	// Скачивание снимка — старое содержимое.
	d := e.do("GET", base+"/"+itoa(bid)+"/download", nil, john)
	if d.Code != 200 || !strings.Contains(d.Header().Get("Content-Type"), "application/zip") {
		t.Fatalf("скачивание копии: %d %v", d.Code, d.Header())
	}
	if name := zipHas(t, d.Body.Bytes(), "index.html"); name == "" || !bytes.Contains(d.Body.Bytes(), []byte("<h1>v1</h1>")) {
		t.Fatalf("в архиве должна быть v1: %q", d.Body.Bytes())
	}

	// Скачивание текущего архива сайта — новое содержимое.
	a := e.do("GET", "/api/sites/"+itoa(id)+"/archive", nil, john)
	if a.Code != 200 || !bytes.Contains(a.Body.Bytes(), []byte("<h1>v2</h1>")) {
		t.Fatalf("архив сайта: %d %s", a.Code, a.Body.Bytes())
	}

	// Восстановление возвращает v1 и обновляет disk_bytes.
	if w := e.do("POST", base+"/"+itoa(bid)+"/restore", map[string]string{}, john); w.Code != 200 {
		t.Fatalf("восстановление: %d %s", w.Code, w.Body)
	}
	if got := e.read(host, "index.html"); got != "<h1>v1</h1>" {
		t.Fatalf("после восстановления: %q", got)
	}
	var after struct {
		Site struct {
			Status    string `json:"status"`
			DiskBytes int64  `json:"disk_bytes"`
		} `json:"site"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &after); err != nil {
		t.Fatal(err)
	}
	if after.Site.Status != "live" || after.Site.DiskBytes <= 0 {
		t.Fatalf("после восстановления сайт: %+v", after.Site)
	}

	// Чужой сайт и битый id.
	if w := e.do("GET", base, nil, mary); w.Code != 404 {
		t.Errorf("чужой сайт: %d", w.Code)
	}
	if w := e.do("POST", base, map[string]string{}, mary); w.Code != 404 {
		t.Errorf("чужое создание: %d", w.Code)
	}
	if w := e.do("GET", base+"/99999/download", nil, john); w.Code != 404 {
		t.Errorf("неизвестная копия: %d", w.Code)
	}
	if w := e.do("POST", base+"/not-a-number/restore", map[string]string{}, john); w.Code != 404 {
		t.Errorf("битый id: %d", w.Code)
	}
	if w := e.do("GET", base, nil, ""); w.Code != 401 {
		t.Errorf("без токена: %d", w.Code)
	}
	if w := e.do("GET", "/api/sites/"+itoa(id)+"/archive", nil, mary); w.Code != 404 {
		t.Errorf("чужой архив: %d", w.Code)
	}
}

func TestBackupsCyclePrunesOld(t *testing.T) {
	e := newEnv(t)
	dir := t.TempDir()
	e.sites.ConfigureBackups(dir)
	adm, _ := e.admin()
	john := e.user(adm, "john")
	id, _ := e.createSite(john, "blog")
	base := "/api/sites/" + itoa(id) + "/backups"

	// Свежий снимок.
	w := e.do("POST", base, map[string]string{}, john)
	if w.Code != 201 {
		t.Fatalf("создание: %d %s", w.Code, w.Body)
	}
	today := decode[backupCreateBody](t, w).Backup.Day
	todayDir := filepath.Join(dir, itoa(id), today)
	if _, err := os.Stat(todayDir); err != nil {
		t.Fatalf("каталог снимка: %v", err)
	}

	// Подкладываем «вчерашний» снимок старше недели через прямую запись в БД и на диск.
	oldDay := time.Now().UTC().AddDate(0, 0, -10).Format("2006-01-02")
	oldDir := filepath.Join(dir, itoa(id), oldDay)
	if err := os.MkdirAll(oldDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(oldDir, "index.html"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := e.db.Exec(`INSERT INTO site_backups (site_id, day, bytes, files) VALUES (?, ?, 3, 1)`, id, oldDay).Error; err != nil {
		t.Fatal(err)
	}

	// Второй цикл не плодит снимки за сегодня и чистит старые.
	if err := e.sites.BackupCycle(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := e.sites.BackupCycle(t.Context()); err != nil {
		t.Fatal(err)
	}

	list := decode[backupsBody](t, e.do("GET", base, nil, john))
	if len(list.Backups) != 1 || list.Backups[0].Day != today {
		t.Fatalf("после prune: %+v", list.Backups)
	}
	if _, err := os.Stat(oldDir); !os.IsNotExist(err) {
		t.Fatalf("старый каталог должен удалиться: %v", err)
	}
	if _, err := os.Stat(todayDir); err != nil {
		t.Fatalf("свежий каталог должен остаться: %v", err)
	}

	// Удаление сайта чистит каталог бэкапов.
	if w := e.do("DELETE", "/api/sites/"+itoa(id), nil, john); w.Code != 204 {
		t.Fatalf("удаление сайта: %d %s", w.Code, w.Body)
	}
	if _, err := os.Stat(filepath.Join(dir, itoa(id))); !os.IsNotExist(err) {
		t.Fatalf("каталог бэкапов должен удалиться: %v", err)
	}
}

// zipHas возвращает имя файла с содержимым needle, если он есть в архиве.
func zipHas(t *testing.T, raw []byte, name string) string {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatalf("открыть zip: %v", err)
	}
	for _, f := range zr.File {
		if f.Name != name {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(rc)
		_ = rc.Close()
		if bytes.Contains(b, []byte("v1")) || bytes.Contains(b, []byte("v2")) {
			return f.Name
		}
	}
	return ""
}
