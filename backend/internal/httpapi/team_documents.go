package httpapi

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"kupol/internal/accounts"
	"kupol/internal/documents"
)

// Документы в team panel: список, создание, сохранение, автосохранение, замок, история и откат.
// Проверку прав делает сервис документов (по человеку и по документу), здесь — разбор запроса и перевод ошибок.

type teamDocumentHandlers struct {
	svc *documents.Service
	log *slog.Logger
}

// maxDocumentBody — предел тела при сохранении документа: тот же, что у обычных запросов (и у PHP-прокси).
const maxDocumentBody = MaxBodyBytes - 4<<10

// actorFrom собирает из вошедшего пользователя того, кто работает с документами; права приходят готовыми из аккаунтов.
func actorFrom(c *gin.Context) documents.Actor {
	u := CurrentAuth(c).User
	return documents.Actor{
		UserID:           u.ID,
		Login:            u.Login,
		Directorate:      u.Directorate,
		CanWrite:         u.Can(accounts.CapWriteDrafts),
		CanReview:        u.Can(accounts.CapReview),
		CanEditPublished: u.Can(accounts.CapEditPublished),
	}
}

func (h *teamDocumentHandlers) fail(c *gin.Context, err error) {
	var (
		qe *documents.QueryError
		ve *documents.ValidationError
		le *documents.LockedError
		ce *documents.ConflictError
		se *documents.StateError
		lf *documents.LintFailedError
	)
	switch {
	case errors.Is(err, documents.ErrNotFound):
		Fail(c, http.StatusNotFound, CodeNotFound, "Документ не найден")
	case errors.Is(err, documents.ErrForbidden):
		Fail(c, http.StatusForbidden, CodeForbidden, "Недостаточно прав")
	case errors.Is(err, documents.ErrSelfReview):
		Fail(c, http.StatusForbidden, CodeSelfReview, "Свой документ проверяет другой Редактор")
	case errors.Is(err, documents.ErrCommentNotFound):
		Fail(c, http.StatusNotFound, CodeNotFound, "Комментарий не найден")
	case errors.As(err, &se):
		Fail(c, http.StatusConflict, CodeInvalidState, "Это действие не подходит документу в его нынешнем статусе")
	case errors.As(err, &lf):
		failDetail(c, http.StatusUnprocessableEntity, ErrorDetail{Code: CodeLintFailed, Message: "Документ не прошёл проверку канона: " + lf.Report.Summary(), Lint: lf.Report})
	case errors.Is(err, documents.ErrCodeTaken):
		FailFields(c, http.StatusConflict, CodeCodeTaken, "Этот шифр уже занят", map[string]string{"code": "Этот шифр уже занят другим документом"})
	case errors.As(err, &le):
		failDetail(c, http.StatusConflict, ErrorDetail{
			Code: CodeLocked, Message: "Документ правит " + le.Holder, Lock: &LockDetail{Holder: le.Holder, ExpiresAt: le.ExpiresAt.UTC()},
		})
	case errors.As(err, &ce):
		failDetail(c, http.StatusConflict, ErrorDetail{
			Code: CodeConflict, Message: "Документ изменён после того, как вы его открыли", CurrentRevision: ce.CurrentRevision,
		})
	case errors.As(err, &ve):
		failDetail(c, http.StatusUnprocessableEntity, ErrorDetail{Code: CodeValidation, Message: "Проверьте содержимое документа", Problems: ve.Problems})
	case errors.As(err, &qe):
		FailFields(c, http.StatusBadRequest, CodeBadRequest, "Некорректные параметры запроса", map[string]string{qe.Field: qe.Message})
	default:
		h.log.Error("ошибка обработчика документов команды", "err", err, "path", c.Request.URL.Path, "request_id", RequestID(c))
		Fail(c, http.StatusInternalServerError, CodeInternal, "Сбой архива")
	}
}

// idParam читает числовой идентификатор из адреса; мусор — 404, как у несуществующего документа.
func idParam(c *gin.Context, name string) (int64, bool) {
	id, err := strconv.ParseInt(c.Param(name), 10, 64)
	if err != nil || id < 1 {
		Fail(c, http.StatusNotFound, CodeNotFound, "Документ не найден")
		return 0, false
	}
	return id, true
}

// GET /api/team/document-types — справочник значений для форм (типы, статусы, свойства Объекта).
func (h *teamDocumentHandlers) meta(c *gin.Context) {
	c.JSON(http.StatusOK, documents.DocumentMeta())
}

// GET /api/team/documents?status=&type=&q=&mine=1&page=&per_page=
func (h *teamDocumentHandlers) list(c *gin.Context) {
	q := documents.TeamListQuery{Status: c.Query("status"), Type: c.Query("type"), Query: c.Query("q")}
	switch c.Query("mine") {
	case "", "0":
	case "1":
		q.Mine = true
	default:
		FailFields(c, http.StatusBadRequest, CodeBadRequest, "Некорректные параметры запроса", map[string]string{"mine": "0 или 1"})
		return
	}
	for name, dst := range map[string]*int{"page": &q.Page, "per_page": &q.PerPage} {
		v, ok := intParam(c, name)
		if !ok {
			return
		}
		if v != nil {
			*dst = *v
		}
	}
	res, err := h.svc.TeamList(c.Request.Context(), actorFrom(c), q)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, res)
}

type CreateDocumentRequest struct {
	Type              string `json:"type"`
	Code              string `json:"code"`
	documents.Content `tstype:",extends"`
}

// POST /api/team/documents — завести черновик.
func (h *teamDocumentHandlers) create(c *gin.Context) {
	var req CreateDocumentRequest
	if !bindJSONLimit(c, &req, maxDocumentBody) {
		return
	}
	d, err := h.svc.TeamCreate(c.Request.Context(), actorFrom(c), documents.CreateInput{Type: req.Type, Code: req.Code, Content: req.Content})
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, TeamDocumentResponse{Document: d})
}

// GET /api/team/documents/:id — документ целиком (без фильтрации по допуску читателя).
func (h *teamDocumentHandlers) get(c *gin.Context) {
	id, ok := idParam(c, "id")
	if !ok {
		return
	}
	d, err := h.svc.TeamGet(c.Request.Context(), actorFrom(c), id)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, TeamDocumentResponse{Document: d})
}

type SaveDocumentRequest struct {
	BaseRevision int               `json:"base_revision"`
	Content      documents.Content `json:"content"`
}

// PUT /api/team/documents/:id — сохранить содержимое. base_revision — редакция, с которой начал редактор.
func (h *teamDocumentHandlers) save(c *gin.Context) {
	id, ok := idParam(c, "id")
	if !ok {
		return
	}
	var req SaveDocumentRequest
	if !bindJSONLimit(c, &req, maxDocumentBody) {
		return
	}
	res, err := h.svc.TeamSave(c.Request.Context(), actorFrom(c), id, req.BaseRevision, req.Content)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, res)
}

// PUT /api/team/documents/:id/draft — автосохранение несохранённых правок (тело — содержимое документа).
func (h *teamDocumentHandlers) autosave(c *gin.Context) {
	id, ok := idParam(c, "id")
	if !ok {
		return
	}
	raw, ok := readJSONBody(c, maxDocumentBody)
	if !ok {
		return
	}
	res, err := h.svc.TeamAutosave(c.Request.Context(), actorFrom(c), id, raw)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, res)
}

// PreviewRequest — тело POST /api/team/documents/:id/preview. Content — несохранённые правки из редактора;
// без него предпросмотр строится по сохранённому.
type PreviewRequest struct {
	Level   int                `json:"level"`
	Content *documents.Content `json:"content,omitempty"`
}

// POST /api/team/documents/:id/preview — документ глазами читателя уровня level (0–7): то, что сервер отдал бы ему на самом деле.
// Ничего не сохраняет. POST, а не GET, потому что в теле — содержимое редактора (оно не помещается в адрес).
func (h *teamDocumentHandlers) preview(c *gin.Context) {
	id, ok := idParam(c, "id")
	if !ok {
		return
	}
	var req PreviewRequest
	if !bindJSONLimit(c, &req, maxDocumentBody) {
		return
	}
	res, err := h.svc.TeamPreview(c.Request.Context(), actorFrom(c), id, req.Level, req.Content)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, res)
}

// POST /api/team/documents/:id/lock — взять документ в работу или продлить свой замок.
func (h *teamDocumentHandlers) takeLock(c *gin.Context) {
	id, ok := idParam(c, "id")
	if !ok {
		return
	}
	l, err := h.svc.TakeLock(c.Request.Context(), actorFrom(c), id)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, LockResponse{Lock: l})
}

// DELETE /api/team/documents/:id/lock — снять замок (свой; чужой — Редактор и Директорат).
func (h *teamDocumentHandlers) releaseLock(c *gin.Context) {
	id, ok := idParam(c, "id")
	if !ok {
		return
	}
	if err := h.svc.ReleaseLock(c.Request.Context(), actorFrom(c), id); err != nil {
		h.fail(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// GET /api/team/documents/:id/versions?page=&per_page=
func (h *teamDocumentHandlers) versions(c *gin.Context) {
	id, ok := idParam(c, "id")
	if !ok {
		return
	}
	page, ok := intParam(c, "page")
	if !ok {
		return
	}
	perPage, ok := intParam(c, "per_page")
	if !ok {
		return
	}
	res, err := h.svc.Versions(c.Request.Context(), actorFrom(c), id, derefOr(page, 0), derefOr(perPage, 0))
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, res)
}

func derefOr(p *int, def int) int {
	if p == nil {
		return def
	}
	return *p
}

// GET /api/team/documents/:id/versions/:vid — снимок целиком.
func (h *teamDocumentHandlers) version(c *gin.Context) {
	id, ok := idParam(c, "id")
	if !ok {
		return
	}
	vid, ok := idParam(c, "vid")
	if !ok {
		return
	}
	v, err := h.svc.GetVersion(c.Request.Context(), actorFrom(c), id, vid)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, VersionResponse{Version: v})
}

// GET /api/team/documents/:id/versions/:vid/diff?against=live|<номер версии> — разница по блокам и полям.
func (h *teamDocumentHandlers) diff(c *gin.Context) {
	id, ok := idParam(c, "id")
	if !ok {
		return
	}
	vid, ok := idParam(c, "vid")
	if !ok {
		return
	}
	var against int64
	if raw := c.Query("against"); raw != "" && raw != "live" {
		n, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || n < 1 {
			FailFields(c, http.StatusBadRequest, CodeBadRequest, "Некорректные параметры запроса", map[string]string{"against": "live или номер версии"})
			return
		}
		against = n
	}
	d, err := h.svc.DiffVersions(c.Request.Context(), actorFrom(c), id, vid, against)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, DiffResponse{Diff: d})
}

// POST /api/team/documents/:id/versions/:vid/restore — откатить документ к снимку (новая редакция).
func (h *teamDocumentHandlers) restore(c *gin.Context) {
	id, ok := idParam(c, "id")
	if !ok {
		return
	}
	vid, ok := idParam(c, "vid")
	if !ok {
		return
	}
	res, err := h.svc.TeamRestore(c.Request.Context(), actorFrom(c), id, vid)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, res)
}
