package httpapi

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"kupol/internal/documents"
)

// Экспорт документа в файл и загрузка документа из файла в team panel (этап 3.6).

// GET /api/team/documents/:id/export?format=json|md — файл для скачивания. Права — как при чтении документа в панели.
func (h *teamDocumentHandlers) export(c *gin.Context) {
	id, ok := idParam(c, "id")
	if !ok {
		return
	}
	format := c.DefaultQuery("format", string(documents.ExportJSON))
	f, err := h.svc.TeamExport(c.Request.Context(), actorFrom(c), id, documents.ExportFormat(format))
	if err != nil {
		h.fail(c, err)
		return
	}
	// Имя файла — только латиница, цифры и «-»: шифр в адресной записи, поэтому кавычек и юникода здесь нет.
	c.Header("Content-Disposition", `attachment; filename="`+f.Filename+`"`)
	c.Data(http.StatusOK, f.ContentType, f.Data)
}

// POST /api/team/documents/import[?dry_run=1] — черновик из файла формата загрузки (тело — сам файл, один документ).
// С dry_run файл только проверяется: те же замечания с путями, но ничего не создаётся.
func (h *teamDocumentHandlers) importDocument(c *gin.Context) {
	dry := false
	switch c.Query("dry_run") {
	case "", "0":
	case "1":
		dry = true
	default:
		FailFields(c, http.StatusBadRequest, CodeBadRequest, "Некорректные параметры запроса", map[string]string{"dry_run": "0 или 1"})
		return
	}
	raw, ok := readJSONBody(c, maxDocumentBody)
	if !ok {
		return
	}
	res, err := h.svc.TeamImport(c.Request.Context(), actorFrom(c), raw, dry)
	if err != nil {
		h.fail(c, err)
		return
	}
	status := http.StatusCreated
	if dry {
		status = http.StatusOK
	}
	c.JSON(status, res)
}
