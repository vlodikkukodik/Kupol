package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"kupol/internal/ratelimit"
)

// maxJSONBody — лимит тела запросов аккаунтов: логин, пароль, код — это сотни байт.
const maxJSONBody = 16 << 10

// ipFloodFactor: до проверки сессии (она стоит запрос в БД) на IP действует лимит в это число раз
// выше пользовательского — защита БД от потока запросов с левыми куками, не задевающая
// многих настоящих пользователей за одним NAT.
const ipFloodFactor = 10

// csrfMiddleware защищает изменяющие запросы от подделки с чужих сайтов.
//
// Запрос с методом POST/PUT/PATCH/DELETE принимается, если его Origin — наш сайт. Запрос без Origin
// допустим только когда у него нет куки сессии (curl, скрипты: у них нет «чужих» полномочий);
// браузер же всегда шлёт Origin на такие запросы. Вторая линия обороны — SameSite=Lax у куки.
func csrfMiddleware(siteOrigin string) gin.HandlerFunc {
	return func(c *gin.Context) {
		switch c.Request.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			c.Next()
			return
		}
		origin := c.GetHeader("Origin")
		if origin != "" {
			if origin != siteOrigin {
				Fail(c, http.StatusForbidden, CodeForbiddenOrigin, "Запрос с чужого сайта отклонён")
				return
			}
		} else if _, err := c.Cookie(SessionCookieName); err == nil {
			Fail(c, http.StatusForbidden, CodeForbiddenOrigin, "Запрос без Origin отклонён")
			return
		}
		c.Next()
	}
}

// ipFloodMiddleware — грубый лимит по IP до обращения к БД.
func ipFloodMiddleware(l *ratelimit.Limiter, perMinute int) gin.HandlerFunc {
	limit := perMinute * ipFloodFactor
	return func(c *gin.Context) {
		if ok, retry := l.Allow("flood:"+ClientIP(c), limit, time.Minute); !ok {
			FailRateLimited(c, retry)
			return
		}
		c.Next()
	}
}

// userRateLimitMiddleware — общий лимит API: на пользователя, а для Гражданина — на IP.
func userRateLimitMiddleware(l *ratelimit.Limiter, perMinute int) gin.HandlerFunc {
	return func(c *gin.Context) {
		key := "api:ip:" + ClientIP(c)
		if a := CurrentAuth(c); a != nil {
			key = "api:user:" + strconv.FormatInt(a.User.ID, 10)
		}
		if ok, retry := l.Allow(key, perMinute, time.Minute); !ok {
			FailRateLimited(c, retry)
			return
		}
		c.Next()
	}
}

// bindJSON разбирает тело запроса строго: Content-Type application/json, не больше maxJSONBody,
// неизвестные поля и мусор после значения — ошибка. При ошибке сам отвечает клиенту и возвращает false.
func bindJSON(c *gin.Context, dst any) bool { return bindJSONLimit(c, dst, maxJSONBody) }

// readJSONBody читает тело запроса с Content-Type: application/json, не больше limit байт; при ошибке сам отвечает.
func readJSONBody(c *gin.Context, limit int64) ([]byte, bool) {
	mt, _, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
	if err != nil || mt != "application/json" {
		Fail(c, http.StatusUnsupportedMediaType, CodeUnsupportedMedia, "Ожидается Content-Type: application/json")
		return nil, false
	}
	body, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, limit))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			Fail(c, http.StatusRequestEntityTooLarge, CodeTooLarge, "Слишком большой запрос")
			return nil, false
		}
		Fail(c, http.StatusBadRequest, CodeBadRequest, "Не удалось прочитать запрос")
		return nil, false
	}
	return body, true
}

// bindJSONLimit разбирает тело строго: неизвестные поля и лишнее после JSON — ошибка. Документы крупнее форм
// аккаунтов, поэтому предел задаёт вызывающий.
func bindJSONLimit(c *gin.Context, dst any, limit int64) bool {
	body, ok := readJSONBody(c, limit)
	if !ok {
		return false
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		Fail(c, http.StatusBadRequest, CodeBadRequest, "Некорректный запрос")
		return false
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		Fail(c, http.StatusBadRequest, CodeBadRequest, "Некорректный запрос")
		return false
	}
	return true
}
