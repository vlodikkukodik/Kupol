package sites

import (
	"context"
	"time"

	"vladhost/internal/apperr"
	"vladhost/internal/sitestats"
)

var ErrStatsBadPeriod = apperr.Validation("days", "stats_period", "invalid statistics period")

// Stats возвращает статистику своего сайта за days суток (7, 30 или 90). Данные до создания сайта не показываются:
// адрес мог принадлежать прежнему сайту с тем же именем.
func (s *Service) Stats(ctx context.Context, userID, id int64, days int) (sitestats.Summary, error) {
	site, err := s.Get(ctx, userID, id)
	if err != nil {
		return sitestats.Summary{}, err
	}
	if s.logDir == "" {
		return sitestats.Summary{}, ErrLogsOff
	}
	if !sitestats.ValidPeriod(days) {
		return sitestats.Summary{}, ErrStatsBadPeriod
	}
	return sitestats.Read(s.logDir, site.Host, days, site.CreatedAt, time.Now())
}
