package httpapi

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"kupol/internal/documents"
)

// GET /api/timeline — хронология «О КУПОЛЕ» для читателя: события не выше его допуска; ссылка на документ — только на доступный ему.
func (h *documentHandlers) timeline(c *gin.Context) {
	items, err := h.svc.Timeline(c.Request.Context(), viewerFrom(c))
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, TimelineResponse{Items: items})
}

// GET /api/site — публичные настройки сайта (контакты автора).
func (h *documentHandlers) site(c *gin.Context) {
	s, err := h.svc.SiteInfo(c.Request.Context())
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, SiteResponse{Site: s})
}

// GET /api/team/site — настройки для панели: читают члены команды, правит только Директорат.
func (h *teamDocumentHandlers) siteGet(c *gin.Context) {
	s, err := h.svc.SiteSettingsGet(c.Request.Context(), actorFrom(c))
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, SiteSettingsResponse{Site: s})
}

// PUT /api/team/site {contact}
func (h *teamDocumentHandlers) siteUpdate(c *gin.Context) {
	var in documents.SiteSettings
	if !bindJSONLimit(c, &in, maxDocumentBody) {
		return
	}
	s, err := h.svc.SiteSettingsUpdate(c.Request.Context(), actorFrom(c), in)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, SiteSettingsResponse{Site: s})
}

// Хронология в team panel. Читают все члены команды (со всеми событиями, в том числе закрытыми уровнем); заводят, правят и удаляют —
// те, у кого право manage_timeline (проверяет сервис по человеку).

func eventID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id < 1 {
		Fail(c, http.StatusNotFound, CodeNotFound, "Событие не найдено")
		return 0, false
	}
	return id, true
}

// GET /api/team/timeline
func (h *teamDocumentHandlers) timelineList(c *gin.Context) {
	items, err := h.svc.TimelineList(c.Request.Context(), actorFrom(c))
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, TimelineEventsResponse{Items: items})
}

// POST /api/team/timeline
func (h *teamDocumentHandlers) timelineCreate(c *gin.Context) {
	var in documents.TimelineInput
	if !bindJSONLimit(c, &in, maxDocumentBody) {
		return
	}
	e, err := h.svc.TimelineCreate(c.Request.Context(), actorFrom(c), in)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, TimelineEventResponse{Event: e})
}

// PUT /api/team/timeline/:id
func (h *teamDocumentHandlers) timelineUpdate(c *gin.Context) {
	id, ok := eventID(c)
	if !ok {
		return
	}
	var in documents.TimelineInput
	if !bindJSONLimit(c, &in, maxDocumentBody) {
		return
	}
	e, err := h.svc.TimelineUpdate(c.Request.Context(), actorFrom(c), id, in)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, TimelineEventResponse{Event: e})
}

// DELETE /api/team/timeline/:id
func (h *teamDocumentHandlers) timelineDelete(c *gin.Context) {
	id, ok := eventID(c)
	if !ok {
		return
	}
	if err := h.svc.TimelineDelete(c.Request.Context(), actorFrom(c), id); err != nil {
		h.fail(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}
