package httpapi

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"kupol/internal/documents"
)

// GET /api/search?q=…&type=&class=&department=&category=&containment=&year_from=&year_to=&status=&page=&per_page= — поиск по
// названиям, шифрам и тексту блоков. Работает для любого читателя, включая Гражданина: в выдаче ровно то, что он вправе
// видеть при чтении (documents/search.go). Ответ зависит от допуска, поэтому не кэшируется (no-store на весь /api).
func (h *documentHandlers) search(c *gin.Context) {
	q := documents.SearchQuery{
		Text: c.Query("q"),
		ListQuery: documents.ListQuery{
			Type:        c.Query("type"),
			Department:  c.Query("department"),
			Category:    c.Query("category"),
			Containment: c.Query("containment"),
			Status:      c.Query("status"),
		},
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
		if *perPage == 0 {
			q.PerPage = -1 // явный ноль — ошибка, а не «по умолчанию»
		}
	}
	res, err := h.svc.Search(c.Request.Context(), viewerFrom(c), q)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, res)
}
