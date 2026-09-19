package httpapi

import (
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"kupol/internal/accounts"
)

type roleDTO struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func toRoleDTOs(roles []accounts.Role) []roleDTO {
	out := make([]roleDTO, len(roles))
	for i, r := range roles {
		out[i] = roleDTO{ID: string(r), Name: r.Name()}
	}
	return out
}

type userDTO struct {
	Login       string    `json:"login"`
	Level       int       `json:"level"`
	LevelName   string    `json:"level_name"`
	Directorate bool      `json:"directorate"`
	CreatedAt   time.Time `json:"created_at"`
	// Roles — роли команды; Capabilities — что пользователю разрешено (готовый список: интерфейс
	// сам права из ролей не выводит, решает сервер).
	Roles        []roleDTO `json:"roles"`
	Capabilities []string  `json:"capabilities"`
}

func toUserDTO(u accounts.User) userDTO {
	caps := []string{}
	for _, c := range u.Capabilities() {
		caps = append(caps, string(c))
	}
	return userDTO{
		Login:        u.Login,
		Level:        u.Level,
		LevelName:    u.LevelName(),
		Directorate:  u.Directorate,
		CreatedAt:    u.CreatedAt.UTC(),
		Roles:        toRoleDTOs(u.Roles),
		Capabilities: caps,
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
		c.JSON(http.StatusOK, gin.H{"user": toUserDTO(a.User)})
		return
	}
	c.JSON(http.StatusOK, gin.H{"user": nil})
}

// GET /api/auth/captcha — вопрос анкеты для регистрации.
func (h *authHandlers) captcha(c *gin.Context) {
	cp, err := h.svc.NewCaptcha(c.Request.Context())
	if err != nil {
		h.fail(c, err, "")
		return
	}
	c.JSON(http.StatusOK, gin.H{"id": cp.ID, "question": cp.Question})
}

type registerRequest struct {
	Login         string `json:"login"`
	Password      string `json:"password"`
	CaptchaID     string `json:"captcha_id"`
	CaptchaAnswer string `json:"captcha_answer"`
}

// POST /api/auth/register
func (h *authHandlers) register(c *gin.Context) {
	var req registerRequest
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
	c.JSON(http.StatusCreated, gin.H{"user": toUserDTO(res.User), "backup_code": res.BackupCode})
}

type loginRequest struct {
	Login    string `json:"login"`
	Password string `json:"password"`
}

// POST /api/auth/login
func (h *authHandlers) login(c *gin.Context) {
	var req loginRequest
	if !bindJSON(c, &req) {
		return
	}
	res, err := h.svc.Login(c.Request.Context(), req.Login, req.Password, clientInfo(c))
	if err != nil {
		h.fail(c, err, "Неверный логин или пароль")
		return
	}
	setSessionCookie(c, res.Token, res.ExpiresAt, h.secure)
	c.JSON(http.StatusOK, gin.H{"user": toUserDTO(res.User)})
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

type restoreRequest struct {
	Login       string `json:"login"`
	BackupCode  string `json:"backup_code"`
	NewPassword string `json:"new_password"`
}

// POST /api/auth/restore — новый пароль по резервному коду; отвечает НОВЫМ резервным кодом.
func (h *authHandlers) restore(c *gin.Context) {
	var req restoreRequest
	if !bindJSON(c, &req) {
		return
	}
	res, err := h.svc.RestoreAccess(c.Request.Context(), req.Login, req.BackupCode, req.NewPassword, clientInfo(c))
	if err != nil {
		h.fail(c, err, "Неверный логин или резервный код")
		return
	}
	setSessionCookie(c, res.Token, res.ExpiresAt, h.secure)
	c.JSON(http.StatusOK, gin.H{"user": toUserDTO(res.User), "backup_code": res.BackupCode})
}

type changePasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

// POST /api/me/password
func (h *authHandlers) changePassword(c *gin.Context) {
	var req changePasswordRequest
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

type deleteAccountRequest struct {
	Password string `json:"password"`
}

// DELETE /api/me — «сдать дело в архив»: полное удаление аккаунта.
func (h *authHandlers) deleteAccount(c *gin.Context) {
	var req deleteAccountRequest
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
