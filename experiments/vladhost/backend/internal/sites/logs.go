package sites

import (
	"context"
	"io"
	"net/http"
	"time"

	"vladhost/internal/apperr"
	"vladhost/internal/sitelog"
)

var (
	ErrLogsOff      = apperr.New(http.StatusConflict, "logs_unavailable", "site logs are not enabled")
	ErrLogsBadQuery = apperr.Validation("status", "log_query", "invalid log query")
)

// ConfigureLogs включает чтение журналов сайтов из каталога, куда пишет веб-шлюз.
func (s *Service) ConfigureLogs(dir string) { s.logDir = dir }

// LogsAvailable сообщает, настроены ли журналы.
func (s *Service) LogsAvailable() bool { return s.logDir != "" }

// LogQuery — параметры запроса журнала из API.
type LogQuery struct {
	Kind   string
	Status string
	Text   string
	Limit  int
	Before time.Time
}

const maxLogText = 100

// Logs возвращает страницу журнала своего сайта. Событий раньше создания сайта не показываем:
// адрес мог принадлежать прежнему сайту с тем же именем.
func (s *Service) Logs(ctx context.Context, userID, id int64, q LogQuery) (sitelog.Page, error) {
	site, err := s.Get(ctx, userID, id)
	if err != nil {
		return sitelog.Page{}, err
	}
	if s.logDir == "" {
		return sitelog.Page{}, ErrLogsOff
	}
	if (q.Kind != sitelog.KindAccess && q.Kind != sitelog.KindError) || !sitelog.ValidStatus(q.Status) || len(q.Text) > maxLogText || q.Limit < 0 {
		return sitelog.Page{}, ErrLogsBadQuery
	}
	return sitelog.Read(s.logDir, site.Host, sitelog.Query{
		Kind: q.Kind, Status: q.Status, Text: q.Text, Limit: q.Limit, Before: q.Before, Since: site.CreatedAt,
	})
}

// WriteLogs пишет журнал целиком обычным текстом (для скачивания). start вызывается после всех проверок
// и перед первой записью: до него ответ ещё можно заменить ошибкой.
func (s *Service) WriteLogs(ctx context.Context, userID, id int64, kind string, start func(), w io.Writer) error {
	site, err := s.Get(ctx, userID, id)
	if err != nil {
		return err
	}
	if s.logDir == "" {
		return ErrLogsOff
	}
	if kind != sitelog.KindAccess && kind != sitelog.KindError {
		return ErrLogsBadQuery
	}
	start()
	return sitelog.WriteText(s.logDir, site.Host, kind, site.CreatedAt, w)
}
