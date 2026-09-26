package auth

import "context"

// UserIDByLogin возвращает номер аккаунта по email или имени (0, если такого нет). Нужен журналу действий: неудачный вход записывается тому,
// чей аккаунт пытались открыть; ответ пользователю при этом не зависит от результата.
func (s *Service) UserIDByLogin(ctx context.Context, login string) int64 {
	var id int64
	if err := s.db.WithContext(ctx).Model(&User{}).Where("email = ? OR username = ?", lower(login), lower(login)).Limit(1).Pluck("id", &id).Error; err != nil {
		return 0
	}
	return id
}

// SessionUser возвращает номер аккаунта, которому принадлежит действующий refresh-токен (0 — токен неизвестен или отозван).
func (s *Service) SessionUser(ctx context.Context, raw string) int64 {
	var id int64
	if err := s.db.WithContext(ctx).Model(&RefreshToken{}).Where("token_hash = ? AND revoked_at IS NULL", hashToken(raw)).Limit(1).Pluck("user_id", &id).Error; err != nil {
		return 0
	}
	return id
}
