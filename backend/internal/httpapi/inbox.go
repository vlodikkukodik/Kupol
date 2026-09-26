package httpapi

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"kupol/internal/accounts"
	"kupol/internal/inbox"
)

// Внутренняя почта (шаг 5.7): ящик читателя и записки Директората. Тексты автоматических записок собираются на
// языке запроса; записка Директората — как написана.

// GET /api/me/inbox?page=N
func (h *authHandlers) inbox(c *gin.Context) {
	page, _ := strconv.Atoi(c.Query("page"))
	res, err := h.svc.Inbox(c.Request.Context(), CurrentAuth(c).User.ID, page, Lang(c))
	if err != nil {
		h.fail(c, err, "")
		return
	}
	c.JSON(http.StatusOK, res)
}

// GET /api/me/inbox/unread — число непрочитанных (значок в меню).
func (h *authHandlers) inboxUnread(c *gin.Context) {
	n, err := h.svc.InboxUnread(c.Request.Context(), CurrentAuth(c).User.ID)
	if err != nil {
		h.fail(c, err, "")
		return
	}
	c.JSON(http.StatusOK, InboxUnreadResponse{Unread: n})
}

// POST /api/me/inbox/:id/read
func (h *authHandlers) inboxRead(c *gin.Context) {
	id, ok := idParam(c, "id")
	if !ok {
		return
	}
	if err := h.svc.InboxMarkRead(c.Request.Context(), CurrentAuth(c).User.ID, id); err != nil {
		if errors.Is(err, accounts.ErrUserNotFound) {
			Fail(c, http.StatusNotFound, CodeNotFound, "Записка не найдена")
			return
		}
		h.fail(c, err, "")
		return
	}
	c.Status(http.StatusNoContent)
}

// POST /api/me/inbox/read-all
func (h *authHandlers) inboxReadAll(c *gin.Context) {
	if err := h.svc.InboxMarkAllRead(c.Request.Context(), CurrentAuth(c).User.ID); err != nil {
		h.fail(c, err, "")
		return
	}
	c.Status(http.StatusNoContent)
}

// SendNoteRequest — POST /api/team/inbox/send. login пуст — записка всем.
type SendNoteRequest struct {
	Login string `json:"login"`
	Title string `json:"title"`
	Body  string `json:"body"`
}

// SendNoteResponse — сколько читателей получили записку.
type SendNoteResponse struct {
	Recipients int64 `json:"recipients"`
}

// POST /api/team/inbox/send — записка Директората; только Директорат.
func (h *authHandlers) inboxSend(c *gin.Context) {
	u := CurrentAuth(c).User
	if !u.Directorate {
		Fail(c, http.StatusForbidden, CodeForbidden, "Недостаточно прав")
		return
	}
	var req SendNoteRequest
	if !bindJSONLimit(c, &req, MaxBodyBytes) {
		return
	}
	note := inbox.Note{Title: req.Title, Body: req.Body}
	if field, msg := note.Validate(); field != "" {
		FailFields(c, http.StatusUnprocessableEntity, CodeValidation, "Проверьте поля формы", map[string]string{field: msg})
		return
	}
	n, err := h.svc.InboxSendNote(c.Request.Context(), u.ID, req.Login, note)
	if errors.Is(err, accounts.ErrUserNotFound) {
		FailFields(c, http.StatusNotFound, CodeNotFound, "Пользователь не найден", map[string]string{"login": "Такого логина нет"})
		return
	}
	if err != nil {
		h.fail(c, err, "")
		return
	}
	c.JSON(http.StatusOK, SendNoteResponse{Recipients: n})
}
