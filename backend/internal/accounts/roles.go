package accounts

import "slices"

// Роли команды (спецификация §12). Выдаёт Директорат. Директорат — не роль, а флаг пользователя;
// он подразумевает все роли и все права.
type Role string

const (
	RoleAuthor    Role = "author"    // пишет документы, отправляет их на проверку
	RoleEditor    Role = "editor"    // проверяет, публикует, правит опубликованное
	RoleModerator Role = "moderator" // комментарии и жалобы (с этапа 4)
	RoleArchivist Role = "archivist" // глоссарий, шаблоны, теги и хронология (с этапа 5)
)

// AllRoles — роли в порядке показа.
var AllRoles = []Role{RoleAuthor, RoleEditor, RoleModerator, RoleArchivist}

var roleNames = map[Role]string{
	RoleAuthor:    "Автор",
	RoleEditor:    "Редактор",
	RoleModerator: "Модератор",
	RoleArchivist: "Архивариус",
}

// Valid — известная роль.
func (r Role) Valid() bool { _, ok := roleNames[r]; return ok }

// Name — название роли для интерфейса.
func (r Role) Name() string { return roleNames[r] }

// ParseRole разбирает имя роли (author, editor, moderator, archivist).
func ParseRole(s string) (Role, bool) {
	r := Role(s)
	return r, r.Valid()
}

// Capability — то, что пользователь может делать в team panel. Права проверяются на сервере;
// интерфейс получает список готовых прав и ничего не выводит из ролей сам.
type Capability string

const (
	CapTeamPanel       Capability = "team_panel"       // войти в team panel
	CapWriteDrafts     Capability = "write_drafts"     // создавать документы и править черновики
	CapReview          Capability = "review"           // проверять документы, писать замечания, выносить вердикт
	CapPublish         Capability = "publish"          // публиковать (кроме собственных документов)
	CapEditPublished   Capability = "edit_published"   // править опубликованное «на месте»
	CapManageGlossary  Capability = "manage_glossary"  // вести глоссарий канона
	CapManageTemplates Capability = "manage_templates" // вести шаблоны документов и наборы блоков
	CapManageTeam      Capability = "manage_team"      // выдавать и снимать роли
)

// AllCapabilities — права в порядке показа.
var AllCapabilities = []Capability{
	CapTeamPanel, CapWriteDrafts, CapReview, CapPublish, CapEditPublished,
	CapManageGlossary, CapManageTemplates, CapManageTeam,
}

var capabilityNames = map[Capability]string{
	CapTeamPanel:       "Входить в панель команды",
	CapWriteDrafts:     "Создавать документы и править черновики",
	CapReview:          "Проверять документы и выносить вердикт",
	CapPublish:         "Публиковать проверенные документы (не свои)",
	CapEditPublished:   "Править опубликованное на месте",
	CapManageGlossary:  "Вести глоссарий канона",
	CapManageTemplates: "Вести шаблоны и наборы блоков",
	CapManageTeam:      "Выдавать и снимать роли",
}

// Name — описание права для интерфейса.
func (c Capability) Name() string { return capabilityNames[c] }

// roleCapabilities — что даёт каждая роль. Директорат получает всё (см. User.Can).
var roleCapabilities = map[Role][]Capability{
	RoleAuthor:    {CapTeamPanel, CapWriteDrafts},
	RoleEditor:    {CapTeamPanel, CapWriteDrafts, CapReview, CapPublish, CapEditPublished, CapManageGlossary, CapManageTemplates},
	RoleModerator: {CapTeamPanel},
	RoleArchivist: {CapTeamPanel, CapManageGlossary, CapManageTemplates},
}

// Capabilities — права, которые даёт роль.
func (r Role) Capabilities() []Capability {
	return append([]Capability(nil), roleCapabilities[r]...)
}

// HasRole — есть ли у пользователя роль. Директорат подразумевает все роли.
func (u User) HasRole(r Role) bool {
	if u.Directorate {
		return true
	}
	return slices.Contains(u.Roles, r)
}

// Can — может ли пользователь делать это. Директорат может всё; остальным права дают роли.
// Роли должны быть загружены (их подгружают Authenticate, Login и RestoreAccess).
func (u User) Can(c Capability) bool {
	if u.Directorate {
		return capabilityNames[c] != ""
	}
	for _, r := range u.Roles {
		if slices.Contains(roleCapabilities[r], c) {
			return true
		}
	}
	return false
}

// Capabilities — все права пользователя в порядке показа.
func (u User) Capabilities() []Capability {
	out := []Capability{}
	for _, c := range AllCapabilities {
		if u.Can(c) {
			out = append(out, c)
		}
	}
	return out
}

// IsTeam — состоит ли пользователь в команде (есть хоть одна роль или Директорат).
func (u User) IsTeam() bool { return u.Can(CapTeamPanel) }

// sortRoles упорядочивает роли как в AllRoles и убирает неизвестные.
func sortRoles(have map[Role]bool) []Role {
	out := []Role{}
	for _, r := range AllRoles {
		if have[r] {
			out = append(out, r)
		}
	}
	return out
}
