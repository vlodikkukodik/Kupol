package httpapi

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"kupol/internal/documents"
)

// Шаблоны документов и наборы блоков в team panel. Читают все члены команды; создают, правят и удаляют — те, у кого право
// manage_templates (проверяет сервис документов по человеку).

// templateID читает номер шаблона из адреса; мусор — 404 «Шаблон не найден».
func templateID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id < 1 {
		Fail(c, http.StatusNotFound, CodeNotFound, "Шаблон не найден")
		return 0, false
	}
	return id, true
}

// GET /api/team/templates?kind=document|blockset — список по алфавиту.
func (h *teamDocumentHandlers) templates(c *gin.Context) {
	items, err := h.svc.TemplateList(c.Request.Context(), actorFrom(c), c.Query("kind"))
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, TemplatesResponse{Items: items})
}

// GET /api/team/templates/:id — шаблон целиком.
func (h *teamDocumentHandlers) template(c *gin.Context) {
	id, ok := templateID(c)
	if !ok {
		return
	}
	t, err := h.svc.TemplateGet(c.Request.Context(), actorFrom(c), id)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, TemplateResponse{Template: t})
}

// POST /api/team/templates — завести шаблон.
func (h *teamDocumentHandlers) createTemplate(c *gin.Context) {
	var in documents.TemplateInput
	if !bindJSONLimit(c, &in, maxDocumentBody) {
		return
	}
	t, err := h.svc.TemplateCreate(c.Request.Context(), actorFrom(c), in)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, TemplateResponse{Template: t})
}

// PUT /api/team/templates/:id — изменить название и описание (и содержимое, если оно передано).
func (h *teamDocumentHandlers) updateTemplate(c *gin.Context) {
	id, ok := templateID(c)
	if !ok {
		return
	}
	var req UpdateTemplateRequest
	if !bindJSONLimit(c, &req, maxDocumentBody) {
		return
	}
	t, err := h.svc.TemplateUpdate(c.Request.Context(), actorFrom(c), id, req.Name, req.Description, req.Content)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, TemplateResponse{Template: t})
}

// DELETE /api/team/templates/:id — удалить шаблон.
func (h *teamDocumentHandlers) deleteTemplate(c *gin.Context) {
	id, ok := templateID(c)
	if !ok {
		return
	}
	if err := h.svc.TemplateDelete(c.Request.Context(), actorFrom(c), id); err != nil {
		h.fail(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}
