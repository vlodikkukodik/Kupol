package accounts

import (
	"context"

	"kupol/internal/achievements"
	"kupol/internal/i18n"
)

// Achievements — грамоты пользователя (шаг 5.6), для личного дела.
func (s *Service) Achievements(ctx context.Context, userID int64, lang i18n.Lang) ([]achievements.Item, error) {
	return achievements.List(ctx, s.db, userID, lang)
}
