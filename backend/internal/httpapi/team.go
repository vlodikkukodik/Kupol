package httpapi

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"kupol/internal/accounts"
	"kupol/internal/i18n"
)

type teamHandlers struct {
	svc *accounts.Service
	log *slog.Logger
}

type MemberDTO struct {
	Login       string    `json:"login"`
	Level       int       `json:"level"`
	LevelName   string    `json:"level_name"`
	Directorate bool      `json:"directorate"`
	Roles       []RoleDTO `json:"roles"`
	CreatedAt   time.Time `json:"created_at"`
}

func toMemberDTO(m accounts.Member, lang i18n.Lang) MemberDTO {
	return MemberDTO{
		Login: m.Login, Level: m.Level, LevelName: accounts.LevelNameIn(lang, m.Level, m.Directorate), Directorate: m.Directorate,
		Roles: toRoleDTOs(m.Roles, lang), CreatedAt: m.CreatedAt,
	}
}

type CapabilityDTO struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type RoleInfoDTO struct {
	ID           string          `json:"id"`
	Name         string          `json:"name"`
	Capabilities []CapabilityDTO `json:"capabilities"`
}

func CapabilityDTOs(caps []accounts.Capability, lang i18n.Lang) []CapabilityDTO {
	out := make([]CapabilityDTO, len(caps))
	for i, c := range caps {
		out[i] = CapabilityDTO{ID: string(c), Name: c.NameIn(lang)}
	}
	return out
}

// GET /api/team/roles — какие бывают роли и что каждая позволяет. Нужна тем, кто состоит в команде: их страница
// «Роли и права» показывает это без знания устройства ролей на стороне интерфейса.
func (h *teamHandlers) roles(c *gin.Context) {
	lang := Lang(c)
	roles := make([]RoleInfoDTO, len(accounts.AllRoles))
	for i, r := range accounts.AllRoles {
		roles[i] = RoleInfoDTO{ID: string(r), Name: r.NameIn(lang), Capabilities: CapabilityDTOs(r.Capabilities(), lang)}
	}
	c.JSON(http.StatusOK, TeamRolesResponse{
		Roles:        roles,
		Directorate:  DirectorateInfoDTO{Name: lang.Translate(accounts.DirectorateName), Capabilities: CapabilityDTOs(accounts.AllCapabilities, lang)},
		Capabilities: CapabilityDTOs(accounts.AllCapabilities, lang),
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
	members := make([]MemberDTO, len(page.Members))
	for i, m := range page.Members {
		members[i] = toMemberDTO(m, Lang(c))
	}
	c.JSON(http.StatusOK, TeamMembersResponse{Members: members, Total: page.Total, Page: page.Page, PerPage: page.PerPage, Pages: page.Pages})
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
	c.JSON(http.StatusOK, MemberRoleResponse{Member: toMemberDTO(*m, Lang(c)), Changed: changed})
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
	c.JSON(http.StatusOK, MemberRoleResponse{Member: toMemberDTO(*m, Lang(c)), Changed: changed})
}
