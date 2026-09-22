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
	// LevelUp — этот вход поднял уровень (XP перешёл порог) — повод показать штамп «ДОПУСК ПОВЫШЕН».
	LevelUp bool `json:"level_up"`
}

// RegisterResponse — POST /api/auth/register: пользователь и резервный код, который показывается ОДИН раз.
type RegisterResponse struct {
	User       UserDTO `json:"user"`
	BackupCode string  `json:"backup_code"`
}

// RestoreResponse — POST /api/auth/restore: НОВЫЙ резервный код (показывается один раз) и, если человек вошёл, он сам.
// User = null — у него включён код из приложения: пароль сменён, но входить нужно обычным путём, с кодом.
type RestoreResponse struct {
	User       *UserDTO `json:"user" tstype:"UserDTO | null"`
	BackupCode string   `json:"backup_code"`
}

// TOTPStatusResponse — GET /api/me/totp: состояние защиты кодом из приложения.
type TOTPStatusResponse struct {
	Enabled bool `json:"enabled"`
	// RecoveryLeft — сколько одноразовых кодов ещё не потрачено (0, если защита выключена).
	RecoveryLeft int `json:"recovery_left"`
}

// TOTPSetupResponse — POST /api/me/totp/setup: что показать при подключении. Secret — для ручного ввода в приложение,
// URI — ссылка otpauth://, которую кодирует QR. Защита ещё не включена: её включает первый верный код (POST …/enable).
type TOTPSetupResponse struct {
	Secret string `json:"secret"`
	URI    string `json:"uri"`
}

// TOTPCodesResponse — одноразовые коды на случай потери телефона; показываются ОДИН раз.
type TOTPCodesResponse struct {
	RecoveryCodes []string `json:"recovery_codes"`
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

// ReviewResponse — GET /api/team/documents/:id/review: ход рецензии и комментарии.
type ReviewResponse struct {
	Review *documents.ReviewInfo `json:"review" tstype:",required"`
}

// LintResponse — GET /api/team/documents/:id/lint.
type LintResponse struct {
	Lint *documents.LintReport `json:"lint" tstype:",required"`
}

// CommentResponse — комментарий рецензента.
type CommentResponse struct {
	Comment *documents.CommentOut `json:"comment" tstype:",required"`
}

// SubmitRequest — POST /api/team/documents/:id/submit. BaseRevision — редакция, которую видел автор.
type SubmitRequest struct {
	BaseRevision int `json:"base_revision"`
}

// NoteRequest — тело действий с необязательным пояснением (забрать с проверки, в архив, из архива).
type NoteRequest struct {
	Comment string `json:"comment"`
}

// VerdictRequest — POST /api/team/documents/:id/verdict. BaseRevision — редакция, которую читал рецензент.
type VerdictRequest struct {
	Verdict      string `json:"verdict" tstype:"'approve' | 'return' | 'reject'"`
	Comment      string `json:"comment"`
	BaseRevision int    `json:"base_revision"`
}

// AddCommentRequest — POST /api/team/documents/:id/comments. Без block_id комментарий относится к документу целиком.
type AddCommentRequest struct {
	BlockID *string `json:"block_id,omitempty"`
	Body    string  `json:"body"`
}

// ResolveCommentRequest — PUT /api/team/documents/:id/comments/:cid.
type ResolveCommentRequest struct {
	Resolved bool `json:"resolved"`
}

// SiteResponse — GET /api/site: публичные настройки сайта (контакты автора для страницы «О КУПОЛЕ»).
type SiteResponse struct {
	Site documents.SiteSettings `json:"site"`
}

// SiteSettingsResponse — GET/PUT /api/team/site: настройки для панели команды.
type SiteSettingsResponse struct {
	Site *documents.SiteSettingsOut `json:"site" tstype:",required"`
}

// TimelineResponse — GET /api/timeline: хронология «О КУПОЛЕ» для читателя.
type TimelineResponse struct {
	Items []documents.TimelineItem `json:"items"`
}

// TimelineEventsResponse — GET /api/team/timeline: все события для редактирования.
type TimelineEventsResponse struct {
	Items []documents.TimelineEventOut `json:"items"`
}

// TimelineEventResponse — событие хронологии.
type TimelineEventResponse struct {
	Event *documents.TimelineEventOut `json:"event" tstype:",required"`
}

// DashboardResponse — GET /api/team/dashboard: рабочий стол человека.
type DashboardResponse struct {
	Dashboard *documents.Dashboard `json:"dashboard" tstype:",required"`
}

// TemplatesResponse — GET /api/team/templates.
type TemplatesResponse struct {
	Items []documents.TemplateItem `json:"items" tstype:",required"`
}

// TemplateResponse — шаблон целиком (чтение, создание, правка).
type TemplateResponse struct {
	Template *documents.TemplateFull `json:"template" tstype:",required"`
}

// GlossaryResponse — GET /api/team/glossary.
type GlossaryResponse struct {
	Items []documents.TermOut `json:"items" tstype:",required"`
}

// TermResponse — термин глоссария (создание, правка).
type TermResponse struct {
	Term *documents.TermOut `json:"term" tstype:",required"`
}

// RemarksResponse — «пометки на полях» (GET /api/documents/:ref/remarks, GET /api/team/remarks/reported).
type RemarksResponse struct {
	Items []documents.RemarkOut `json:"items" tstype:",required"`
}

// RemarkResponse — одна пометка (POST /api/documents/:ref/remarks).
type RemarkResponse struct {
	Remark *documents.RemarkOut `json:"remark" tstype:",required"`
}

// RatingsResponse — оценки документа (GET/POST/DELETE /api/documents/:ref/ratings).
type RatingsResponse struct {
	Ratings *documents.DocumentRatings `json:"ratings" tstype:",required"`
}

// SetRatingRequest — POST /api/documents/:ref/ratings.
type SetRatingRequest struct {
	Rating documents.Rating `json:"rating"`
}

// UpdateTemplateRequest — PUT /api/team/templates/:id. Без content меняются только название и описание.
type UpdateTemplateRequest struct {
	Name        string                     `json:"name"`
	Description string                     `json:"description"`
	Content     *documents.TemplateContent `json:"content,omitempty"`
}
