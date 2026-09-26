package httpapi

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"kupol/internal/documents"
)

// Ход документа и рецензия в team panel: отправка на проверку, вердикты, архив, комментарии рецензента, линтер канона.
// Права и правила переходов решает сервис документов; здесь — разбор запроса и ответы.

// GET /api/team/documents/:id/lint — проверка канона по сохранённому документу.
func (h *teamDocumentHandlers) lint(c *gin.Context) {
	id, ok := idParam(c, "id")
	if !ok {
		return
	}
	rep, err := h.svc.TeamLint(c.Request.Context(), actorFrom(c), id)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, LintResponse{Lint: rep})
}

// GET /api/team/documents/:id/review — ход рецензии и комментарии.
func (h *teamDocumentHandlers) review(c *gin.Context) {
	id, ok := idParam(c, "id")
	if !ok {
		return
	}
	info, err := h.svc.TeamReview(c.Request.Context(), actorFrom(c), id)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, ReviewResponse{Review: info})
}

// POST /api/team/documents/:id/submit — отправить черновик на проверку (тело: base_revision).
func (h *teamDocumentHandlers) submit(c *gin.Context) {
	id, ok := idParam(c, "id")
	if !ok {
		return
	}
	var req SubmitRequest
	if !bindJSONLimit(c, &req, maxDocumentBody) {
		return
	}
	d, err := h.svc.Submit(c.Request.Context(), actorFrom(c), id, req.BaseRevision)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, TeamDocumentResponse{Document: d})
}

// noteAction — общий разбор тела с необязательным пояснением для забрать, в архив, из архива.
func (h *teamDocumentHandlers) noteAction(c *gin.Context, do func(a documents.Actor, id int64, note string) (*documents.TeamDocument, error)) {
	id, ok := idParam(c, "id")
	if !ok {
		return
	}
	var req NoteRequest
	if !bindJSONLimit(c, &req, maxDocumentBody) {
		return
	}
	d, err := do(actorFrom(c), id, req.Comment)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, TeamDocumentResponse{Document: d})
}

// POST /api/team/documents/:id/withdraw — автор забирает документ с проверки.
func (h *teamDocumentHandlers) withdraw(c *gin.Context) {
	h.noteAction(c, func(a documents.Actor, id int64, note string) (*documents.TeamDocument, error) {
		return h.svc.Withdraw(c.Request.Context(), a, id, note)
	})
}

// POST /api/team/documents/:id/archive и /unarchive — в архив и обратно (Редактор и Директорат).
func (h *teamDocumentHandlers) archive(c *gin.Context) {
	h.noteAction(c, func(a documents.Actor, id int64, note string) (*documents.TeamDocument, error) {
		return h.svc.Archive(c.Request.Context(), a, id, note)
	})
}

func (h *teamDocumentHandlers) unarchive(c *gin.Context) {
	h.noteAction(c, func(a documents.Actor, id int64, note string) (*documents.TeamDocument, error) {
		return h.svc.Unarchive(c.Request.Context(), a, id, note)
	})
}

// POST /api/team/documents/:id/verdict — вердикт рецензента: approve (публикация), return, reject.
func (h *teamDocumentHandlers) verdict(c *gin.Context) {
	id, ok := idParam(c, "id")
	if !ok {
		return
	}
	var req VerdictRequest
	if !bindJSONLimit(c, &req, maxDocumentBody) {
		return
	}
	d, err := h.svc.Decide(c.Request.Context(), actorFrom(c), id, documents.Verdict(req.Verdict), req.Comment, req.BaseRevision)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, TeamDocumentResponse{Document: d})
}

// POST /api/team/documents/:id/comments — комментарий к блоку или к документу целиком.
func (h *teamDocumentHandlers) addComment(c *gin.Context) {
	id, ok := idParam(c, "id")
	if !ok {
		return
	}
	var req AddCommentRequest
	if !bindJSONLimit(c, &req, maxDocumentBody) {
		return
	}
	out, err := h.svc.AddComment(c.Request.Context(), actorFrom(c), id, req.BlockID, req.Body)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, CommentResponse{Comment: out})
}

// PUT /api/team/documents/:id/comments/:cid — отметить исправленным или вернуть в открытые.
func (h *teamDocumentHandlers) resolveComment(c *gin.Context) {
	id, ok := idParam(c, "id")
	if !ok {
		return
	}
	cid, ok := idParam(c, "cid")
	if !ok {
		return
	}
	var req ResolveCommentRequest
	if !bindJSONLimit(c, &req, maxDocumentBody) {
		return
	}
	out, err := h.svc.SetCommentResolved(c.Request.Context(), actorFrom(c), id, cid, req.Resolved)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, CommentResponse{Comment: out})
}

// DELETE /api/team/documents/:id/comments/:cid — удалить комментарий (автор комментария и Директорат).
func (h *teamDocumentHandlers) deleteComment(c *gin.Context) {
	id, ok := idParam(c, "id")
	if !ok {
		return
	}
	cid, ok := idParam(c, "cid")
	if !ok {
		return
	}
	if err := h.svc.DeleteComment(c.Request.Context(), actorFrom(c), id, cid); err != nil {
		h.fail(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// GET /api/team/dashboard — рабочий стол: мои документы, возвращённые на доработку, очередь на проверку (рецензентам).
func (h *teamDocumentHandlers) dashboard(c *gin.Context) {
	d, err := h.svc.TeamDashboard(c.Request.Context(), actorFrom(c))
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, DashboardResponse{Dashboard: d})
}
