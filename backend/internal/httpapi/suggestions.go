package httpapi

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"kupol/internal/suggestions"
)

// «Предложения» (шаг 5.4, спецификация §8): одна форма для читателей (идея или замечание), очередь
// Редакторов в панели команды, статусы получено → рассмотрено → принято/отклонено, +100 XP автору при принятии.

type suggestionHandlers struct {
	svc *suggestions.Service
	log *slog.Logger
}

// fail переводит ошибки сервиса предложений в ответ API.
func (h *suggestionHandlers) fail(c *gin.Context, err error) {
	var ve *suggestions.ValidationError
	switch {
	case errors.Is(err, suggestions.ErrNotFound):
		Fail(c, http.StatusNotFound, CodeNotFound, "Предложение не найдено")
	case errors.Is(err, suggestions.ErrForbidden):
		Fail(c, http.StatusForbidden, CodeForbidden, "Недостаточно прав")
	case errors.Is(err, suggestions.ErrSelfReview):
		Fail(c, http.StatusForbidden, CodeSelfReview, "Своё предложение рассматривает другой Редактор")
	case errors.Is(err, suggestions.ErrInvalidState):
		Fail(c, http.StatusConflict, CodeInvalidState, "Это действие не подходит предложению в его нынешнем статусе")
	case errors.As(err, &ve):
		FailFields(c, http.StatusUnprocessableEntity, CodeValidation, "Проверьте поля формы", ve.Fields)
	default:
		h.log.Error("ошибка обработчика предложений", "err", err, "path", c.Request.URL.Path, "request_id", RequestID(c))
		Fail(c, http.StatusInternalServerError, CodeInternal, "Сбой архива")
	}
}

// POST /api/suggestions — отправить предложение (для вошедших; публикуется сразу со статусом «получено»).
func (h *suggestionHandlers) create(c *gin.Context) {
	var req CreateSuggestionRequest
	if !bindJSON(c, &req) {
		return
	}
	out, err := h.svc.Create(c.Request.Context(), CurrentAuth(c).User.ID, req.Text)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, SuggestionResponse{Suggestion: out})
}

// GET /api/suggestions — собственные предложения читателя: статусы и «записки» Редакторов.
func (h *suggestionHandlers) mine(c *gin.Context) {
	items, err := h.svc.Mine(c.Request.Context(), CurrentAuth(c).User.ID)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, SuggestionsResponse{Items: items})
}

// GET /api/team/suggestions?status=&page=&per_page= — очередь для Редакторов (право review).
func (h *suggestionHandlers) queue(c *gin.Context) {
	q := suggestions.Query{Status: suggestions.Status(c.Query("status"))}
	for name, dst := range map[string]*int{"page": &q.Page, "per_page": &q.PerPage} {
		raw := c.Query(name)
		if raw == "" {
			continue
		}
		n, err := strconv.Atoi(raw)
		if err != nil {
			FailFields(c, http.StatusBadRequest, CodeBadRequest, "Некорректные параметры запроса", map[string]string{name: "ожидается число"})
			return
		}
		*dst = n
	}
	list, err := h.svc.Queue(c.Request.Context(), q)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, SuggestionsListResponse{
		Items: list.Items, Total: list.Total, Page: list.Page, PerPage: list.PerPage, Pages: list.Pages,
	})
}

// POST /api/team/suggestions/:id/status — перевести предложение в новый статус с пояснением автору.
func (h *suggestionHandlers) setStatus(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id < 1 {
		Fail(c, http.StatusNotFound, CodeNotFound, "Предложение не найдено")
		return
	}
	var req SuggestionStatusRequest
	if !bindJSON(c, &req) {
		return
	}
	out, err := h.svc.SetStatus(c.Request.Context(), CurrentAuth(c).User.ID, id, req.Status, req.Comment)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, SuggestionResponse{Suggestion: out})
}
