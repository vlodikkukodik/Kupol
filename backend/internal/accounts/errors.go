package accounts

import (
	"errors"
	"fmt"
	"time"
)

var (
	// ErrInvalidCredentials — неверный логин или пароль (или резервный код). Причина намеренно не раскрывается.
	ErrInvalidCredentials = errors.New("accounts: неверные учётные данные")
	// ErrWrongPassword — пользователь вошёл, но не смог подтвердить текущий пароль (смена пароля, удаление).
	ErrWrongPassword = errors.New("accounts: неверный текущий пароль")
	// ErrBanned — аккаунт заблокирован (шаг 5.9); сообщается только тому, кто ввёл верный пароль.
	ErrBanned = errors.New("accounts: аккаунт заблокирован")
	// ErrLoginTaken — логин занят.
	ErrLoginTaken = errors.New("accounts: логин занят")
	// ErrCaptcha — неверный, просроченный или уже использованный ответ на анкету.
	ErrCaptcha = errors.New("accounts: неверный ответ на анкету")
	// ErrNoSession — сессии нет, она просрочена или отозвана.
	ErrNoSession = errors.New("accounts: нет действующей сессии")
	// ErrUserNotFound — пользователя с таким логином нет (административные операции).
	ErrUserNotFound = errors.New("accounts: пользователь не найден")
	// ErrEmailTaken — почта уже подтверждена другим аккаунтом.
	ErrEmailTaken = errors.New("accounts: почта занята")
	// ErrEmailTokenInvalid — ссылка подтверждения недействительна, устарела или уже использована.
	ErrEmailTokenInvalid = errors.New("accounts: ссылка подтверждения недействительна или устарела")
)

// ValidationError — ошибки в полях формы; ключ — имя поля в JSON.
type ValidationError struct {
	Fields map[string]string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("accounts: ошибки в полях: %v", e.Fields)
}

func fieldError(field, message string) *ValidationError {
	return &ValidationError{Fields: map[string]string{field: message}}
}

// RateLimitedError — превышен лимит частоты; повторить можно через RetryAfter.
type RateLimitedError struct {
	RetryAfter time.Duration
}

func (e *RateLimitedError) Error() string {
	return fmt.Sprintf("accounts: слишком часто, повторить через %s", e.RetryAfter.Round(time.Second))
}
