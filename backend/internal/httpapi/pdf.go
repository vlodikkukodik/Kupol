package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"strconv"

	"github.com/gin-gonic/gin"

	"kupol/internal/documents"
	"kupol/internal/pdf"
)

// PDFQueue — очередь печати (этап 6.2). В тестах подменяется фейком: сборка typst'ом — не их дело.
type PDFQueue interface {
	Enqueue(ctx context.Context, args pdf.Args) (int64, error)
	Status(ctx context.Context, id int64) (*pdf.Status, error)
	File(id int64) string
}

type pdfHandlers struct {
	queue  PDFQueue
	docs   *documents.Service
	origin string
	log    *slog.Logger
}

// EnqueueResponse — POST /api/documents/:ref/pdfs: номер джобы, за ним можно следить.
type EnqueueResponse struct {
	ID int64 `json:"id"`
}

// enqueue ставит печать документа: лист соберётся глазами вошедшего читателя — по его допуску,
// как он читает документ на сайте.
func (h *pdfHandlers) enqueue(c *gin.Context) {
	v := viewerFrom(c)
	// до постановки в очередь документ обязан читаться:404 не создаёт джобу
	if _, err := h.docs.Get(c.Request.Context(), v, c.Param("ref")); err != nil {
		if errors.Is(err, documents.ErrNotFound) {
			Fail(c, http.StatusNotFound, CodeNotFound, "Дело не найдено")
			return
		}
		h.log.Error("pdf: проверка документа", "ошибка", err)
		Fail(c, http.StatusInternalServerError, CodeInternal, "Не удалось напечатать дело")
		return
	}
	id, err := h.queue.Enqueue(c.Request.Context(), pdf.Args{
		Ref:         c.Param("ref"),
		UserID:      v.UserID,
		UserLevel:   v.UserLevel,
		Directorate: v.Directorate,
		Lang:        string(v.Lang),
		SiteOrigin:  h.origin,
	})
	if err != nil {
		h.log.Error("pdf: постановка в очередь", "ошибка", err)
		Fail(c, http.StatusInternalServerError, CodeInternal, "Не удалось напечатать дело")
		return
	}
	c.JSON(http.StatusAccepted, EnqueueResponse{ID: id})
}

// mine находит джобу печати и проверяет, что её положил именно этот читатель: чужой номер
// неотличим от несуществующего — перебор ничего не раскрывает.
func (h *pdfHandlers) mine(c *gin.Context) (*pdf.Status, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		Fail(c, http.StatusNotFound, CodeNotFound, "Печать не найдена")
		return nil, false
	}
	st, err := h.queue.Status(c.Request.Context(), id)
	if err != nil || st.UserID != CurrentAuth(c).User.ID {
		Fail(c, http.StatusNotFound, CodeNotFound, "Печать не найдена")
		return nil, false
	}
	return st, true
}

// status — GET /api/pdfs/:id: queued | running | ready | failed.
func (h *pdfHandlers) status(c *gin.Context) {
	if st, ok := h.mine(c); ok {
		c.JSON(http.StatusOK, st)
	}
}

// file — GET /api/pdfs/:id/file: готовый PDF (только после ready).
func (h *pdfHandlers) file(c *gin.Context) {
	st, ok := h.mine(c)
	if !ok {
		return
	}
	path := h.queue.File(st.ID)
	if st.State != pdf.StateReady {
		Fail(c, http.StatusNotFound, CodeNotFound, "Печать ещё не готова")
		return
	}
	if _, err := os.Stat(path); err != nil {
		Fail(c, http.StatusNotFound, CodeNotFound, "Печать не найдена")
		return
	}
	c.Header("Content-Type", "application/pdf")
	c.Header("Cache-Control", "private, max-age=3600")
	c.File(path)
}
