package suggestions_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"gorm.io/gorm"

	"kupol/internal/suggestions"
	"kupol/internal/testutil"
	"kupol/internal/xp"
)

// newUser заводит пользователя тем же способом, что и админ-команды: только обязательные колонки.
func newUser(t *testing.T, db *gorm.DB, login string) int64 {
	t.Helper()
	now := time.Now().UTC()
	var id int64
	if err := db.Raw(`INSERT INTO users (login, password_hash, backup_code_hash, created_at, password_changed_at)
		VALUES (?, 'x', 'y', ?, ?) RETURNING id`, login, now, now).Scan(&id).Error; err != nil {
		t.Fatal(err)
	}
	return id
}

func newService(t *testing.T) (*suggestions.Service, *gorm.DB) {
	t.Helper()
	db := testutil.NewMigratedDB(t)
	return suggestions.NewService(db, testutil.Logger(), nil), db
}

func userXP(t *testing.T, db *gorm.DB, id int64) (xpTotal, level int) {
	t.Helper()
	var row struct {
		XP    int
		Level int
	}
	if err := db.Raw("SELECT xp, level FROM users WHERE id = ?", id).Scan(&row).Error; err != nil {
		t.Fatal(err)
	}
	return row.XP, row.Level
}

func TestCreateValidatesAndListsMine(t *testing.T) {
	svc, db := newService(t)
	ctx := context.Background()
	author := newUser(t, db, "author1")
	other := newUser(t, db, "other1")

	// Гражданин (нет пользователя) не может писать
	if _, err := svc.Create(ctx, 0, "идея"); !errors.Is(err, suggestions.ErrForbidden) {
		t.Fatalf("без входа: %v, ожидалась ErrForbidden", err)
	}
	// пустое и слишком длинное — ошибки по полю text
	for _, bad := range []string{"", "   ", string(make([]rune, 2001))} {
		_, err := svc.Create(ctx, author, bad)
		var ve *suggestions.ValidationError
		if !errors.As(err, &ve) || ve.Fields["text"] == "" {
			t.Fatalf("текст %q: %v, ожидалась ошибка поля text", truncate(bad), err)
		}
	}

	first, err := svc.Create(ctx, author, "  стоит сделать тёмную тему  ")
	if err != nil {
		t.Fatal(err)
	}
	if first.Status != suggestions.StatusReceived || first.Author != "author1" || first.Comment != "" {
		t.Fatalf("новое предложение: %+v", first)
	}
	if first.Text != "стоит сделать тёмную тему" {
		t.Errorf("текст не обрезан пробелами: %q", first.Text)
	}
	if _, err := svc.Create(ctx, author, "второе замечание"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Create(ctx, other, "чужое предложение"); err != nil {
		t.Fatal(err)
	}

	mine, err := svc.Mine(ctx, author)
	if err != nil {
		t.Fatal(err)
	}
	if len(mine) != 2 {
		t.Fatalf("мои предложения: %d, ожидалось 2", len(mine))
	}
	if mine[0].Text != "второе замечание" { // новые сверху
		t.Errorf("порядок: %q", mine[0].Text)
	}
	if _, err := svc.Mine(ctx, 0); !errors.Is(err, suggestions.ErrForbidden) {
		t.Errorf("список без входа: %v", err)
	}
}

func truncate(s string) string {
	if len(s) > 30 {
		return s[:30] + "…"
	}
	return s
}

func TestStatusFlow(t *testing.T) {
	svc, db := newService(t)
	ctx := context.Background()
	author := newUser(t, db, "author2")
	editor := newUser(t, db, "editor2")

	s, err := svc.Create(ctx, author, "предложение о каталоге")
	if err != nil {
		t.Fatal(err)
	}

	// несуществующее и мусор в адресе
	if _, err := svc.SetStatus(ctx, editor, 999999, suggestions.StatusReviewed, ""); !errors.Is(err, suggestions.ErrNotFound) {
		t.Fatalf("нет такого: %v", err)
	}
	// неизвестный статус и слишком длинное пояснение — ошибки полей
	for _, st := range []suggestions.Status{"approved", "новый"} {
		_, err := svc.SetStatus(ctx, editor, s.ID, st, "")
		var ve *suggestions.ValidationError
		if !errors.As(err, &ve) || ve.Fields["status"] == "" {
			t.Fatalf("статус %q: %v, ожидалась ошибка поля status", st, err)
		}
	}
	if _, err := svc.SetStatus(ctx, editor, s.ID, suggestions.StatusReviewed, string(make([]rune, 2001))); func() bool {
		var ve *suggestions.ValidationError
		return !errors.As(err, &ve) || ve.Fields["comment"] == ""
	}() {
		t.Fatalf("длинное пояснение: %v", err)
	}
	// своё предложение Редактор не разбирает сам
	if _, err := svc.SetStatus(ctx, author, s.ID, suggestions.StatusReviewed, ""); !errors.Is(err, suggestions.ErrSelfReview) {
		t.Fatalf("своё предложение: %v, ожидался ErrSelfReview", err)
	}

	// получено → рассмотрено → принято
	reviewed, err := svc.SetStatus(ctx, editor, s.ID, suggestions.StatusReviewed, "Смотрю")
	if err != nil {
		t.Fatal(err)
	}
	if reviewed.Status != suggestions.StatusReviewed || reviewed.HandledBy != "editor2" || reviewed.HandledAt == nil {
		t.Fatalf("рассмотрено: %+v", reviewed)
	}
	accepted, err := svc.SetStatus(ctx, editor, s.ID, suggestions.StatusAccepted, "Принято, спасибо")
	if err != nil {
		t.Fatal(err)
	}
	if accepted.Status != suggestions.StatusAccepted || accepted.Comment != "Принято, спасибо" {
		t.Fatalf("принято: %+v", accepted)
	}

	// пустое пояснение не затирает уже написанное
	s0, err := svc.Create(ctx, author, "предложение с пояснением")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetStatus(ctx, editor, s0.ID, suggestions.StatusReviewed, "Заметил"); err != nil {
		t.Fatal(err)
	}
	kept, err := svc.SetStatus(ctx, editor, s0.ID, suggestions.StatusAccepted, "   ")
	if err != nil {
		t.Fatal(err)
	}
	if kept.Comment != "Заметил" {
		t.Errorf("пустое пояснение затёрло прежнее: %q", kept.Comment)
	}

	// окончательные статусы необратимы (иначе XP можно было бы получить повторно)
	s2, err := svc.Create(ctx, author, "второе предложение")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetStatus(ctx, editor, s2.ID, suggestions.StatusRejected, "Не подходит"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetStatus(ctx, editor, s2.ID, suggestions.StatusAccepted, ""); !errors.Is(err, suggestions.ErrInvalidState) {
		t.Fatalf("отклонено → принято: %v, ожидался ErrInvalidState", err)
	}
	if _, err := svc.SetStatus(ctx, editor, s2.ID, suggestions.StatusReceived, ""); !errors.Is(err, suggestions.ErrInvalidState) {
		t.Fatalf("возврат в «получено»: %v, ожидался ErrInvalidState", err)
	}
}

func TestAcceptAwardsXPWithDailyCap(t *testing.T) {
	svc, db := newService(t)
	ctx := context.Background()
	author := newUser(t, db, "author3")
	editor := newUser(t, db, "editor3")

	before, level := userXP(t, db, author)

	// принять можно и сразу из «получено» (решение не ждёт промежуточного статуса)
	var ids []int64
	for i := 0; i < xp.SuggestionDailyCap+1; i++ {
		s, err := svc.Create(ctx, author, "предложение номер пять")
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, s.ID)
	}
	awarded := 0
	for _, id := range ids {
		out, err := svc.SetStatus(ctx, editor, id, suggestions.StatusAccepted, "принято")
		if err != nil {
			t.Fatal(err)
		}
		if out.Status != suggestions.StatusAccepted {
			t.Fatalf("статус: %s", out.Status)
		}
	}
	// дневной лимит: xp.SuggestionDailyCap принятий в день приносят XP, остальные — нет
	got, newLevel := userXP(t, db, author)
	awarded = xp.SuggestionDailyCap * xp.SuggestionXP
	if got-before != awarded {
		t.Fatalf("XP: +%d, ожидалось +%d (лимит %d/день)", got-before, awarded, xp.SuggestionDailyCap)
	}
	// принятие поднимает уровень 1→2 (порог 100 XP набран с запасом)
	if level == 1 && newLevel != 2 {
		t.Errorf("уровень после %d XP: %d, ожидался 2", got-before, newLevel)
	}
}

func TestQueueFilterPagingAndOrder(t *testing.T) {
	svc, db := newService(t)
	ctx := context.Background()
	author := newUser(t, db, "author4")
	editor := newUser(t, db, "editor4")

	// три «получено» и одно «принято»: в очереди сначала нерассмотренные
	var received []int64
	for i := 0; i < 3; i++ {
		s, err := svc.Create(ctx, author, "предложение для очереди")
		if err != nil {
			t.Fatal(err)
		}
		received = append(received, s.ID)
	}
	done, err := svc.Create(ctx, author, "решённое предложение")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetStatus(ctx, editor, done.ID, suggestions.StatusAccepted, "ок"); err != nil {
		t.Fatal(err)
	}

	all, err := svc.Queue(ctx, suggestions.Query{})
	if err != nil {
		t.Fatal(err)
	}
	if all.Total != 4 || all.Pages != 1 || all.PerPage != 50 {
		t.Fatalf("очередь: %+v", all)
	}
	if all.Items[0].Status != suggestions.StatusReceived || all.Items[3].Status != suggestions.StatusAccepted {
		t.Fatalf("порядок: сначала нерассмотренные, получено %+v", all.Items[0])
	}
	if all.Items[0].Author != "author4" {
		t.Errorf("автор в очереди: %q", all.Items[0].Author)
	}

	onlyAccepted, err := svc.Queue(ctx, suggestions.Query{Status: suggestions.StatusAccepted})
	if err != nil {
		t.Fatal(err)
	}
	if onlyAccepted.Total != 1 || onlyAccepted.Items[0].ID != done.ID {
		t.Fatalf("фильтр accepted: %+v", onlyAccepted)
	}

	// страницы
	page1, err := svc.Queue(ctx, suggestions.Query{PerPage: 2, Page: 1})
	if err != nil {
		t.Fatal(err)
	}
	page2, err := svc.Queue(ctx, suggestions.Query{PerPage: 2, Page: 2})
	if err != nil {
		t.Fatal(err)
	}
	if page1.Total != 4 || page1.Pages != 2 || len(page1.Items) != 2 || len(page2.Items) != 2 {
		t.Fatalf("пагинация: страница 1 %+v, страница 2 %+v", page1, page2)
	}
	if page1.Items[0].ID == page2.Items[0].ID {
		t.Error("страницы совпадают")
	}

	// неверные параметры — ошибки полей
	for _, bad := range []suggestions.Query{{Status: "wrong"}, {Page: -1}, {PerPage: 101}, {PerPage: -3}} {
		var ve *suggestions.ValidationError
		if _, err := svc.Queue(ctx, bad); !errors.As(err, &ve) {
			t.Fatalf("параметры %+v: %v, ожидалась ValidationError", bad, err)
		}
	}
}
