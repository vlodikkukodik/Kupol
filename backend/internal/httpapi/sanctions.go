package httpapi

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"kupol/internal/accounts"
	"kupol/internal/sanctions"
)

type sanctionHandlers struct {
	svc *sanctions.Service
	log *slog.Logger
}

func (h *sanctionHandlers) fail(c *gin.Context, err error) {
	var ve *sanctions.ValidationError
	switch {
	case errors.Is(err, sanctions.ErrForbidden):
		Fail(c, http.StatusForbidden, CodeForbidden, "Недостаточно прав")
	case errors.Is(err, sanctions.ErrNotFound):
		Fail(c, http.StatusNotFound, CodeNotFound, "Наказание не найдено")
	case errors.Is(err, sanctions.ErrUserMissing):
		FailFields(c, http.StatusNotFound, CodeNotFound, "Пользователь не найден", map[string]string{"login": "Такого логина нет"})
	case errors.Is(err, sanctions.ErrProtected):
		Fail(c, http.StatusForbidden, CodeForbidden, "Этого пользователя наказать нельзя")
	case errors.Is(err, sanctions.ErrRevoked):
		Fail(c, http.StatusConflict, CodeInvalidState, "Наказание уже снято")
	case errors.As(err, &ve):
		FailFields(c, http.StatusUnprocessableEntity, CodeValidation, "Проверьте поля формы", map[string]string{ve.Field: ve.Message})
	default:
		h.log.Error("ошибка обработчика наказаний", "err", err, "path", c.Request.URL.Path, "request_id", RequestID(c))
		Fail(c, http.StatusInternalServerError, CodeInternal, "Сбой архива")
	}
}

func sanctionActor(c *gin.Context) sanctions.Actor {
	u := CurrentAuth(c).User
	return sanctions.Actor{ID: u.ID, Directorate: u.Directorate, Moderator: u.Can(accounts.CapModerateComments)}
}

// GET /api/team/sanctions?login=
func (h *sanctionHandlers) list(c *gin.Context) {
	items, err := h.svc.List(c.Request.Context(), sanctionActor(c), c.Query("login"))
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, SanctionsResponse{Items: items})
}

// POST /api/team/sanctions {login, kind, reason, days?}
func (h *sanctionHandlers) issue(c *gin.Context) {
	var in sanctions.Input
	if !bindJSON(c, &in) {
		return
	}
	it, err := h.svc.Issue(c.Request.Context(), sanctionActor(c), in)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, SanctionResponse{Sanction: *it})
}

// POST /api/team/sanctions/:id/revoke
func (h *sanctionHandlers) revoke(c *gin.Context) {
	id, ok := idParam(c, "id")
	if !ok {
		return
	}
	it, err := h.svc.Revoke(c.Request.Context(), sanctionActor(c), id)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, SanctionResponse{Sanction: *it})
}
