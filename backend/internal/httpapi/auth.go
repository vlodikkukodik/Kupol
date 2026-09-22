package httpapi

import (
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"kupol/internal/accounts"
	"kupol/internal/i18n"
)

type RoleDTO struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func toRoleDTOs(roles []accounts.Role, lang i18n.Lang) []RoleDTO {
	out := make([]RoleDTO, len(roles))
	for i, r := range roles {
		out[i] = RoleDTO{ID: string(r), Name: r.NameIn(lang)}
	}
	return out
}

type UserDTO struct {
	Login       string    `json:"login"`
	Level       int       `json:"level"`
	LevelName   string    `json:"level_name"`
	Directorate bool      `json:"directorate"`
	CreatedAt   time.Time `json:"created_at"`
	// Roles — роли команды; Capabilities — что пользователю разрешено (готовый список: интерфейс
	// сам права из ролей не выводит, решает сервер).
	Roles        []RoleDTO `json:"roles"`
	Capabilities []string  `json:"capabilities"`
	// TOTPEnabled — включён ли вход с кодом из приложения.
	TOTPEnabled bool `json:"totp_enabled"`
	// XP, LoginStreak — очки опыта и серия ежедневных входов подряд (шаг 5.1).
	XP          int `json:"xp"`
	LoginStreak int `json:"login_streak"`
	// NextLevelXP — сколько XP нужно для следующего уровня; 0 — дальше только решением Особого Совета.
	NextLevelXP int `json:"next_level_xp"`
}

func toUserDTO(u accounts.User, lang i18n.Lang) UserDTO {
	caps := []string{}
	for _, c := range u.Capabilities() {
		caps = append(caps, string(c))
	}
	return UserDTO{
		Login:        u.Login,
		Level:        u.Level,
		LevelName:    u.LevelNameIn(lang),
		Directorate:  u.Directorate,
		CreatedAt:    u.CreatedAt.UTC(),
		Roles:        toRoleDTOs(u.Roles, lang),
		Capabilities: caps,
		TOTPEnabled:  u.TOTPEnabled(),
		XP:           u.XP,
		LoginStreak:  u.LoginStreak,
		NextLevelXP:  u.NextLevelXP(),
	}
}

type authHandlers struct {
	svc    *accounts.Service
	secure bool
	log    *slog.Logger
}

func clientInfo(c *gin.Context) accounts.ClientInfo {
	return accounts.ClientInfo{IP: ClientIP(c), UserAgent: c.GetHeader("User-Agent")}
}

// fail переводит ошибку сервиса в ответ API. Неизвестные ошибки логируются и превращаются в 500:
// подробности клиенту не отдаются.
func (h *authHandlers) fail(c *gin.Context, err error, invalidCredentialsMsg string) {
	var (
		ve *accounts.ValidationError
		rl *accounts.RateLimitedError
	)
	switch {
	case errors.As(err, &ve):
		FailFields(c, http.StatusUnprocessableEntity, CodeValidation, "Проверьте поля формы", ve.Fields)
	case errors.As(err, &rl):
		FailRateLimited(c, rl.RetryAfter)
	case errors.Is(err, accounts.ErrLoginTaken):
		FailFields(c, http.StatusConflict, CodeLoginTaken, "Этот логин уже занят", map[string]string{"login": "Этот логин уже занят"})
	case errors.Is(err, accounts.ErrCaptcha):
		FailFields(c, http.StatusUnprocessableEntity, CodeCaptchaFailed, "Неверный ответ на вопрос анкеты",
			map[string]string{"captcha_answer": "Неверный ответ. Вопрос обновлён — ответьте на новый."})
	case errors.Is(err, accounts.ErrInvalidCredentials):
		Fail(c, http.StatusUnauthorized, CodeInvalidCredentials, invalidCredentialsMsg)
	case errors.Is(err, accounts.ErrTOTPRequired):
		Fail(c, http.StatusUnauthorized, CodeTOTPRequired, "Введите код из приложения-аутентификатора")
	case errors.Is(err, accounts.ErrTOTPInvalid):
		FailFields(c, http.StatusUnprocessableEntity, CodeTOTPInvalid, "Неверный код", map[string]string{"totp": "Неверный или уже использованный код"})
	case errors.Is(err, accounts.ErrTOTPAlreadyEnabled):
		Fail(c, http.StatusConflict, CodeTOTPAlreadyEnabled, "Код из приложения уже включён")
	case errors.Is(err, accounts.ErrTOTPNotEnabled):
		Fail(c, http.StatusConflict, CodeTOTPNotEnabled, "Код из приложения не включён")
	case errors.Is(err, accounts.ErrWrongPassword):
		FailFields(c, http.StatusForbidden, CodeWrongPassword, "Неверный пароль", map[string]string{"current_password": "Неверный пароль"})
	case errors.Is(err, accounts.ErrNoSession):
		Fail(c, http.StatusUnauthorized, CodeUnauthenticated, "Требуется вход")
	default:
		h.log.Error("ошибка обработчика аккаунтов", "err", err, "path", c.Request.URL.Path, "request_id", RequestID(c))
		Fail(c, http.StatusInternalServerError, CodeInternal, "Сбой архива")
	}
}

// GET /api/auth/session — кто я. Для Гражданина (без входа) отвечает 200 с user: null:
// 401 на каждой загрузке страницы засорял бы консоль браузера и журналы.
func (h *authHandlers) session(c *gin.Context) {
	if a := CurrentAuth(c); a != nil {
		u := toUserDTO(a.User, Lang(c))
		c.JSON(http.StatusOK, SessionResponse{User: &u})
		return
	}
	c.JSON(http.StatusOK, SessionResponse{})
}

// GET /api/auth/captcha — вопрос анкеты для регистрации.
func (h *authHandlers) captcha(c *gin.Context) {
	cp, err := h.svc.NewCaptcha(c.Request.Context())
	if err != nil {
		h.fail(c, err, "")
		return
	}
	c.JSON(http.StatusOK, CaptchaResponse{ID: cp.ID, Question: cp.Question})
}

type RegisterRequest struct {
	Login         string `json:"login"`
	Password      string `json:"password"`
	CaptchaID     string `json:"captcha_id"`
	CaptchaAnswer string `json:"captcha_answer"`
}

// POST /api/auth/register
func (h *authHandlers) register(c *gin.Context) {
	var req RegisterRequest
	if !bindJSON(c, &req) {
		return
	}
	res, err := h.svc.Register(c.Request.Context(), accounts.RegisterInput{
		Login: req.Login, Password: req.Password, CaptchaID: req.CaptchaID, CaptchaAnswer: req.CaptchaAnswer,
	}, clientInfo(c))
	if err != nil {
		h.fail(c, err, "")
		return
	}
	setSessionCookie(c, res.Token, res.ExpiresAt, h.secure)
	c.JSON(http.StatusCreated, RegisterResponse{User: toUserDTO(res.User, Lang(c)), BackupCode: res.BackupCode})
}

type LoginRequest struct {
	Login    string `json:"login"`
	Password string `json:"password"`
	// TOTP — код из приложения или одноразовый код; нужен, только если у пользователя включена защита кодом
	// (без него сервер отвечает 401 totp_required — после проверки пароля).
	TOTP string `json:"totp,omitempty"`
}

// POST /api/auth/login
func (h *authHandlers) login(c *gin.Context) {
	var req LoginRequest
	if !bindJSON(c, &req) {
		return
	}
	res, err := h.svc.LoginWithCode(c.Request.Context(), req.Login, req.Password, req.TOTP, clientInfo(c))
	if err != nil {
		h.fail(c, err, "Неверный логин или пароль")
		return
	}
	setSessionCookie(c, res.Token, res.ExpiresAt, h.secure)
	c.JSON(http.StatusOK, LoginResponse{User: toUserDTO(res.User, Lang(c)), LevelUp: res.LevelUp})
}

// POST /api/auth/logout — завершает текущую сессию. Без сессии тоже успешно: выход идемпотентен.
func (h *authHandlers) logout(c *gin.Context) {
	if token, err := c.Cookie(SessionCookieName); err == nil && token != "" {
		if err := h.svc.Logout(c.Request.Context(), token); err != nil {
			h.fail(c, err, "")
			return
		}
	}
	clearSessionCookie(c, h.secure)
	c.Status(http.StatusNoContent)
}

type RestoreRequest struct {
	Login       string `json:"login"`
	BackupCode  string `json:"backup_code"`
	NewPassword string `json:"new_password"`
}

// POST /api/auth/restore — новый пароль по резервному коду; отвечает НОВЫМ резервным кодом.
func (h *authHandlers) restore(c *gin.Context) {
	var req RestoreRequest
	if !bindJSON(c, &req) {
		return
	}
	res, err := h.svc.RestoreAccess(c.Request.Context(), req.Login, req.BackupCode, req.NewPassword, clientInfo(c))
	if err != nil {
		h.fail(c, err, "Неверный логин или резервный код")
		return
	}
	resp := RestoreResponse{BackupCode: res.BackupCode}
	// У кого включён код из приложения, тот после восстановления входит обычным путём — с новым паролем и кодом.
	if res.Token != "" {
		setSessionCookie(c, res.Token, res.ExpiresAt, h.secure)
		u := toUserDTO(res.User, Lang(c))
		resp.User = &u
	}
	c.JSON(http.StatusOK, resp)
}

type ChangePasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

// POST /api/me/password
func (h *authHandlers) changePassword(c *gin.Context) {
	var req ChangePasswordRequest
	if !bindJSON(c, &req) {
		return
	}
	a := CurrentAuth(c)
	if err := h.svc.ChangePassword(c.Request.Context(), a.User.ID, a.Session.ID, req.CurrentPassword, req.NewPassword, clientInfo(c)); err != nil {
		h.fail(c, err, "")
		return
	}
	c.Status(http.StatusNoContent)
}

type DeleteAccountRequest struct {
	Password string `json:"password"`
}

// DELETE /api/me — «сдать дело в архив»: полное удаление аккаунта.
func (h *authHandlers) deleteAccount(c *gin.Context) {
	var req DeleteAccountRequest
	if !bindJSON(c, &req) {
		return
	}
	a := CurrentAuth(c)
	if err := h.svc.DeleteAccount(c.Request.Context(), a.User.ID, req.Password, clientInfo(c)); err != nil {
		h.fail(c, err, "")
		return
	}
	clearSessionCookie(c, h.secure)
	c.Status(http.StatusNoContent)
}
