package mail

import (
	"context"
)

// Dynamic — отправитель, который перед каждой отправкой спрашивает актуальные настройки SMTP.
// Нужен, потому что почту включает Директорат из панели команды (internal/mailsettings), а не только
// файл окружения: sender, собранный при запуске, устарел бы сразу после сохранения настроек.
//
// Load возвращает false, когда почта выключена (SMTP не задан или снят флаг): письмо просто не уходит —
// вызывающий код (accounts, очередь писем) не считает это ошибкой.
type Dynamic struct {
	Load func(ctx context.Context) (*SMTPSender, bool)
}

// Enabled — настроен ли почтовый сервер прямо сейчас.
func (d *Dynamic) Enabled(ctx context.Context) bool {
	_, ok := d.Load(ctx)
	return ok
}

// Send отправляет письмо через текущие настройки; выключенная почта — не ошибка, а «ничего не делаем».
func (d *Dynamic) Send(ctx context.Context, msg Message) error {
	s, ok := d.Load(ctx)
	if !ok {
		return nil
	}
	return s.Send(ctx, msg)
}
