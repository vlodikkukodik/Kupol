package httpapi

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"kupol/internal/documents"
)

// Скрытые коды (пасхалки, шаг 5.5) в team panel: список, создание и удаление — только Директорат
// (сервис сам это проверяет по Actor.Directorate); погашение кода читателем — POST /api/secret-codes/redeem
// в documents.go, не здесь.

type SecretCodesResponse struct {
	Codes []documents.SecretCodeOut `json:"codes"`
}

type SecretCodeResponse struct {
	Code documents.SecretCodeOut `json:"code"`
}

// GET /api/team/secret-codes — список заведённых кодов.
func (h *teamDocumentHandlers) secretCodes(c *gin.Context) {
	codes, err := h.svc.SecretCodeList(c.Request.Context(), actorFrom(c))
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, SecretCodesResponse{Codes: codes})
}

// POST /api/team/secret-codes — завести код.
func (h *teamDocumentHandlers) createSecretCode(c *gin.Context) {
	var in documents.SecretCodeInput
	if !bindJSONLimit(c, &in, maxDocumentBody) {
		return
	}
	code, err := h.svc.SecretCodeCreate(c.Request.Context(), actorFrom(c), in)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, SecretCodeResponse{Code: *code})
}

// DELETE /api/team/secret-codes/:id — удалить код.
func (h *teamDocumentHandlers) deleteSecretCode(c *gin.Context) {
	id, ok := idParam(c, "id")
	if !ok {
		return
	}
	if err := h.svc.SecretCodeDelete(c.Request.Context(), actorFrom(c), id); err != nil {
		h.fail(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}
