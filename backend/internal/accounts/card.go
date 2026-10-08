package accounts

import (
	"context"

	"kupol/internal/achievements"
	"kupol/internal/i18n"
)

// UserCard — ограниченная карточка пользователя для других читателей (спецификация §4): ник, уровень, звание, грамоты.
// История чтения, закладки, XP, почта и роли команды не раскрываются.
type UserCard struct {
	Login        string              `json:"login"`
	Level        int                 `json:"level"`
	LevelName    string              `json:"level_name"`
	Directorate  bool                `json:"directorate"`
	Achievements []achievements.Item `json:"achievements"`
}

// Card возвращает карточку по логину (без учёта регистра) или ErrUserNotFound.
func (s *Service) Card(ctx context.Context, login string, lang i18n.Lang) (*UserCard, error) {
	u, err := s.findByLogin(ctx, login)
	if err != nil {
		return nil, err
	}
	items, err := achievements.List(ctx, s.db, u.ID, lang)
	if err != nil {
		return nil, err
	}
	return &UserCard{Login: u.Login, Level: u.Level, LevelName: u.LevelNameIn(lang), Directorate: u.Directorate, Achievements: items}, nil
}
