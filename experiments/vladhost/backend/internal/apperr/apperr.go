// Package apperr: ошибки домена с кодом. Код — ключ в каталоге сообщений ("err."+код), поэтому текст
// для пользователя не зашит в ошибку: HTTP-слой и FTP подбирают его по языку клиента.
package apperr

import "net/http"

// Error — ошибка, о которой можно рассказать пользователю.
type Error struct {
	Status int    // HTTP-статус ответа API
	Code   string // стабильный код; по нему клиенты API отличают ошибки
	Field  string // поле формы, к которому относится ошибка
	Args   []any  // подстановки в текст из каталога
	msg    string // техническое описание для логов (по-английски)
}

func New(status int, code, msg string) *Error {
	return &Error{Status: status, Code: code, msg: msg}
}

func (e *Error) Error() string { return e.msg }

// Is сравнивает по коду: так errors.Is работает и для копий с другими аргументами.
func (e *Error) Is(target error) bool {
	t, ok := target.(*Error)
	return ok && t.Code == e.Code
}

// With возвращает копию с подстановками для текста (числа, имена файлов).
func (e *Error) With(args ...any) *Error {
	c := *e
	c.Args = args
	return &c
}

// OnField возвращает копию, привязанную к полю формы.
func (e *Error) OnField(field string) *Error {
	c := *e
	c.Field = field
	return &c
}

// Validation — ошибка проверки введённых данных.
func Validation(field, code, msg string) *Error {
	return New(http.StatusUnprocessableEntity, "validation."+code, msg).OnField(field)
}
