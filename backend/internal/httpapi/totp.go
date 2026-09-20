package httpapi

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"kupol/internal/accounts"
)

// Код из приложения (TOTP) в личном деле: все действия — только вошедшего, с подтверждением паролем. Логика — accounts/totp_service.go.

type totpPasswordRequest struct {
	Password string `json:"password"`
}

type totpCodeRequest struct {
	Code string `json:"code"`
}

type totpConfirmRequest struct {
	Password string `json:"password"`
	Code     string `json:"code"`
}

// totpFail: неверный код в личном деле — ошибка поля «code», а не «totp» (так называется поле входа).
func (h *authHandlers) totpFail(c *gin.Context, err error) {
	if errors.Is(err, accounts.ErrTOTPInvalid) {
		FailFields(c, http.StatusUnprocessableEntity, CodeTOTPInvalid, "Неверный код", map[string]string{"code": "Неверный или уже использованный код"})
		return
	}
	h.fail(c, err, "")
}

// GET /api/me/totp — включена ли защита и сколько одноразовых кодов осталось.
func (h *authHandlers) totpStatus(c *gin.Context) {
	u := CurrentAuth(c).User
	resp := TOTPStatusResponse{Enabled: u.TOTPEnabled()}
	if resp.Enabled {
		n, err := h.svc.RecoveryCodesLeft(c.Request.Context(), u.ID)
		if err != nil {
			h.fail(c, err, "")
			return
		}
		resp.RecoveryLeft = n
	}
	c.JSON(http.StatusOK, resp)
}

// POST /api/me/totp/setup {password} — начать подключение: секрет и ссылка для QR.
func (h *authHandlers) totpSetup(c *gin.Context) {
	var req totpPasswordRequest
	if !bindJSON(c, &req) {
		return
	}
	setup, err := h.svc.TOTPBegin(c.Request.Context(), CurrentAuth(c).User.ID, req.Password, clientInfo(c))
	if err != nil {
		h.totpFail(c, err)
		return
	}
	c.JSON(http.StatusOK, TOTPSetupResponse{Secret: setup.Secret, URI: setup.URI})
}

// POST /api/me/totp/enable {code} — подтвердить подключение первым кодом из приложения; отвечает одноразовыми кодами.
func (h *authHandlers) totpEnable(c *gin.Context) {
	var req totpCodeRequest
	if !bindJSON(c, &req) {
		return
	}
	a := CurrentAuth(c)
	codes, err := h.svc.TOTPEnable(c.Request.Context(), a.User.ID, a.Session.ID, req.Code)
	if err != nil {
		h.totpFail(c, err)
		return
	}
	c.JSON(http.StatusOK, TOTPCodesResponse{RecoveryCodes: codes})
}

// POST /api/me/totp/disable {password, code} — выключить защиту (код из приложения или одноразовый).
func (h *authHandlers) totpDisable(c *gin.Context) {
	var req totpConfirmRequest
	if !bindJSON(c, &req) {
		return
	}
	if err := h.svc.TOTPDisable(c.Request.Context(), CurrentAuth(c).User.ID, req.Password, req.Code, clientInfo(c)); err != nil {
		h.totpFail(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// POST /api/me/totp/recovery-codes {password, code} — выдать новые одноразовые коды; прежние перестают действовать.
func (h *authHandlers) totpRenew(c *gin.Context) {
	var req totpConfirmRequest
	if !bindJSON(c, &req) {
		return
	}
	codes, err := h.svc.TOTPRenewRecoveryCodes(c.Request.Context(), CurrentAuth(c).User.ID, req.Password, req.Code, clientInfo(c))
	if err != nil {
		h.totpFail(c, err)
		return
	}
	c.JSON(http.StatusOK, TOTPCodesResponse{RecoveryCodes: codes})
}
