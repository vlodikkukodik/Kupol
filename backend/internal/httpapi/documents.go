package httpapi

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"kupol/internal/documents"
)

type documentHandlers struct {
	svc *documents.Service
	log *slog.Logger
}

// viewerFrom определяет допуск читателя по сессии: Гражданин, пользователь с уровнем или Директорат.
func viewerFrom(c *gin.Context) documents.Viewer {
	a := CurrentAuth(c)
	if a == nil {
		return documents.Guest
	}
	return documents.Viewer{UserID: a.User.ID, UserLevel: a.User.Level, Directorate: a.User.Directorate}
}

// fail переводит ошибки сервиса документов в ответ API.
func (h *documentHandlers) fail(c *gin.Context, err error) {
	var (
		qe *documents.QueryError
		ad *documents.AccessDeniedError
	)
	switch {
	case errors.Is(err, documents.ErrNotFound):
		Fail(c, http.StatusNotFound, CodeNotFound, "Дело не найдено")
	case errors.As(err, &ad):
		FailAccessDenied(c, ad.RequiredLevel, documents.LevelName(ad.RequiredLevel))
	case errors.As(err, &qe):
		FailFields(c, http.StatusBadRequest, CodeBadRequest, "Некорректные параметры запроса", map[string]string{qe.Field: qe.Message})
	default:
		h.log.Error("ошибка обработчика документов", "err", err, "path", c.Request.URL.Path, "request_id", RequestID(c))
		Fail(c, http.StatusInternalServerError, CodeInternal, "Сбой архива")
	}
}

// intParam читает необязательный целый параметр запроса. При мусоре отвечает 400 и возвращает ok=false.
func intParam(c *gin.Context, name string) (val *int, ok bool) {
	raw := strings.TrimSpace(c.Query(name))
	if raw == "" {
		return nil, true
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		FailFields(c, http.StatusBadRequest, CodeBadRequest, "Некорректные параметры запроса",
			map[string]string{name: "ожидается целое число"})
		return nil, false
	}
	return &n, true
}

// GET /api/documents — каталог. Параметры: type, class, year_from, year_to, department, category,
// containment, status (только Директорат), sort, order (asc|desc), page, per_page.
func (h *documentHandlers) list(c *gin.Context) {
	q := documents.ListQuery{
		Type:        c.Query("type"),
		Department:  c.Query("department"),
		Category:    c.Query("category"),
		Containment: c.Query("containment"),
		Status:      c.Query("status"),
		Sort:        c.Query("sort"),
	}
	var ok bool
	if q.Class, ok = intParam(c, "class"); !ok {
		return
	}
	if q.YearFrom, ok = intParam(c, "year_from"); !ok {
		return
	}
	if q.YearTo, ok = intParam(c, "year_to"); !ok {
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
	if page != nil {
		q.Page = *page
	}
	if perPage != nil {
		q.PerPage = *perPage
		if *perPage == 0 { // 0 значил бы «по умолчанию»; явный ноль — ошибка
			q.PerPage = -1
		}
	}
	switch c.Query("order") {
	case "", "asc":
	case "desc":
		q.Desc = true
	default:
		FailFields(c, http.StatusBadRequest, CodeBadRequest, "Некорректные параметры запроса", map[string]string{"order": "asc или desc"})
		return
	}

	res, err := h.svc.List(c.Request.Context(), viewerFrom(c), q)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, res)
}

// GET /api/documents/recent?limit=N — лента «Поступило в ЦАК».
func (h *documentHandlers) recent(c *gin.Context) {
	limit := 10
	if n, ok := intParam(c, "limit"); !ok {
		return
	} else if n != nil {
		limit = *n
	}
	items, err := h.svc.Recent(c.Request.Context(), viewerFrom(c), limit)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, RecentResponse{Items: items})
}

// GET /api/documents/summary — счётчики по типам, отделам и классам для фильтров и «папок».
func (h *documentHandlers) summary(c *gin.Context) {
	s, err := h.svc.Summary(c.Request.Context(), viewerFrom(c))
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, s)
}

// GET /api/documents/:ref — документ. ref — шифр в любой раскладке: О-041, O-041, o-41.
// Закрытые блоки в ответ не попадают; закрытый документ — 404 или 403 по настройке самого документа.
func (h *documentHandlers) get(c *gin.Context) {
	d, err := h.svc.Get(c.Request.Context(), viewerFrom(c), c.Param("ref"))
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, DocumentResponse{Document: d})
}
