package httpapi

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"kupol/internal/documents"
)

// remarkIDParam — числовой id пометки из адреса; мусор — 404 с понятным для пометок текстом (idParam из
// team_documents.go отвечает «Документ не найден», здесь это было бы неверно).
func remarkIDParam(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id < 1 {
		Fail(c, http.StatusNotFound, CodeNotFound, "Пометка не найдена")
		return 0, false
	}
	return id, true
}

// GET /api/documents/:ref/remarks — «пометки на полях» под документом; видимость треда — как у самого документа.
func (h *documentHandlers) listRemarks(c *gin.Context) {
	out, err := h.svc.ListRemarks(c.Request.Context(), viewerFrom(c), c.Param("ref"))
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, RemarksResponse{Items: out})
}

type CreateRemarkRequest struct {
	ParentID *int64 `json:"parent_id,omitempty"`
	Text     string `json:"text"`
}

// POST /api/documents/:ref/remarks — новая пометка или ответ; публикуется сразу, начисляет XP (до дневного лимита).
func (h *documentHandlers) createRemark(c *gin.Context) {
	var req CreateRemarkRequest
	if !bindJSON(c, &req) {
		return
	}
	out, err := h.svc.CreateRemark(c.Request.Context(), viewerFrom(c), c.Param("ref"), documents.RemarkInput{ParentID: req.ParentID, Text: req.Text})
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, RemarkResponse{Remark: out})
}

// POST /api/remarks/:id/report — «Жалоба»; одна на пометку от одного читателя.
func (h *documentHandlers) reportRemark(c *gin.Context) {
	id, ok := remarkIDParam(c)
	if !ok {
		return
	}
	if err := h.svc.ReportRemark(c.Request.Context(), viewerFrom(c), id); err != nil {
		h.fail(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// DELETE /api/remarks/:id — автор удаляет свою пометку, модератор — чужую.
func (h *documentHandlers) deleteRemark(c *gin.Context) {
	id, ok := remarkIDParam(c)
	if !ok {
		return
	}
	if err := h.svc.DeleteRemark(c.Request.Context(), viewerFrom(c), id); err != nil {
		h.fail(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// GET /api/team/remarks/reported — очередь жалоб для модератора (право moderate_comments).
func (h *documentHandlers) reportedRemarks(c *gin.Context) {
	out, err := h.svc.ReportedRemarks(c.Request.Context(), viewerFrom(c))
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, RemarksResponse{Items: out})
}
