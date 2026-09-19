package httpapi

import (
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"kupol/internal/accounts"
)

// SessionCookieName — имя куки сессии. Кука host-only (без Domain): браузер отдаёт её только
// сайту, с которого она получена, то есть kupol.vladinc.ru. На api-поддомен она не уходит —
// поэтому WebSocket и загрузки авторизуются одноразовым тикетом, а не кукой.
const SessionCookieName = "kupol_session"

const ctxAuth = "kupol.auth"

// CurrentAuth возвращает вошедшего пользователя и его сессию или nil, если посетитель — Гражданин.
func CurrentAuth(c *gin.Context) *accounts.Authenticated {
	v, ok := c.Get(ctxAuth)
	if !ok {
		return nil
	}
	a, _ := v.(*accounts.Authenticated)
	return a
}

func setSessionCookie(c *gin.Context, token string, expires time.Time, secure bool) {
	maxAge := int(time.Until(expires).Seconds())
	if maxAge < 1 {
		maxAge = 1
	}
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     SessionCookieName,
		Value:    token,
		Path:     "/",
		Expires:  expires,
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
}

func clearSessionCookie(c *gin.Context, secure bool) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     SessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
}

// sessionMiddleware определяет вошедшего пользователя по куке. Недействительная кука
// (просрочена, отозвана, подделана) стирается. Сбой БД — 500, а не «Гражданин»: иначе
// авария выглядела бы как массовый выход из аккаунтов.
func sessionMiddleware(svc *accounts.Service, secure bool, log *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		token, err := c.Cookie(SessionCookieName)
		if err != nil { // куки нет вовсе — Гражданин
			c.Next()
			return
		}
		a, err := svc.Authenticate(c.Request.Context(), token) // пустой токен даёт ErrNoSession и стирание куки
		switch {
		case err == nil:
			c.Set(ctxAuth, a)
			if a.Renewed {
				setSessionCookie(c, token, a.Session.ExpiresAt, secure)
			}
		case errors.Is(err, accounts.ErrNoSession):
			clearSessionCookie(c, secure)
		default:
			log.Error("не удалось проверить сессию", "err", err, "request_id", RequestID(c))
			Fail(c, http.StatusInternalServerError, CodeInternal, "Сбой архива")
			return
		}
		c.Next()
	}
}

// requireCapability пропускает только тех, у кого есть право (роль команды или Директорат).
// Гражданину — 401 (нужен вход), вошедшему без права — 403. Право проверяется по данным, которые
// Authenticate читает из БД на каждый запрос, поэтому выданная или снятая роль действует сразу.
func requireCapability(c accounts.Capability) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		a := CurrentAuth(ctx)
		if a == nil {
			Fail(ctx, http.StatusUnauthorized, CodeUnauthenticated, "Требуется вход")
			return
		}
		if !a.User.Can(c) {
			Fail(ctx, http.StatusForbidden, CodeForbidden, "Недостаточно прав")
			return
		}
		ctx.Next()
	}
}

// requireAuth пропускает только вошедших.
func requireAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		if CurrentAuth(c) == nil {
			Fail(c, http.StatusUnauthorized, CodeUnauthenticated, "Требуется вход")
			return
		}
		c.Next()
	}
}
