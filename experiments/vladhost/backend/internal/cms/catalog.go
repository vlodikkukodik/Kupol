// Package cms — установка готовых приложений на сайт «в один клик». Сейчас это WordPress: панель создаёт базу в MariaDB, скачивает
// проверенный дистрибутив, раскладывает файлы, пишет wp-config.php и выполняет установку самого WordPress через веб-шлюз.
package cms

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
)

// WordPress — идентификатор приложения в каталоге.
const WordPress = "wordpress"

// App — приложение каталога. Версия и контрольная сумма закреплены: мы ставим ровно то, что проверили, а не «последнее с сайта».
type App struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Version string `json:"version"`
	URL     string `json:"url"`
	SHA256  string `json:"sha256"`
}

var sha256Re = regexp.MustCompile(`^[0-9a-f]{64}$`)

// DefaultCatalog — встроенный каталог. Обновление версии — правка этой записи (адрес и sha256 официального архива).
func DefaultCatalog() []App {
	return []App{{
		ID: WordPress, Name: "WordPress", Version: "7.1.2",
		URL:    "https://downloads.wordpress.org/release/wordpress-7.1.2.zip",
		SHA256: "8fc96c59a78b7219e4a130222b7fadb51b03e503e8b0123beaa7e28961c21ce2",
	}}
}

// Validate проверяет запись каталога: имя попадает в путь кэша, адрес — в запрос, сумма — в сравнение.
func (a App) Validate() error {
	switch {
	case a.ID != WordPress:
		return fmt.Errorf("cms: unknown app %q", a.ID)
	case !regexp.MustCompile(`^[0-9]+(\.[0-9]+){1,3}$`).MatchString(a.Version):
		return fmt.Errorf("cms: bad version %q", a.Version)
	case !strings.HasPrefix(a.URL, "https://") && !strings.HasPrefix(a.URL, "http://127.0.0.1"):
		return fmt.Errorf("cms: url %q must be https", a.URL)
	case !sha256Re.MatchString(a.SHA256):
		return errors.New("cms: sha256 must be 64 hex digits")
	}
	return nil
}

// LoadCatalog читает каталог из JSON-файла (VLADHOST_CMS_CATALOG) вместо встроенного: администратор сервера может закрепить свою версию
// или зеркало. Файл — [{"id":"wordpress","name":"WordPress","version":"…","url":"https://…","sha256":"…"}].
func LoadCatalog(path string) ([]App, error) {
	if path == "" {
		return DefaultCatalog(), nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var apps []App
	if err := json.Unmarshal(data, &apps); err != nil {
		return nil, fmt.Errorf("cms: catalog %s: %w", path, err)
	}
	for _, a := range apps {
		if err := a.Validate(); err != nil {
			return nil, err
		}
	}
	return apps, nil
}
