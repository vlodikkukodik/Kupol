package httpapi

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"kupol/internal/petitions"
)

type petitionHandlers struct {
	svc *petitions.Service
	log *slog.Logger
}

func (h *petitionHandlers) fail(c *gin.Context, err error) {
	var ve *petitions.ValidationError
	switch {
	case errors.Is(err, petitions.ErrNotFound):
		Fail(c, http.StatusNotFound, CodeNotFound, "Ходатайство не найдено")
	case errors.Is(err, petitions.ErrForbidden):
		Fail(c, http.StatusForbidden, CodeForbidden, "Недостаточно прав")
	case errors.Is(err, petitions.ErrSelf):
		Fail(c, http.StatusForbidden, CodeSelfReview, "Своё ходатайство решает другой член Совета")
	case errors.Is(err, petitions.ErrPending):
		Fail(c, http.StatusConflict, CodeInvalidState, "У вас уже есть нерассмотренное ходатайство")
	case errors.Is(err, petitions.ErrNoNextLevel):
		Fail(c, http.StatusConflict, CodeInvalidState, "Ходатайство на следующий уровень сейчас недоступно")
	case errors.Is(err, petitions.ErrAlreadyDone), errors.Is(err, petitions.ErrLevelChanged):
		Fail(c, http.StatusConflict, CodeInvalidState, "Это ходатайство уже нельзя решить")
	case errors.Is(err, petitions.ErrInvNotFound):
		Fail(c, http.StatusNotFound, CodeNotFound, "Приглашение не найдено")
	case errors.Is(err, petitions.ErrUserMissing):
		FailFields(c, http.StatusNotFound, CodeNotFound, "Пользователь не найден", map[string]string{"login": "Такого логина нет"})
	case errors.Is(err, petitions.ErrInvDone):
		Fail(c, http.StatusConflict, CodeInvalidState, "Приглашение уже закрыто")
	case errors.Is(err, petitions.ErrInvSelf):
		Fail(c, http.StatusForbidden, CodeForbidden, "Себя пригласить нельзя")
	case errors.Is(err, petitions.ErrBadVerdict):
		FailFields(c, http.StatusUnprocessableEntity, CodeValidation, "Проверьте поля формы", map[string]string{"verdict": "Решение: approved или rejected"})
	case errors.As(err, &ve):
		FailFields(c, http.StatusUnprocessableEntity, CodeValidation, "Проверьте поля формы", map[string]string{ve.Field: ve.Message})
	default:
		h.log.Error("ошибка обработчика ходатайств", "err", err, "path", c.Request.URL.Path, "request_id", RequestID(c))
		Fail(c, http.StatusInternalServerError, CodeInternal, "Сбой архива")
	}
}

func actorOf(c *gin.Context) petitions.Actor {
	u := CurrentAuth(c).User
	return petitions.Actor{ID: u.ID, Level: u.Level, Directorate: u.Directorate}
}

// POST /api/petitions {text} — ходатайство на следующий уровень.
func (h *petitionHandlers) create(c *gin.Context) {
	var req CreatePetitionRequest
	if !bindJSON(c, &req) {
		return
	}
	it, err := h.svc.Create(c.Request.Context(), CurrentAuth(c).User.ID, req.Text)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, PetitionResponse{Petition: *it})
}

// GET /api/petitions — собственные ходатайства.
func (h *petitionHandlers) mine(c *gin.Context) {
	items, err := h.svc.Mine(c.Request.Context(), CurrentAuth(c).User.ID)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, PetitionsResponse{Items: items})
}

// GET /api/team/petitions — очередь Совета.
func (h *petitionHandlers) queue(c *gin.Context) {
	items, err := h.svc.Queue(c.Request.Context(), actorOf(c))
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, PetitionsResponse{Items: items})
}

// POST /api/team/petitions/:id/decision {verdict, comment}
func (h *petitionHandlers) decide(c *gin.Context) {
	id, ok := idParam(c, "id")
	if !ok {
		return
	}
	var req DecidePetitionRequest
	if !bindJSON(c, &req) {
		return
	}
	it, err := h.svc.Decide(c.Request.Context(), actorOf(c), id, req.Verdict, req.Comment)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, PetitionResponse{Petition: *it})
}

// POST /api/team/invitations {login, message} — Совет приглашает читателя на следующий уровень.
func (h *petitionHandlers) invite(c *gin.Context) {
	var req InviteRequest
	if !bindJSON(c, &req) {
		return
	}
	it, err := h.svc.Invite(c.Request.Context(), actorOf(c), req.Login, req.Message)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, InvitationResponse{Invitation: *it})
}

// GET /api/team/invitations — приглашения для Совета.
func (h *petitionHandlers) invitations(c *gin.Context) {
	items, err := h.svc.Invitations(c.Request.Context(), actorOf(c))
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, InvitationsResponse{Items: items})
}

// POST /api/team/invitations/:id/withdraw — Совет отзывает нерассмотренное приглашение.
func (h *petitionHandlers) withdraw(c *gin.Context) {
	h.answer(c, "withdraw")
}

// GET /api/invitations — приглашения читателя.
func (h *petitionHandlers) myInvitations(c *gin.Context) {
	items, err := h.svc.MyInvitations(c.Request.Context(), CurrentAuth(c).User.ID)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, InvitationsResponse{Items: items})
}

// POST /api/invitations/:id/respond {answer: accept|decline}
func (h *petitionHandlers) respond(c *gin.Context) {
	var req RespondInvitationRequest
	if !bindJSON(c, &req) {
		return
	}
	if req.Answer == "withdraw" {
		req.Answer = "" // отзыв — только через /team/invitations/:id/withdraw
	}
	h.answer(c, req.Answer)
}

func (h *petitionHandlers) answer(c *gin.Context, answer string) {
	id, ok := idParam(c, "id")
	if !ok {
		return
	}
	it, err := h.svc.Respond(c.Request.Context(), actorOf(c), id, answer)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, InvitationResponse{Invitation: *it})
}
