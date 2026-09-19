package httpapi

import (
	"math"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"kupol/internal/documents"
)

// Коды ошибок API. Фронтенд ориентируется на code, а не на текст.
const (
	CodeNotFound         = "not_found"
	CodeMethodNotAllowed = "method_not_allowed"
	CodeInternal         = "internal"
	CodeBadProxySig      = "bad_proxy_signature"
	CodeTooLarge         = "payload_too_large"

	CodeBadRequest         = "bad_request"
	CodeUnsupportedMedia   = "unsupported_media_type"
	CodeForbiddenOrigin    = "forbidden_origin"
	CodeUnauthenticated    = "unauthenticated"
	CodeInvalidCredentials = "invalid_credentials"
	CodeWrongPassword      = "wrong_password"
	CodeValidation         = "validation"
	CodeLoginTaken         = "login_taken"
	CodeCaptchaFailed      = "captcha_failed"
	CodeRateLimited        = "rate_limited"
	CodeAccessDenied       = "access_denied"
	CodeForbidden          = "forbidden" // вошёл, но для действия нужна роль или право
	CodeLocked             = "locked"    // документ правит другой человек
	CodeConflict           = "conflict"  // документ изменён после того, как его открыл редактор
	CodeCodeTaken          = "code_taken"
)

type errorBody struct {
	Error errorDetail `json:"error"`
}

type errorDetail struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"request_id,omitempty"`
	// Fields — ошибки по полям формы (ключ — имя поля в JSON запроса).
	Fields map[string]string `json:"fields,omitempty"`
	// RetryAfter — через сколько секунд можно повторить (для 429).
	RetryAfter int `json:"retry_after,omitempty"`
	// RequiredLevel и RequiredLevelName — какой допуск нужен (для 403 «Доступ запрещён»).
	RequiredLevel     int    `json:"required_level,omitempty"`
	RequiredLevelName string `json:"required_level_name,omitempty"`
	// Problems — замечания к содержимому документа (путь в JSON и текст) при ответе 422.
	Problems []documents.Problem `json:"problems,omitempty"`
	// Lock — кто правит документ (409 locked); CurrentRevision — актуальная редакция (409 conflict).
	Lock            *lockDetail `json:"lock,omitempty"`
	CurrentRevision int         `json:"current_revision,omitempty"`
}

type lockDetail struct {
	Holder    string    `json:"holder"`
	ExpiresAt time.Time `json:"expires_at"`
}

// failDetail прерывает обработку ответом с заполненными подробностями (request_id ставится сам).
func failDetail(c *gin.Context, status int, d errorDetail) {
	d.RequestID = RequestID(c)
	c.AbortWithStatusJSON(status, errorBody{Error: d})
}

// FailAccessDenied отвечает 403 «Доступ запрещён» с указанием нужного допуска.
func FailAccessDenied(c *gin.Context, level int, levelName string) {
	c.AbortWithStatusJSON(403, errorBody{Error: errorDetail{
		Code:              CodeAccessDenied,
		Message:           "Доступ запрещён",
		RequestID:         RequestID(c),
		RequiredLevel:     level,
		RequiredLevelName: levelName,
	}})
}

// Fail прерывает обработку и отдаёт единый JSON-формат ошибки.
func Fail(c *gin.Context, status int, code, message string) {
	FailFields(c, status, code, message, nil)
}

// FailFields — Fail с ошибками по полям формы.
func FailFields(c *gin.Context, status int, code, message string, fields map[string]string) {
	c.AbortWithStatusJSON(status, errorBody{Error: errorDetail{
		Code:      code,
		Message:   message,
		RequestID: RequestID(c),
		Fields:    fields,
	}})
}

// FailRateLimited отвечает 429 с заголовком Retry-After (в секундах, округление вверх).
func FailRateLimited(c *gin.Context, retryAfter time.Duration) {
	secs := int(math.Ceil(retryAfter.Seconds()))
	if secs < 1 {
		secs = 1
	}
	c.Header("Retry-After", strconv.Itoa(secs))
	c.AbortWithStatusJSON(429, errorBody{Error: errorDetail{
		Code:       CodeRateLimited,
		Message:    "Слишком много попыток. Повторите позже.",
		RequestID:  RequestID(c),
		RetryAfter: secs,
	}})
}
