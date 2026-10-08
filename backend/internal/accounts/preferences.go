package accounts

import (
	"context"

	"kupol/internal/i18n"
	"kupol/internal/inbox"
)

// EmailPrefsGet — о чём читатель хочет получать письма-уведомления (копии записок внутренней почты).
func (s *Service) EmailPrefsGet(ctx context.Context, userID int64) (inbox.EmailPrefs, error) {
	return inbox.EmailPrefsGet(ctx, s.db, userID)
}

// EmailPrefsSet — сохранить настройки писем читателя.
func (s *Service) EmailPrefsSet(ctx context.Context, userID int64, p inbox.EmailPrefs) error {
	return inbox.EmailPrefsSet(ctx, s.db, userID, p)
}

// SetLang — язык писем, выбранный в интерфейсе: сервер запоминает его для уведомлений, которые уходят
// без запроса читателя. Сам интерфейс и ошибки по-прежнему живут на языке Accept-Language.
func (s *Service) SetLang(ctx context.Context, userID int64, lang string) error {
	if !i18n.Lang(lang).Valid() {
		return fieldError("lang", i18n.From(ctx).Translate("Неизвестный язык"))
	}
	return s.db.WithContext(ctx).Model(&User{}).Where("id = ?", userID).Update("lang", lang).Error
}
