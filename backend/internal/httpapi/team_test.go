package httpapi_test

import (
	"context"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"

	"kupol/internal/accounts"
)

const teamPassword = "секретный пароль 1"

type teamActor struct {
	name   string
	login  string
	client *client
}

// teamStackWithActors регистрирует по одному настоящему пользователю на каждую «позицию»:
// гость, обычный пользователь высокого уровня (без ролей), по одной роли, несколько ролей и Директорат.
func teamStackWithActors(t *testing.T) (*stack, map[string]*teamActor) {
	t.Helper()
	st := newStack(t, nil)
	ctx := context.Background()
	actors := map[string]*teamActor{"guest": {name: "guest", client: st.newClient(t)}}
	add := func(name string, roles []accounts.Role, directorate bool, level int) {
		c := st.newClient(t)
		login := "user_" + name
		if r := c.register(login, teamPassword); r.Code != http.StatusCreated {
			t.Fatalf("регистрация %s: %d %s", name, r.Code, r.Body)
		}
		if level > 0 {
			if err := st.svc.SetLevel(ctx, login, level); err != nil {
				t.Fatal(err)
			}
		}
		if directorate {
			if err := st.svc.SetDirectorate(ctx, login, true); err != nil {
				t.Fatal(err)
			}
		}
		for _, r := range roles {
			if _, _, err := st.svc.GrantRole(ctx, login, r, nil); err != nil {
				t.Fatal(err)
			}
		}
		actors[name] = &teamActor{name: name, login: login, client: c}
	}
	add("plain", nil, false, 6) // Особый Совет, но не команда
	add("author", []accounts.Role{accounts.RoleAuthor}, false, 0)
	add("editor", []accounts.Role{accounts.RoleEditor}, false, 0)
	add("moderator", []accounts.Role{accounts.RoleModerator}, false, 0)
	add("archivist", []accounts.Role{accounts.RoleArchivist}, false, 0)
	add("all_roles", accounts.AllRoles, false, 0)
	add("director", nil, true, 0)
	return st, actors
}

func teamMembersPath(login string) string {
	return "/api/team/members/" + url.PathEscape(login) + "/roles/"
}

func TestTeamEndpointsPermissionMatrix(t *testing.T) {
	_, actors := teamStackWithActors(t)

	// кто что видит: 200 или код отказа
	type want struct{ roles, members, grant, revoke int }
	matrix := map[string]want{
		"guest":     {401, 401, 401, 401},
		"plain":     {403, 403, 403, 403}, // высокий допуск (уровень 6) прав команды не даёт
		"author":    {200, 403, 403, 403},
		"editor":    {200, 403, 403, 403},
		"moderator": {200, 403, 403, 403},
		"archivist": {200, 403, 403, 403},
		"all_roles": {200, 403, 403, 403}, // все роли вместе — всё равно не Директорат
		"director":  {200, 200, 200, 200},
	}
	target := actors["plain"].login
	for name, w := range matrix {
		c := actors[name].client
		if got := c.do("GET", "/api/team/roles", nil).Code; got != w.roles {
			t.Errorf("%s: GET /team/roles = %d, ожидалось %d", name, got, w.roles)
		}
		if got := c.do("GET", "/api/team/members", nil).Code; got != w.members {
			t.Errorf("%s: GET /team/members = %d, ожидалось %d", name, got, w.members)
		}
		if got := c.do("PUT", teamMembersPath(target)+"author", nil).Code; got != w.grant {
			t.Errorf("%s: PUT роли = %d, ожидалось %d", name, got, w.grant)
		}
		if got := c.do("DELETE", teamMembersPath(target)+"author", nil).Code; got != w.revoke {
			t.Errorf("%s: DELETE роли = %d, ожидалось %d", name, got, w.revoke)
		}
	}
}

func TestForbiddenAndUnauthenticatedShapes(t *testing.T) {
	_, actors := teamStackWithActors(t)
	if r := actors["guest"].client.do("GET", "/api/team/members", nil); r.Code != 401 || r.errCode() != "unauthenticated" {
		t.Errorf("гость: %d %s", r.Code, r.errCode())
	}
	r := actors["editor"].client.do("GET", "/api/team/members", nil)
	if r.Code != 403 || r.errCode() != "forbidden" {
		t.Errorf("Редактор: %d %s", r.Code, r.errCode())
	}
	if strings.Contains(r.Body.String(), "user_") {
		t.Errorf("отказ не должен содержать данных: %s", r.Body)
	}
}

func TestGrantRoleTakesEffectImmediatelyOnOpenSession(t *testing.T) {
	_, actors := teamStackWithActors(t)
	dir, plain := actors["director"].client, actors["plain"]

	sessionUser := func(c *client) map[string]any {
		r := c.do("GET", "/api/auth/session", nil)
		u, _ := r.json()["user"].(map[string]any)
		return u
	}
	before := sessionUser(plain.client)
	if roles, _ := before["roles"].([]any); len(roles) != 0 {
		t.Fatalf("до выдачи роли есть: %v", roles)
	}
	if caps, _ := before["capabilities"].([]any); len(caps) != 0 {
		t.Fatalf("до выдачи права есть: %v", caps)
	}
	if plain.client.do("GET", "/api/team/roles", nil).Code != 403 {
		t.Fatal("до выдачи роли панель закрыта")
	}

	r := dir.do("PUT", teamMembersPath(plain.login)+"editor", nil)
	if r.Code != 200 {
		t.Fatalf("выдача: %d %s", r.Code, r.Body)
	}
	body := r.json()
	if body["changed"] != true {
		t.Errorf("changed = %v", body["changed"])
	}
	m := body["member"].(map[string]any)
	if m["login"] != plain.login || len(m["roles"].([]any)) != 1 {
		t.Errorf("участник: %v", m)
	}

	// та же сессия, без повторного входа
	after := sessionUser(plain.client)
	roles := after["roles"].([]any)
	if len(roles) != 1 || roles[0].(map[string]any)["id"] != "editor" || roles[0].(map[string]any)["name"] != "Редактор" {
		t.Errorf("роли в сессии: %v", roles)
	}
	caps := after["capabilities"].([]any)
	got := []string{}
	for _, c := range caps {
		got = append(got, c.(string))
	}
	if !slices.Contains(got, "publish") || !slices.Contains(got, "team_panel") || slices.Contains(got, "manage_team") {
		t.Errorf("права Редактора: %v", got)
	}
	if plain.client.do("GET", "/api/team/roles", nil).Code != 200 {
		t.Error("после выдачи роли панель должна открыться сразу")
	}

	// повтор идемпотентен
	if r := dir.do("PUT", teamMembersPath(plain.login)+"editor", nil); r.Code != 200 || r.json()["changed"] != false {
		t.Errorf("повторная выдача: %d %s", r.Code, r.Body)
	}

	// снятие тоже действует сразу
	if r := dir.do("DELETE", teamMembersPath(plain.login)+"editor", nil); r.Code != 200 || r.json()["changed"] != true {
		t.Fatalf("снятие: %d %s", r.Code, r.Body)
	}
	if plain.client.do("GET", "/api/team/roles", nil).Code != 403 {
		t.Error("после снятия роли панель должна закрыться сразу")
	}
	if r := dir.do("DELETE", teamMembersPath(plain.login)+"editor", nil); r.json()["changed"] != false {
		t.Errorf("повторное снятие: %s", r.Body)
	}
}

func TestLoginAndRegisterResponsesCarryRolesAndCapabilities(t *testing.T) {
	st := newStack(t, nil)
	c := st.newClient(t)
	r := c.register("Vladislav", teamPassword)
	u := r.json()["user"].(map[string]any)
	if roles, ok := u["roles"].([]any); !ok || len(roles) != 0 {
		t.Errorf("регистрация: roles = %v (нужен пустой список, не null)", u["roles"])
	}
	if caps, ok := u["capabilities"].([]any); !ok || len(caps) != 0 {
		t.Errorf("регистрация: capabilities = %v", u["capabilities"])
	}
	if _, _, err := st.svc.GrantRole(context.Background(), "Vladislav", accounts.RoleArchivist, nil); err != nil {
		t.Fatal(err)
	}
	login := st.newClient(t)
	r = login.do("POST", "/api/auth/login", map[string]any{"login": "Vladislav", "password": teamPassword})
	u = r.json()["user"].(map[string]any)
	if roles := u["roles"].([]any); len(roles) != 1 || roles[0].(map[string]any)["id"] != "archivist" {
		t.Errorf("вход: roles = %v", u["roles"])
	}
}

func TestDirectorateSessionListsEveryCapability(t *testing.T) {
	_, actors := teamStackWithActors(t)
	r := actors["director"].client.do("GET", "/api/auth/session", nil)
	caps := r.json()["user"].(map[string]any)["capabilities"].([]any)
	if len(caps) != len(accounts.AllCapabilities) {
		t.Errorf("права Директората: %v", caps)
	}
	// «Директорат» — не роль: список ролей у него пуст, права идут от флага
	if roles := r.json()["user"].(map[string]any)["roles"].([]any); len(roles) != 0 {
		t.Errorf("роли Директората: %v", roles)
	}
}

func TestGrantRoleRejectsUnknownRoleUserAndForeignOrigin(t *testing.T) {
	st, actors := teamStackWithActors(t)
	dir := actors["director"].client
	plain := actors["plain"].login

	for _, role := range []string{"admin", "Author", "directorate", "", "author%20"} {
		r := dir.do("PUT", teamMembersPath(plain)+role, nil)
		if r.Code != 404 {
			t.Errorf("роль %q: %d, ожидалось 404", role, r.Code)
		}
	}
	if r := dir.do("PUT", teamMembersPath("nobody")+"author", nil); r.Code != 404 || r.errCode() != "not_found" {
		t.Errorf("несуществующий пользователь: %d %s", r.Code, r.errCode())
	}
	// по одной роли у Автора, Редактора, Модератора и Архивариуса и все роли у all_roles
	if n := countRows(t, st, "user_roles"); n != int64(4+len(accounts.AllRoles)) {
		t.Errorf("неудачные запросы изменили роли: %d строк", n)
	}

	// CSRF: запрос с чужого сайта и запрос без Origin при живой сессии — отказ, роль не выдана
	evil := *dir
	evil.origin = "https://evil.example"
	if r := evil.do("PUT", teamMembersPath(plain)+"author", nil); r.Code != 403 || r.errCode() != "forbidden_origin" {
		t.Errorf("чужой Origin: %d %s", r.Code, r.errCode())
	}
	noOrigin := *dir
	noOrigin.origin = ""
	if r := noOrigin.do("PUT", teamMembersPath(plain)+"author", nil); r.Code != 403 {
		t.Errorf("без Origin: %d", r.Code)
	}
	if got := actors["plain"].client.do("GET", "/api/team/roles", nil).Code; got != 403 {
		t.Errorf("после отклонённых запросов роль всё же выдана: %d", got)
	}
}

func TestTeamMembersListing(t *testing.T) {
	_, actors := teamStackWithActors(t)
	dir := actors["director"].client

	r := dir.do("GET", "/api/team/members", nil)
	if r.Code != 200 {
		t.Fatalf("%d %s", r.Code, r.Body)
	}
	body := r.json()
	members := body["members"].([]any)
	if int(body["total"].(float64)) != len(members) || len(members) != 7 { // все, кроме гостя
		t.Fatalf("участников %d, total %v", len(members), body["total"])
	}
	// внутренние поля наружу не уходят
	for _, banned := range []string{"password", "hash", "backup", `"id"`, "granted_by"} {
		raw := strings.ReplaceAll(r.Body.String(), `"id":"`, "") // id роли — не идентификатор пользователя
		if strings.Contains(raw, banned) {
			t.Errorf("в списке команды есть %q: %s", banned, r.Body)
		}
	}

	staff := dir.do("GET", "/api/team/members?staff=1&q=all", nil).json()["members"].([]any)
	if len(staff) != 1 || staff[0].(map[string]any)["login"] != "user_all_roles" {
		t.Errorf("команда + поиск: %v", staff)
	}
	if got := dir.do("GET", "/api/team/members?role=editor", nil).json()["members"].([]any); len(got) != 2 { // editor и all_roles
		t.Errorf("фильтр по роли: %d участников", len(got))
	}
	p := dir.do("GET", "/api/team/members?per_page=3&page=2", nil).json()
	if len(p["members"].([]any)) != 3 || int(p["pages"].(float64)) != 3 || int(p["page"].(float64)) != 2 {
		t.Errorf("пагинация: %v", p)
	}
	// у Директората в списке — флаг
	var found bool
	for _, m := range dir.do("GET", "/api/team/members?q=director", nil).json()["members"].([]any) {
		found = m.(map[string]any)["directorate"] == true
	}
	if !found {
		t.Error("Директорат в списке не помечен")
	}
}

func TestTeamMembersRejectsBadParameters(t *testing.T) {
	_, actors := teamStackWithActors(t)
	dir := actors["director"].client
	for _, q := range []string{
		"role=admin", "staff=yes", "page=-1", "page=abc", "per_page=101", "per_page=x", "q=" + strings.Repeat("я", 25),
	} {
		r := dir.do("GET", "/api/team/members?"+q, nil)
		if r.Code != 400 || r.errCode() != "bad_request" || len(r.fields()) == 0 {
			t.Errorf("%s: %d %s %s", q, r.Code, r.errCode(), r.Body)
		}
	}
}

func TestTeamRolesEndpointDescribesRolesAndRights(t *testing.T) {
	_, actors := teamStackWithActors(t)
	r := actors["author"].client.do("GET", "/api/team/roles", nil)
	body := r.json()
	roles := body["roles"].([]any)
	if len(roles) != len(accounts.AllRoles) {
		t.Fatalf("ролей %d", len(roles))
	}
	byID := map[string][]string{}
	for _, ri := range roles {
		m := ri.(map[string]any)
		var caps []string
		for _, c := range m["capabilities"].([]any) {
			caps = append(caps, c.(map[string]any)["id"].(string))
		}
		byID[m["id"].(string)] = caps
		if m["name"] == "" {
			t.Errorf("у роли %v нет названия", m["id"])
		}
	}
	if !slices.Contains(byID["editor"], "publish") || slices.Contains(byID["author"], "publish") {
		t.Errorf("описание прав: %v", byID)
	}
	for id, caps := range byID {
		if slices.Contains(caps, "manage_team") {
			t.Errorf("роль %s описана как дающая выдачу ролей", id)
		}
	}
	if d := body["directorate"].(map[string]any); d["name"] != "Директорат" || len(d["capabilities"].([]any)) != len(accounts.AllCapabilities) {
		t.Errorf("описание Директората: %v", d)
	}
}

func TestTeamRoutesUseOnlyTheirMethods(t *testing.T) {
	_, actors := teamStackWithActors(t)
	dir := actors["director"].client
	for _, tc := range []struct{ method, path string }{
		{"POST", "/api/team/members"},
		{"PUT", "/api/team/members"},
		{"GET", teamMembersPath("user_plain") + "author"},
		{"POST", "/api/team/roles"},
	} {
		if r := dir.do(tc.method, tc.path, nil); r.Code != 405 && r.Code != 404 {
			t.Errorf("%s %s: %d", tc.method, tc.path, r.Code)
		}
	}
}
