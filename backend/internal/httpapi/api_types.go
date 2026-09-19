package httpapi

import "kupol/internal/documents"

// Контракт JSON API: все тела ответов — именованные структуры, а не gin.H. Из них командой `make types`
// (tygo, см. backend/tygo.yaml) генерируются TypeScript-типы фронтенда (frontend/src/api/generated/), поэтому
// сервер и интерфейс не могут разойтись в формах ответов: несоответствие ловит сборка фронтенда и CI.
//
// Ответы, которых нет здесь, уже определены рядом с предметом: списки документов и сводка — в пакете documents
// (ListResult, Summary), проверка здоровья — HealthResponse, ошибки — ErrorBody.

// SessionResponse — GET /api/auth/session. Для Гражданина (не вошёл) user = null: 401 на каждой загрузке страницы
// засорял бы консоль браузера и журналы.
type SessionResponse struct {
	User *UserDTO `json:"user" tstype:"UserDTO | null"`
}

// CaptchaResponse — GET /api/auth/captcha: вопрос анкеты для регистрации.
type CaptchaResponse struct {
	ID       string `json:"id"`
	Question string `json:"question"`
}

// LoginResponse — POST /api/auth/login.
type LoginResponse struct {
	User UserDTO `json:"user"`
}

// RegisterResponse — POST /api/auth/register и POST /api/auth/restore: пользователь и резервный код,
// который показывается ОДИН раз.
type RegisterResponse struct {
	User       UserDTO `json:"user"`
	BackupCode string  `json:"backup_code"`
}

// RecentResponse — GET /api/documents/recent: лента «Поступило в ЦАК».
type RecentResponse struct {
	Items []documents.Item `json:"items"`
}

// DocumentResponse — GET /api/documents/:ref: документ, собранный заново по допуску читателя.
type DocumentResponse struct {
	Document *documents.OutDocument `json:"document" tstype:",required"`
}

// DirectorateInfoDTO — что подразумевает Директорат (для страницы «Роли и права»).
type DirectorateInfoDTO struct {
	Name         string          `json:"name"`
	Capabilities []CapabilityDTO `json:"capabilities"`
}

// TeamRolesResponse — GET /api/team/roles: какие бывают роли и что каждая позволяет.
type TeamRolesResponse struct {
	Roles        []RoleInfoDTO      `json:"roles"`
	Directorate  DirectorateInfoDTO `json:"directorate"`
	Capabilities []CapabilityDTO    `json:"capabilities"`
}

// TeamMembersResponse — GET /api/team/members: страница пользователей с ролями.
type TeamMembersResponse struct {
	Members []MemberDTO `json:"members"`
	Total   int64       `json:"total"`
	Page    int         `json:"page"`
	PerPage int         `json:"per_page"`
	Pages   int         `json:"pages"`
}

// MemberRoleResponse — PUT/DELETE /api/team/members/:login/roles/:role. changed=false — роль уже была (или её не было).
type MemberRoleResponse struct {
	Member  MemberDTO `json:"member"`
	Changed bool      `json:"changed"`
}

// TeamDocumentResponse — создание и чтение документа в team panel.
type TeamDocumentResponse struct {
	Document *documents.TeamDocument `json:"document" tstype:",required"`
}

// LockResponse — POST /api/team/documents/:id/lock.
type LockResponse struct {
	Lock *documents.LockInfo `json:"lock" tstype:",required"`
}

// VersionResponse — GET /api/team/documents/:id/versions/:vid.
type VersionResponse struct {
	Version *documents.VersionFull `json:"version" tstype:",required"`
}

// DiffResponse — GET /api/team/documents/:id/versions/:vid/diff.
type DiffResponse struct {
	Diff *documents.Diff `json:"diff" tstype:",required"`
}
