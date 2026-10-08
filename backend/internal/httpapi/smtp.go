package httpapi

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"kupol/internal/mailsettings"
)

// Почтовый сервер в панели команды: читают члены команды (как настройки сайта), правит и проверяет
// письмом только Директорат — право проверяет сервис. Пароль в ответ не входит: видно, только что задан.

// SmtpTestRequest — POST /api/team/smtp/test: кому уходит письмо-проверка.
type SmtpTestRequest struct {
	To string `json:"to"`
}

func smtpActor(c *gin.Context) mailsettings.Actor {
	u := CurrentAuth(c).User
	return mailsettings.Actor{UserID: u.ID, Directorate: u.Directorate}
}

// GET /api/team/smtp
func (h *teamDocumentHandlers) smtpGet(c *gin.Context) {
	s, err := h.smtp.Get(c.Request.Context(), smtpActor(c))
	if err != nil {
		h.failSMTP(c, err)
		return
	}
	c.JSON(http.StatusOK, SmtpSettingsResponse{Smtp: s})
}

// PUT /api/team/smtp {enabled, host, port, username, password?, from, from_name}
func (h *teamDocumentHandlers) smtpUpdate(c *gin.Context) {
	var in mailsettings.Input
	if !bindJSON(c, &in) {
		return
	}
	s, err := h.smtp.Update(c.Request.Context(), smtpActor(c), in)
	if err != nil {
		h.failSMTP(c, err)
		return
	}
	c.JSON(http.StatusOK, SmtpSettingsResponse{Smtp: s})
}

// POST /api/team/smtp/test {to} — письмо-проверка сохранёнными настройками.
func (h *teamDocumentHandlers) smtpTest(c *gin.Context) {
	var in SmtpTestRequest
	if !bindJSON(c, &in) {
		return
	}
	if err := h.smtp.Test(c.Request.Context(), smtpActor(c), in.To); err != nil {
		h.failSMTP(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// failSMTP переводит ошибки настроек почты в ответ API.
func (h *teamDocumentHandlers) failSMTP(c *gin.Context, err error) {
	var (
		ve *mailsettings.ValidationError
		se *mailsettings.SendError
	)
	switch {
	case errors.Is(err, mailsettings.ErrForbidden):
		Fail(c, http.StatusForbidden, CodeForbidden, "Недостаточно прав")
	case errors.Is(err, mailsettings.ErrNotEnabled):
		Fail(c, http.StatusConflict, CodeInvalidState, "Почта выключена: включите отправку писем и повторите")
	case errors.As(err, &se):
		// подробности (адрес сервера, ответ SMTP) — в журнале: клиенту они только мешают
		h.log.Warn("письмо-проверка не ушло", "err", se.Err, "path", c.Request.URL.Path, "request_id", RequestID(c))
		Fail(c, http.StatusUnprocessableEntity, CodeValidation, "Письмо не ушло: проверьте настройки почтового сервера")
	case errors.As(err, &ve):
		FailFields(c, http.StatusUnprocessableEntity, CodeValidation, "Проверьте поля формы", ve.Fields)
	default:
		h.log.Error("ошибка настроек почты", "err", err, "path", c.Request.URL.Path, "request_id", RequestID(c))
		Fail(c, http.StatusInternalServerError, CodeInternal, "Сбой архива")
	}
}
