package accounts

import (
	"context"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"

	"kupol/internal/audit"
)

// userRole — строка таблицы user_roles.
type userRole struct {
	UserID    int64  `gorm:"primaryKey"`
	Role      string `gorm:"primaryKey"`
	GrantedBy *int64
	GrantedAt time.Time
}

func (userRole) TableName() string { return "user_roles" }

// loadRoles подгружает пользователю роли команды.
func (s *Service) loadRoles(db *gorm.DB, u *User) error {
	roles, err := s.rolesOf(db, u.ID)
	if err != nil {
		return err
	}
	u.Roles = roles[u.ID]
	if u.Roles == nil {
		u.Roles = []Role{}
	}
	return nil
}

// rolesOf возвращает роли указанных пользователей (в порядке AllRoles); у кого ролей нет — в ответе нет записи.
func (s *Service) rolesOf(db *gorm.DB, ids ...int64) (map[int64][]Role, error) {
	out := map[int64][]Role{}
	if len(ids) == 0 {
		return out, nil
	}
	var rows []userRole
	if err := db.Where("user_id IN ?", ids).Find(&rows).Error; err != nil {
		return nil, err
	}
	have := map[int64]map[Role]bool{}
	for _, r := range rows {
		if have[r.UserID] == nil {
			have[r.UserID] = map[Role]bool{}
		}
		have[r.UserID][Role(r.Role)] = true
	}
	for id, set := range have {
		out[id] = sortRoles(set)
	}
	return out, nil
}

// Member — участник списка «Команда»: пользователь и его роли.
type Member struct {
	Login       string
	Level       int
	LevelName   string
	Directorate bool
	Roles       []Role
	CreatedAt   time.Time
}

func memberOf(u User, roles []Role) Member {
	if roles == nil {
		roles = []Role{}
	}
	return Member{
		Login: u.Login, Level: u.Level, LevelName: u.LevelName(), Directorate: u.Directorate,
		Roles: roles, CreatedAt: u.CreatedAt.UTC(),
	}
}

// TeamQuery — параметры списка «Команда».
type TeamQuery struct {
	Query   string // часть логина, без учёта регистра
	Role    Role   // только с этой ролью
	Staff   bool   // только те, у кого есть роль или Директорат
	Page    int
	PerPage int
}

const (
	defaultTeamPerPage = 50
	maxTeamPerPage     = 100
	maxTeamQueryRunes  = 24 // как максимальная длина логина
)

// TeamPage — страница списка «Команда».
type TeamPage struct {
	Members []Member
	Total   int64
	Page    int
	PerPage int
	Pages   int
}

// likeEscaper экранирует символы шаблона LIKE: «%» и «_» в поиске — обычные символы (логин может содержать «_»).
var likeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

// ListMembers возвращает страницу пользователей с их ролями. Неверные параметры — ValidationError.
func (s *Service) ListMembers(ctx context.Context, q TeamQuery) (*TeamPage, error) {
	if q.Role != "" && !q.Role.Valid() {
		return nil, fieldError("role", "неизвестная роль")
	}
	if q.Page < 0 {
		return nil, fieldError("page", "номер страницы не может быть отрицательным")
	}
	if q.Page == 0 {
		q.Page = 1
	}
	if q.PerPage < 0 || q.PerPage > maxTeamPerPage {
		return nil, fieldError("per_page", "размер страницы — от 1 до 100")
	}
	if q.PerPage == 0 {
		q.PerPage = defaultTeamPerPage
	}
	q.Query = strings.TrimSpace(q.Query)
	if len([]rune(q.Query)) > maxTeamQueryRunes {
		return nil, fieldError("q", "слишком длинный запрос")
	}

	db := s.db.WithContext(ctx)
	base := db.Model(&User{})
	if q.Query != "" {
		base = base.Where(`login::text ILIKE ? ESCAPE '\'`, "%"+likeEscaper.Replace(q.Query)+"%")
	}
	if q.Role != "" {
		base = base.Where("EXISTS (SELECT 1 FROM user_roles r WHERE r.user_id = users.id AND r.role = ?)", string(q.Role))
	}
	if q.Staff {
		base = base.Where("(directorate OR EXISTS (SELECT 1 FROM user_roles r WHERE r.user_id = users.id))")
	}

	var total int64
	if err := base.Count(&total).Error; err != nil {
		return nil, err
	}
	var users []User
	if err := base.Order("lower(login::text), id").Limit(q.PerPage).Offset((q.Page - 1) * q.PerPage).Find(&users).Error; err != nil {
		return nil, err
	}
	ids := make([]int64, len(users))
	for i, u := range users {
		ids[i] = u.ID
	}
	roles, err := s.rolesOf(db, ids...)
	if err != nil {
		return nil, err
	}
	page := &TeamPage{Members: make([]Member, len(users)), Total: total, Page: q.Page, PerPage: q.PerPage}
	for i, u := range users {
		page.Members[i] = memberOf(u, roles[u.ID])
	}
	page.Pages = int((total + int64(q.PerPage) - 1) / int64(q.PerPage))
	return page, nil
}

// MemberByLogin возвращает пользователя с ролями или ErrUserNotFound.
func (s *Service) MemberByLogin(ctx context.Context, login string) (*Member, error) {
	u, err := s.findByLogin(ctx, login)
	if err != nil {
		return nil, err
	}
	roles, err := s.rolesOf(s.db.WithContext(ctx), u.ID)
	if err != nil {
		return nil, err
	}
	m := memberOf(*u, roles[u.ID])
	return &m, nil
}

// GrantRole выдаёт роль. changed=false, если роль уже была (операция идемпотентна).
// grantedBy — кто выдал (nil — команда на сервере).
func (s *Service) GrantRole(ctx context.Context, login string, role Role, grantedBy *int64) (member *Member, changed bool, err error) {
	if !role.Valid() {
		return nil, false, errors.New("неизвестная роль")
	}
	u, err := s.findByLogin(ctx, login)
	if err != nil {
		return nil, false, err
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Exec(
			`INSERT INTO user_roles (user_id, role, granted_by, granted_at) VALUES (?, ?, ?, ?) ON CONFLICT (user_id, role) DO NOTHING`,
			u.ID, string(role), grantedBy, s.now())
		if res.Error != nil {
			return res.Error
		}
		changed = res.RowsAffected == 1
		if !changed {
			return nil
		}
		return audit.Record(tx, s.now(), audit.RoleGranted, audit.Event{ActorID: grantedBy, TargetUserID: &u.ID, Details: audit.Details("role", string(role))})
	})
	if err != nil {
		return nil, false, err
	}
	if changed {
		s.log.Info("роль выдана", "user_id", u.ID, "role", string(role), "granted_by", grantedBy)
	}
	m, err := s.MemberByLogin(ctx, login)
	return m, changed, err
}

// RevokeRole снимает роль. changed=false, если её не было.
func (s *Service) RevokeRole(ctx context.Context, login string, role Role, revokedBy *int64) (member *Member, changed bool, err error) {
	if !role.Valid() {
		return nil, false, errors.New("неизвестная роль")
	}
	u, err := s.findByLogin(ctx, login)
	if err != nil {
		return nil, false, err
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Where("user_id = ? AND role = ?", u.ID, string(role)).Delete(&userRole{})
		if res.Error != nil {
			return res.Error
		}
		changed = res.RowsAffected == 1
		if !changed {
			return nil
		}
		return audit.Record(tx, s.now(), audit.RoleRevoked, audit.Event{ActorID: revokedBy, TargetUserID: &u.ID, Details: audit.Details("role", string(role))})
	})
	if err != nil {
		return nil, false, err
	}
	if changed {
		s.log.Info("роль снята", "user_id", u.ID, "role", string(role), "revoked_by", revokedBy)
	}
	m, err := s.MemberByLogin(ctx, login)
	return m, changed, err
}
