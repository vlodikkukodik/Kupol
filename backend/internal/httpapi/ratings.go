package httpapi

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// GET /api/documents/:ref/ratings — оценки документа (счётчики + моя оценка).
func (h *documentHandlers) getRatings(c *gin.Context) {
	out, err := h.svc.GetRatings(c.Request.Context(), viewerFrom(c), c.Param("ref"))
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, RatingsResponse{Ratings: out})
}

// POST /api/documents/:ref/ratings — установить оценку (toggle: повторная отправка убирает).
func (h *documentHandlers) setRating(c *gin.Context) {
	var req SetRatingRequest
	if !bindJSON(c, &req) {
		return
	}
	out, err := h.svc.SetRating(c.Request.Context(), viewerFrom(c), c.Param("ref"), req.Rating)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, RatingsResponse{Ratings: out})
}

// DELETE /api/documents/:ref/ratings — удалить свою оценку.
func (h *documentHandlers) deleteRating(c *gin.Context) {
	out, err := h.svc.DeleteRating(c.Request.Context(), viewerFrom(c), c.Param("ref"))
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, RatingsResponse{Ratings: out})
}
