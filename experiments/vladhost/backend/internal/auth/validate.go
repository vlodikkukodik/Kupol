package auth

import (
	"net/http"
	"net/mail"
	"regexp"
	"strings"

	"vladhost/internal/apperr"
)

// Ошибки домена. Текст для пользователя берётся из каталога i18n по коду.
var (
	ErrInvalidInvite      = apperr.New(http.StatusUnprocessableEntity, "invalid_invite", "invite is invalid or already used")
	ErrEmailTaken         = apperr.New(http.StatusConflict, "email_taken", "email already registered").OnField("email")
	ErrUsernameTaken      = apperr.New(http.StatusConflict, "username_taken", "username already taken").OnField("username")
	ErrInvalidCredentials = apperr.New(http.StatusUnauthorized, "invalid_credentials", "invalid login or password")
	ErrInvalidToken       = apperr.New(http.StatusUnauthorized, "unauthorized", "session is invalid")
	ErrWrongPassword      = apperr.New(http.StatusUnprocessableEntity, "wrong_password", "current password is wrong").OnField("current_password")
	ErrPasswordSame       = apperr.New(http.StatusUnprocessableEntity, "password_same", "new password equals the current one").OnField("new_password")
)

// Имя пользователя становится DNS-меткой в {site}.{user}.vladinc.ru.
var usernameRe = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{1,30})[a-z0-9]$`)

// Имена, которые нельзя отдавать: они совпали бы со служебными хостами.
var reservedUsernames = map[string]bool{
	"app": true, "www": true, "api": true, "admin": true, "root": true, "mail": true,
	"smtp": true, "imap": true, "ftp": true, "ns": true, "ns1": true, "ns2": true,
	"kupol": true, "registro": true, "status": true, "docs": true, "static": true,
	"cdn": true, "dev": true, "test": true, "support": true, "help": true, "vladhost": true,
}

const (
	minPasswordLen = 8
	maxPasswordLen = 72 // предел bcrypt
)

// applyField привязывает ошибку проверки к другому полю формы (при смене пароля поле называется new_password).
func applyField(err error, field string) error {
	if ae, ok := err.(*apperr.Error); ok {
		return ae.OnField(field)
	}
	return err
}

func normalizeEmail(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	addr, err := mail.ParseAddress(raw)
	if err != nil || addr.Address != raw || len(raw) > 254 {
		return "", apperr.Validation("email", "email", "invalid email")
	}
	return strings.ToLower(raw), nil
}

func normalizeUsername(raw string) (string, error) {
	name := strings.ToLower(strings.TrimSpace(raw))
	if !usernameRe.MatchString(name) {
		return "", apperr.Validation("username", "username_format", "invalid username format")
	}
	if strings.Contains(name, "--") {
		return "", apperr.Validation("username", "username_dashes", "username has consecutive dashes")
	}
	if reservedUsernames[name] {
		return "", apperr.Validation("username", "username_reserved", "username is reserved")
	}
	return name, nil
}

func validatePassword(pw string) error {
	if len(pw) < minPasswordLen {
		return apperr.Validation("password", "password_short", "password is too short")
	}
	if len(pw) > maxPasswordLen {
		return apperr.Validation("password", "password_long", "password is too long")
	}
	return nil
}
