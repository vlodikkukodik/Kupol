package httpapi

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"kupol/internal/documents"
)

// Глоссарий канона в team panel. Читают все члены команды; заводят, правят и удаляют — те, у кого право manage_glossary
// (проверяет сервис документов по человеку).

// termID читает номер термина из адреса; мусор — 404 «Термин не найден».
func termID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id < 1 {
		Fail(c, http.StatusNotFound, CodeNotFound, "Термин не найден")
		return 0, false
	}
	return id, true
}

// GET /api/team/glossary?q= — термины по алфавиту; q ищет по термину, другим написаниям и определению.
func (h *teamDocumentHandlers) glossary(c *gin.Context) {
	items, err := h.svc.GlossaryList(c.Request.Context(), actorFrom(c), c.Query("q"))
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, GlossaryResponse{Items: items})
}

// POST /api/team/glossary — завести термин.
func (h *teamDocumentHandlers) createTerm(c *gin.Context) {
	var in documents.TermInput
	if !bindJSONLimit(c, &in, maxDocumentBody) {
		return
	}
	t, err := h.svc.GlossaryCreate(c.Request.Context(), actorFrom(c), in)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, TermResponse{Term: t})
}

// PUT /api/team/glossary/:id — заменить термин, определение и написания.
func (h *teamDocumentHandlers) updateTerm(c *gin.Context) {
	id, ok := termID(c)
	if !ok {
		return
	}
	var in documents.TermInput
	if !bindJSONLimit(c, &in, maxDocumentBody) {
		return
	}
	t, err := h.svc.GlossaryUpdate(c.Request.Context(), actorFrom(c), id, in)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, TermResponse{Term: t})
}

// DELETE /api/team/glossary/:id — удалить термин.
func (h *teamDocumentHandlers) deleteTerm(c *gin.Context) {
	id, ok := termID(c)
	if !ok {
		return
	}
	if err := h.svc.GlossaryDelete(c.Request.Context(), actorFrom(c), id); err != nil {
		h.fail(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}
