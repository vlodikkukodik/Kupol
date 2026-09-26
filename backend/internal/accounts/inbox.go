package accounts

import (
	"context"

	"gorm.io/gorm"

	"kupol/internal/audit"
	"kupol/internal/i18n"
	"kupol/internal/inbox"
)

// Inbox — внутренняя почта пользователя (шаг 5.7): страница ящика на языке читателя.
func (s *Service) Inbox(ctx context.Context, userID int64, page int, lang i18n.Lang) (*inbox.Page, error) {
	return inbox.List(ctx, s.db, userID, page, lang)
}

// InboxUnread — число непрочитанных записок (для значка в меню).
func (s *Service) InboxUnread(ctx context.Context, userID int64) (int64, error) {
	return inbox.Unread(ctx, s.db, userID)
}

// InboxMarkRead помечает записку прочитанной; ErrUserNotFound (нет такой записки у этого пользователя) — чужие id неотличимы от несуществующих.
func (s *Service) InboxMarkRead(ctx context.Context, userID, id int64) error {
	ok, err := inbox.MarkRead(ctx, s.db, userID, id, s.now())
	if err != nil {
		return err
	}
	if !ok {
		return ErrUserNotFound
	}
	return nil
}

// InboxMarkAllRead помечает прочитанными все записки.
func (s *Service) InboxMarkAllRead(ctx context.Context, userID int64) error {
	return inbox.MarkAllRead(ctx, s.db, userID, s.now())
}

// InboxSendNote — записка Директората одному пользователю (login) или всем (login == ""). Право проверяет вызывающий
// (Директорат — флаг пользователя, не роль). Возвращает число получателей; ErrUserNotFound — логина нет.
func (s *Service) InboxSendNote(ctx context.Context, actorID int64, login string, n inbox.Note) (int64, error) {
	var count int64
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		now := s.now()
		var err error
		if count, err = inbox.SendNote(ctx, tx, login, n, now); err != nil {
			return err
		}
		if count == 0 {
			return ErrUserNotFound
		}
		return audit.Record(tx, now, audit.InboxNoteSent, audit.Event{
			ActorID: &actorID, Details: audit.Details("to", login, "recipients", count, "title", n.Title),
		})
	})
	return count, err
}
