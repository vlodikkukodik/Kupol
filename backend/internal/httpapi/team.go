package httpapi

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"kupol/internal/accounts"
)

type teamHandlers struct {
	svc *accounts.Service
	log *slog.Logger
}

type memberDTO struct {
	Login       string    `json:"login"`
	Level       int       `json:"level"`
	LevelName   string    `json:"level_name"`
	Directorate bool      `json:"directorate"`
	Roles       []roleDTO `json:"roles"`
	CreatedAt   time.Time `json:"created_at"`
}

func toMemberDTO(m accounts.Member) memberDTO {
	return memberDTO{
		Login: m.Login, Level: m.Level, LevelName: m.LevelName, Directorate: m.Directorate,
		Roles: toRoleDTOs(m.Roles), CreatedAt: m.CreatedAt,
	}
}

type capabilityDTO struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type roleInfoDTO struct {
	ID           string          `json:"id"`
	Name         string          `json:"name"`
	Capabilities []capabilityDTO `json:"capabilities"`
}

func capabilityDTOs(caps []accounts.Capability) []capabilityDTO {
	out := make([]capabilityDTO, len(caps))
	for i, c := range caps {
		out[i] = capabilityDTO{ID: string(c), Name: c.Name()}
	}
	return out
}

// GET /api/team/roles — какие бывают роли и что каждая позволяет. Нужна тем, кто состоит в команде: их страница
// «Роли и права» показывает это без знания устройства ролей на стороне интерфейса.
func (h *teamHandlers) roles(c *gin.Context) {
	roles := make([]roleInfoDTO, len(accounts.AllRoles))
	for i, r := range accounts.AllRoles {
		roles[i] = roleInfoDTO{ID: string(r), Name: r.Name(), Capabilities: capabilityDTOs(r.Capabilities())}
	}
	c.JSON(http.StatusOK, gin.H{
		"roles":        roles,
		"directorate":  gin.H{"name": accounts.DirectorateName, "capabilities": capabilityDTOs(accounts.AllCapabilities)},
		"capabilities": capabilityDTOs(accounts.AllCapabilities),
	})
}

func (h *teamHandlers) fail(c *gin.Context, err error) {
	var ve *accounts.ValidationError
	switch {
	case errors.As(err, &ve):
		FailFields(c, http.StatusBadRequest, CodeBadRequest, "Некорректные параметры запроса", ve.Fields)
	case errors.Is(err, accounts.ErrUserNotFound):
		Fail(c, http.StatusNotFound, CodeNotFound, "Пользователь не найден")
	default:
		h.log.Error("ошибка обработчика команды", "err", err, "path", c.Request.URL.Path, "request_id", RequestID(c))
		Fail(c, http.StatusInternalServerError, CodeInternal, "Сбой архива")
	}
}

// GET /api/team/members?q=&role=&staff=1&page=&per_page= — пользователи и их роли (Директорату).
func (h *teamHandlers) members(c *gin.Context) {
	q := accounts.TeamQuery{Query: c.Query("q"), Role: accounts.Role(c.Query("role"))}
	switch c.Query("staff") {
	case "", "0":
	case "1":
		q.Staff = true
	default:
		FailFields(c, http.StatusBadRequest, CodeBadRequest, "Некорректные параметры запроса", map[string]string{"staff": "0 или 1"})
		return
	}
	for name, dst := range map[string]*int{"page": &q.Page, "per_page": &q.PerPage} {
		raw := c.Query(name)
		if raw == "" {
			continue
		}
		n, err := strconv.Atoi(raw)
		if err != nil {
			FailFields(c, http.StatusBadRequest, CodeBadRequest, "Некорректные параметры запроса", map[string]string{name: "ожидается число"})
			return
		}
		*dst = n
	}
	page, err := h.svc.ListMembers(c.Request.Context(), q)
	if err != nil {
		h.fail(c, err)
		return
	}
	members := make([]memberDTO, len(page.Members))
	for i, m := range page.Members {
		members[i] = toMemberDTO(m)
	}
	c.JSON(http.StatusOK, gin.H{"members": members, "total": page.Total, "page": page.Page, "per_page": page.PerPage, "pages": page.Pages})
}

// roleParam разбирает роль из адреса; неизвестная роль — 404.
func roleParam(c *gin.Context) (accounts.Role, bool) {
	role, ok := accounts.ParseRole(c.Param("role"))
	if !ok {
		Fail(c, http.StatusNotFound, CodeNotFound, "Такой роли нет")
	}
	return role, ok
}

// PUT /api/team/members/:login/roles/:role — выдать роль. Повтор безопасен: changed=false.
func (h *teamHandlers) grant(c *gin.Context) {
	role, ok := roleParam(c)
	if !ok {
		return
	}
	by := CurrentAuth(c).User.ID
	m, changed, err := h.svc.GrantRole(c.Request.Context(), c.Param("login"), role, &by)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"member": toMemberDTO(*m), "changed": changed})
}

// DELETE /api/team/members/:login/roles/:role — снять роль.
func (h *teamHandlers) revoke(c *gin.Context) {
	role, ok := roleParam(c)
	if !ok {
		return
	}
	by := CurrentAuth(c).User.ID
	m, changed, err := h.svc.RevokeRole(c.Request.Context(), c.Param("login"), role, &by)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"member": toMemberDTO(*m), "changed": changed})
}
