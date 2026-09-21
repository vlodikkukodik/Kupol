package httpapi

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log/slog"
	"net/http"
	"regexp"
	"runtime/debug"
	"time"

	"github.com/gin-gonic/gin"

	"kupol/internal/i18n"
	"kupol/internal/proxyauth"
)

const (
	ctxRequestID = "kupol.request_id"
	ctxClientIP  = "kupol.client_ip"
	ctxIPSource  = "kupol.ip_source"

	HeaderRequestID = "X-Request-Id"

	// MaxBodyBytes — лимит тела обычных JSON-запросов. Загрузки файлов идут
	// отдельным маршрутом с собственным лимитом.
	MaxBodyBytes = 1 << 20

	IPSourceProxy  = "proxy"  // IP подтверждён подписью PHP-прокси
	IPSourceDirect = "direct" // IP из TCP-соединения / доверенного обратного прокси
)

var requestIDPattern = regexp.MustCompile(`^[A-Za-z0-9-]{8,64}$`)

func RequestID(c *gin.Context) string { return c.GetString(ctxRequestID) }
func ClientIP(c *gin.Context) string  { return c.GetString(ctxClientIP) }
func IPSource(c *gin.Context) string  { return c.GetString(ctxIPSource) }

// Lang — язык ответа: определяется по Accept-Language (его шлёт интерфейс сайта); нет заголовка — русский.
func Lang(c *gin.Context) i18n.Lang { return i18n.From(c.Request.Context()) }

// languageMiddleware кладёт язык запроса в контекст запроса — им пользуются и обработчики, и сервисы (i18n.From) — и в ответе
// сообщает Content-Language. Ответы зависят от языка, поэтому кэшам сообщается Vary.
func languageMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		lang := i18n.Parse(c.GetHeader("Accept-Language"))
		c.Request = c.Request.WithContext(i18n.WithLang(c.Request.Context(), lang))
		c.Header("Content-Language", lang.Tag())
		c.Writer.Header().Add("Vary", "Accept-Language")
		c.Next()
	}
}

func requestIDMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.GetHeader(HeaderRequestID)
		if !requestIDPattern.MatchString(id) {
			var b [8]byte
			_, _ = rand.Read(b[:])
			id = hex.EncodeToString(b[:])
		}
		c.Set(ctxRequestID, id)
		c.Header(HeaderRequestID, id)
		c.Next()
	}
}

// proxyTrustMiddleware определяет IP клиента.
//   - Есть валидная подпись PHP-прокси → IP из подписанного заголовка.
//   - Заголовков подписи нет → запрос пришёл напрямую (WebSocket, загрузки):
//     IP из соединения, X-Forwarded-For учитывается только от TrustedProxies.
//   - Подпись есть, но неверна или просрочена → 401: либо рассинхрон часов/секрета
//     на хостинге (иначе все пользователи молча сольются в один IP), либо подделка.
//
// Служебные заголовки подписи после проверки удаляются.
func proxyTrustMiddleware(secret []byte, log *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		h := c.Request.Header
		ipHdr, tsHdr, sigHdr := h.Get(proxyauth.HeaderIP), h.Get(proxyauth.HeaderTimestamp), h.Get(proxyauth.HeaderSignature)
		h.Del(proxyauth.HeaderIP)
		h.Del(proxyauth.HeaderTimestamp)
		h.Del(proxyauth.HeaderSignature)

		ip, err := proxyauth.Verify(secret, time.Now(), ipHdr, tsHdr, sigHdr, c.Request.Method, c.Request.RequestURI)
		switch {
		case err == nil:
			c.Set(ctxClientIP, ip.String())
			c.Set(ctxIPSource, IPSourceProxy)
		case errors.Is(err, proxyauth.ErrMissing):
			c.Set(ctxClientIP, c.ClientIP())
			c.Set(ctxIPSource, IPSourceDirect)
		default:
			log.Error("подпись PHP-прокси отклонена",
				"err", err, "remote", c.Request.RemoteAddr, "request_id", RequestID(c), "path", c.Request.URL.Path)
			Fail(c, http.StatusUnauthorized, CodeBadProxySig, "Подпись прокси недействительна")
			return
		}
		c.Next()
	}
}

func recoveryMiddleware(log *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			rec := recover()
			if rec == nil {
				return
			}
			if rec == http.ErrAbortHandler { //nolint:errorlint // sentinel по контракту net/http
				panic(rec)
			}
			log.Error("паника в обработчике",
				"panic", rec, "request_id", RequestID(c),
				"method", c.Request.Method, "path", c.Request.URL.Path,
				"stack", string(debug.Stack()))
			if !c.Writer.Written() {
				Fail(c, http.StatusInternalServerError, CodeInternal, "Сбой архива")
			} else {
				c.Abort()
			}
		}()
		c.Next()
	}
}

func accessLogMiddleware(log *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()

		status := c.Writer.Status()
		level := slog.LevelInfo
		switch {
		case status >= 500:
			level = slog.LevelError
		case status >= 400:
			level = slog.LevelWarn
		case isHealthPath(c.Request.URL.Path):
			level = slog.LevelDebug
		}
		// query намеренно не логируется: в нём могут быть тикеты и токены.
		log.Log(c.Request.Context(), level, "http",
			"method", c.Request.Method,
			"path", c.Request.URL.Path,
			"route", c.FullPath(),
			"status", status,
			"bytes", c.Writer.Size(),
			"took", time.Since(start),
			"ip", ClientIP(c),
			"ip_source", IPSource(c),
			"request_id", RequestID(c),
		)
	}
}

func isHealthPath(p string) bool { return p == "/health" || p == "/api/health" }

func securityHeadersMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		h := c.Writer.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Cache-Control", "no-store")
		c.Next()
	}
}

func bodyLimitMiddleware(limit int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.ContentLength > limit {
			Fail(c, http.StatusRequestEntityTooLarge, CodeTooLarge, "Слишком большой запрос")
			return
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, limit)
		c.Next()
	}
}
