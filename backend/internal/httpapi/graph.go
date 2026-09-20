package httpapi

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// GET /api/graph/:шифр?depth=1|2 — «доска с нитками»: документ, связанные с ним ссылками документы и сами нити. Только то, что
// читатель вправе видеть (documents/graph.go); документ, которого он не видит, — 404 как и при чтении.
func (h *documentHandlers) graph(c *gin.Context) {
	depth := 0
	if n, ok := intParam(c, "depth"); !ok {
		return
	} else if n != nil {
		depth = *n
		if depth == 0 {
			depth = -1 // явный ноль — ошибка, а не «по умолчанию»
		}
	}
	res, err := h.svc.Graph(c.Request.Context(), viewerFrom(c), c.Param("ref"), depth)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, res)
}
