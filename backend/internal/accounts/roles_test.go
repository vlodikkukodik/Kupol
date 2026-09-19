package accounts

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
)

// Ожидаемые права записаны здесь заново, а не выведены из кода: изменение таблицы прав в roles.go
// (например, случайно дали Автору право публиковать) должно ломать этот тест.
var wantCapabilities = map[Role][]Capability{
	RoleAuthor:    {CapTeamPanel, CapWriteDrafts},
	RoleEditor:    {CapTeamPanel, CapWriteDrafts, CapReview, CapPublish, CapEditPublished, CapManageGlossary, CapManageTemplates},
	RoleModerator: {CapTeamPanel},
	RoleArchivist: {CapTeamPanel, CapManageGlossary, CapManageTemplates},
}

func TestRoleCapabilitiesMatrix(t *testing.T) {
	for _, role := range AllRoles {
		u := User{Roles: []Role{role}}
		for _, c := range AllCapabilities {
			want := slices.Contains(wantCapabilities[role], c)
			if got := u.Can(c); got != want {
				t.Errorf("роль %s, право %s: Can = %v, ожидалось %v", role, c, got, want)
			}
		}
	}
}

func TestOnlyDirectorateManagesTeam(t *testing.T) {
	for _, role := range AllRoles {
		if (User{Roles: []Role{role}}).Can(CapManageTeam) {
			t.Errorf("роль %s не должна давать право выдавать роли", role)
		}
	}
	// даже все роли сразу — не Директорат
	if (User{Roles: AllRoles}).Can(CapManageTeam) {
		t.Error("все роли вместе не дают права выдавать роли")
	}
	if !(User{Directorate: true}).Can(CapManageTeam) {
		t.Error("Директорату выдавать роли можно")
	}
}

func TestDirectorateHasEveryRoleAndCapability(t *testing.T) {
	d := User{Directorate: true}
	for _, r := range AllRoles {
		if !d.HasRole(r) {
			t.Errorf("Директорат подразумевает роль %s", r)
		}
	}
	if got := d.Capabilities(); !slices.Equal(got, AllCapabilities) {
		t.Errorf("права Директората = %v, ожидалось все: %v", got, AllCapabilities)
	}
	if !d.IsTeam() {
		t.Error("Директорат состоит в команде")
	}
	// но право, которого не существует, не выдаётся никому
	if d.Can(Capability("fly")) {
		t.Error("неизвестное право не должно быть разрешено даже Директорату")
	}
}

func TestUserWithoutRolesHasNoCapabilities(t *testing.T) {
	u := User{Level: 6} // высокий уровень допуска прав команды не даёт
	if u.IsTeam() || len(u.Capabilities()) != 0 {
		t.Errorf("пользователь без ролей: IsTeam=%v, права=%v", u.IsTeam(), u.Capabilities())
	}
	for _, r := range AllRoles {
		if u.HasRole(r) {
			t.Errorf("роли %s у пользователя нет", r)
		}
	}
	if caps := u.Capabilities(); caps == nil {
		t.Error("Capabilities должен возвращать пустой список, а не nil (в JSON — [] вместо null)")
	}
}

func TestSeveralRolesUnionCapabilities(t *testing.T) {
	u := User{Roles: []Role{RoleAuthor, RoleArchivist}}
	want := []Capability{CapTeamPanel, CapWriteDrafts, CapManageGlossary, CapManageTemplates}
	if got := u.Capabilities(); !slices.Equal(got, want) {
		t.Errorf("права = %v, ожидалось %v", got, want)
	}
	if u.Can(CapPublish) || u.Can(CapReview) {
		t.Error("Автор + Архивариус не могут публиковать и проверять")
	}
}

func TestRolesAndCapabilitiesHaveNames(t *testing.T) {
	for _, r := range AllRoles {
		if !r.Valid() || r.Name() == "" {
			t.Errorf("у роли %q нет названия", r)
		}
		if got, ok := ParseRole(string(r)); !ok || got != r {
			t.Errorf("ParseRole(%q) = %v, %v", r, got, ok)
		}
	}
	for _, c := range AllCapabilities {
		if c.Name() == "" {
			t.Errorf("у права %q нет описания", c)
		}
	}
	for _, bad := range []string{"", "Author", "EDITOR", "admin", "directorate", " editor", "author "} {
		if _, ok := ParseRole(bad); ok {
			t.Errorf("ParseRole(%q) не должен принимать", bad)
		}
	}
}

func TestRoleCapabilitiesReturnsCopy(t *testing.T) {
	caps := RoleAuthor.Capabilities()
	caps[0] = CapManageTeam
	if RoleAuthor.Capabilities()[0] != CapTeamPanel {
		t.Error("изменение результата Capabilities() повлияло на таблицу прав")
	}
}

// ---------------------------------------------------------------- БД

func (e *env) userID(login string) int64 {
	e.t.Helper()
	u, err := e.svc.Find(context.Background(), login)
	if err != nil {
		e.t.Fatal(err)
	}
	return u.ID
}

func TestGrantAndRevokeRole(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.register("Vladislav", "секретный пароль 1")
	admin := e.register("Direktor", "секретный пароль 2")
	by := e.userID(admin.User.Login)

	m, changed, err := e.svc.GrantRole(ctx, "vladislav", RoleEditor, &by) // без учёта регистра
	if err != nil || !changed {
		t.Fatalf("выдача: changed=%v err=%v", changed, err)
	}
	if len(m.Roles) != 1 || m.Roles[0] != RoleEditor || m.Login != "Vladislav" {
		t.Fatalf("участник после выдачи: %+v", m)
	}
	var grantedBy *int64
	if err := e.db.Raw("SELECT granted_by FROM user_roles WHERE role = 'editor'").Scan(&grantedBy).Error; err != nil || grantedBy == nil || *grantedBy != by {
		t.Errorf("кто выдал: %v, %v (ожидался %d)", grantedBy, err, by)
	}

	// повторная выдача идемпотентна и ничего не меняет
	_, changed, err = e.svc.GrantRole(ctx, "Vladislav", RoleEditor, nil)
	if err != nil || changed {
		t.Errorf("повторная выдача: changed=%v err=%v", changed, err)
	}
	if n := e.count("SELECT count(*) FROM user_roles"); n != 1 {
		t.Errorf("строк ролей %d, ожидалась 1", n)
	}

	// вторая роль; порядок как в AllRoles, а не как выдавали
	if _, _, err := e.svc.GrantRole(ctx, "Vladislav", RoleAuthor, nil); err != nil {
		t.Fatal(err)
	}
	m, _ = e.svc.MemberByLogin(ctx, "Vladislav")
	if !slices.Equal(m.Roles, []Role{RoleAuthor, RoleEditor}) {
		t.Errorf("роли %v, ожидалось [author editor]", m.Roles)
	}

	m, changed, err = e.svc.RevokeRole(ctx, "Vladislav", RoleEditor, &by)
	if err != nil || !changed || !slices.Equal(m.Roles, []Role{RoleAuthor}) {
		t.Fatalf("снятие: changed=%v roles=%v err=%v", changed, m.Roles, err)
	}
	_, changed, err = e.svc.RevokeRole(ctx, "Vladislav", RoleEditor, &by)
	if err != nil || changed {
		t.Errorf("повторное снятие: changed=%v err=%v", changed, err)
	}
}

func TestGrantRoleErrors(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.register("Vladislav", "секретный пароль 1")
	if _, _, err := e.svc.GrantRole(ctx, "Vladislav", Role("admin"), nil); err == nil {
		t.Error("неизвестная роль должна отвергаться")
	}
	if _, _, err := e.svc.GrantRole(ctx, "nobody", RoleAuthor, nil); !errors.Is(err, ErrUserNotFound) {
		t.Errorf("несуществующий пользователь: %v", err)
	}
	if _, _, err := e.svc.RevokeRole(ctx, "nobody", RoleAuthor, nil); !errors.Is(err, ErrUserNotFound) {
		t.Errorf("снятие у несуществующего: %v", err)
	}
	if n := e.count("SELECT count(*) FROM user_roles"); n != 0 {
		t.Errorf("неудачные операции оставили %d ролей", n)
	}
}

func TestRolesAreLoadedBySessionLoginAndRestore(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	reg := e.register("Vladislav", "секретный пароль 1")
	if len(reg.User.Roles) != 0 {
		t.Errorf("у нового пользователя ролей нет: %v", reg.User.Roles)
	}
	if _, _, err := e.svc.GrantRole(ctx, "Vladislav", RoleEditor, nil); err != nil {
		t.Fatal(err)
	}
	if _, _, err := e.svc.GrantRole(ctx, "Vladislav", RoleAuthor, nil); err != nil {
		t.Fatal(err)
	}

	// сессия, созданная до выдачи роли, видит её сразу: роли читаются при каждой проверке
	a, err := e.svc.Authenticate(ctx, reg.Token)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(a.User.Roles, []Role{RoleAuthor, RoleEditor}) || !a.User.Can(CapPublish) {
		t.Errorf("Authenticate: роли %v", a.User.Roles)
	}

	res, err := e.svc.Login(ctx, "Vladislav", "секретный пароль 1", e.freshIP())
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(res.User.Roles, []Role{RoleAuthor, RoleEditor}) {
		t.Errorf("Login: роли %v", res.User.Roles)
	}

	rest, err := e.svc.RestoreAccess(ctx, "Vladislav", reg.BackupCode, "новый пароль 3", e.freshIP())
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(rest.User.Roles, []Role{RoleAuthor, RoleEditor}) {
		t.Errorf("RestoreAccess: роли %v", rest.User.Roles)
	}

	// снятая роль перестаёт действовать на уже открытой сессии
	if _, _, err := e.svc.RevokeRole(ctx, "Vladislav", RoleEditor, nil); err != nil {
		t.Fatal(err)
	}
	a, err = e.svc.Authenticate(ctx, rest.Token)
	if err != nil {
		t.Fatal(err)
	}
	if a.User.Can(CapPublish) || !a.User.Can(CapWriteDrafts) {
		t.Errorf("после снятия Редактора: права %v", a.User.Capabilities())
	}
}

func TestRolesDisappearWithAccountAndKeepAfterGranterDeleted(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.register("Direktor", "секретный пароль 2")
	e.register("Vladislav", "секретный пароль 1")
	by := e.userID("Direktor")
	if _, _, err := e.svc.GrantRole(ctx, "Vladislav", RoleAuthor, &by); err != nil {
		t.Fatal(err)
	}

	// выдавший удалил аккаунт: роль остаётся, «кто выдал» пустеет
	if err := e.svc.DeleteAccount(ctx, by, "секретный пароль 2", e.freshIP()); err != nil {
		t.Fatal(err)
	}
	if n := e.count("SELECT count(*) FROM user_roles WHERE granted_by IS NULL AND role = 'author'"); n != 1 {
		t.Errorf("после удаления выдавшего роль должна остаться с пустым granted_by, найдено %d", n)
	}

	// удаление самого пользователя убирает его роли
	if err := e.svc.DeleteAccount(ctx, e.userID("Vladislav"), "секретный пароль 1", e.freshIP()); err != nil {
		t.Fatal(err)
	}
	if n := e.count("SELECT count(*) FROM user_roles"); n != 0 {
		t.Errorf("после удаления аккаунта осталось %d ролей", n)
	}
}

func TestListMembers(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	logins := []string{"Anna", "boris", "Vera_1", "Vera-2", "Zed", "100pct"}
	for _, l := range logins {
		e.register(l, "секретный пароль 1")
	}
	must := func(_ *Member, _ bool, err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(e.svc.GrantRole(ctx, "boris", RoleEditor, nil))
	must(e.svc.GrantRole(ctx, "boris", RoleAuthor, nil))
	must(e.svc.GrantRole(ctx, "Zed", RoleArchivist, nil))
	if err := e.svc.SetDirectorate(ctx, "Anna", true); err != nil {
		t.Fatal(err)
	}

	list := func(q TeamQuery) *TeamPage {
		t.Helper()
		p, err := e.svc.ListMembers(ctx, q)
		if err != nil {
			t.Fatalf("%+v: %v", q, err)
		}
		return p
	}
	loginsOf := func(p *TeamPage) []string {
		out := []string{}
		for _, m := range p.Members {
			out = append(out, m.Login)
		}
		return out
	}

	// порядок — по логину без учёта регистра
	all := list(TeamQuery{})
	if got, want := loginsOf(all), []string{"100pct", "Anna", "boris", "Vera_1", "Vera-2", "Zed"}; !slices.Equal(got, want) {
		t.Errorf("порядок %v, ожидалось %v", got, want)
	}
	if all.Total != 6 || all.Pages != 1 || all.Page != 1 || all.PerPage != 50 {
		t.Errorf("страница: %+v", all)
	}
	for _, m := range all.Members {
		if m.Login == "boris" && !slices.Equal(m.Roles, []Role{RoleAuthor, RoleEditor}) {
			t.Errorf("роли boris: %v", m.Roles)
		}
		if m.Roles == nil {
			t.Errorf("у %s Roles nil (в JSON нужен [])", m.Login)
		}
	}

	if got := loginsOf(list(TeamQuery{Query: "VE"})); !slices.Equal(got, []string{"Vera_1", "Vera-2"}) {
		t.Errorf("поиск «VE» без учёта регистра: %v", got)
	}
	// «_» и «%» в поиске — обычные символы, а не шаблон LIKE
	if got := loginsOf(list(TeamQuery{Query: "vera_"})); !slices.Equal(got, []string{"Vera_1"}) {
		t.Errorf("поиск «vera_»: %v", got)
	}
	if got := loginsOf(list(TeamQuery{Query: "%"})); len(got) != 0 {
		t.Errorf("поиск «%%» не должен находить всех: %v", got)
	}
	if got := loginsOf(list(TeamQuery{Query: "  zed  "})); !slices.Equal(got, []string{"Zed"}) {
		t.Errorf("поиск с пробелами: %v", got)
	}
	if got := loginsOf(list(TeamQuery{Role: RoleEditor})); !slices.Equal(got, []string{"boris"}) {
		t.Errorf("фильтр по роли: %v", got)
	}
	if got := loginsOf(list(TeamQuery{Staff: true})); !slices.Equal(got, []string{"Anna", "boris", "Zed"}) {
		t.Errorf("только команда: %v", got)
	}
	if got := loginsOf(list(TeamQuery{Staff: true, Query: "z"})); !slices.Equal(got, []string{"Zed"}) {
		t.Errorf("команда + поиск: %v", got)
	}

	// пагинация
	p := list(TeamQuery{PerPage: 4})
	if p.Total != 6 || p.Pages != 2 || len(p.Members) != 4 {
		t.Errorf("первая страница: %+v", p)
	}
	p = list(TeamQuery{PerPage: 4, Page: 2})
	if len(p.Members) != 2 || p.Members[0].Login != "Vera-2" && p.Members[0].Login != "Zed" {
		t.Errorf("вторая страница: %v", loginsOf(p))
	}
	if p = list(TeamQuery{Page: 9}); len(p.Members) != 0 || p.Total != 6 {
		t.Errorf("страница за пределами: %+v", p)
	}
}

func TestListMembersRejectsBadParameters(t *testing.T) {
	e := newEnv(t)
	for name, q := range map[string]TeamQuery{
		"role":     {Role: "admin"},
		"page":     {Page: -1},
		"per_page": {PerPage: 101},
		"q":        {Query: strings.Repeat("я", 25)},
	} {
		_, err := e.svc.ListMembers(context.Background(), q)
		var ve *ValidationError
		if !errors.As(err, &ve) || ve.Fields[name] == "" {
			t.Errorf("%s: ожидалась ошибка в поле, получено %v", name, err)
		}
	}
	if _, err := e.svc.ListMembers(context.Background(), TeamQuery{PerPage: -3}); err == nil {
		t.Error("отрицательный размер страницы должен отвергаться")
	}
}

func TestGrantRoleConcurrentlyGivesOneRow(t *testing.T) {
	e := newEnv(t)
	e.register("Vladislav", "секретный пароль 1")
	const n = 8
	errs := make(chan error, n)
	changedCh := make(chan bool, n)
	for range n {
		go func() {
			_, changed, err := e.svc.GrantRole(context.Background(), "Vladislav", RoleEditor, nil)
			errs <- err
			changedCh <- changed
		}()
	}
	changes := 0
	for range n {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
		if <-changedCh {
			changes++
		}
	}
	if changes != 1 {
		t.Errorf("роль выдана %d раз одновременно, ожидалась ровно 1 запись (changed=true)", changes)
	}
	if c := e.count("SELECT count(*) FROM user_roles"); c != 1 {
		t.Errorf("строк ролей %d", c)
	}
}
