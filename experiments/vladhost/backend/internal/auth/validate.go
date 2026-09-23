package auth

import (
	"errors"
	"net/mail"
	"regexp"
	"strings"
)

// ValidationError — ошибка ввода с кодом поля, чтобы фронт показал её у нужного поля.
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string { return e.Field + ": " + e.Message }

var (
	ErrInvalidInvite      = errors.New("инвайт недействителен или уже использован")
	ErrEmailTaken         = errors.New("этот email уже зарегистрирован")
	ErrUsernameTaken      = errors.New("это имя уже занято")
	ErrInvalidCredentials = errors.New("неверный логин или пароль")
	ErrInvalidToken       = errors.New("сессия недействительна")
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

func normalizeEmail(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	addr, err := mail.ParseAddress(raw)
	if err != nil || addr.Address != raw || len(raw) > 254 {
		return "", &ValidationError{"email", "некорректный email"}
	}
	return strings.ToLower(raw), nil
}

func normalizeUsername(raw string) (string, error) {
	name := strings.ToLower(strings.TrimSpace(raw))
	if !usernameRe.MatchString(name) {
		return "", &ValidationError{"username", "3–32 символа: латиница, цифры и дефис, не с дефиса и не на дефис"}
	}
	if strings.Contains(name, "--") {
		return "", &ValidationError{"username", "два дефиса подряд недопустимы"}
	}
	if reservedUsernames[name] {
		return "", &ValidationError{"username", "это имя зарезервировано"}
	}
	return name, nil
}

func validatePassword(pw string) error {
	if len(pw) < minPasswordLen {
		return &ValidationError{"password", "пароль короче 8 символов"}
	}
	if len(pw) > maxPasswordLen {
		return &ValidationError{"password", "пароль длиннее 72 байт"}
	}
	return nil
}
