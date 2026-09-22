package documents

import (
	"errors"
	"testing"

	"kupol/internal/xp"
)

func (e *env) publishedDoc(code string, level int) string {
	e.t.Helper()
	rep := e.imp(obj(code, "Тест", "published", level, ""))
	return rep.Items[0].Slug
}

func TestRemarkCreateListDelete(t *testing.T) {
	e := newEnv(t)
	slug := e.publishedDoc("О-101", 0)
	uid := e.user("reader1")
	v := Viewer{UserID: uid, UserLevel: 1}

	// Гражданин (без входа) не пишет
	if _, err := e.svc.CreateRemark(ctx, Guest, slug, RemarkInput{Text: "первая пометка"}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("гость: %v", err)
	}

	r, err := e.svc.CreateRemark(ctx, v, slug, RemarkInput{Text: "первая пометка"})
	if err != nil {
		t.Fatalf("создание: %v", err)
	}
	if r.Author != "reader1" || r.Text != "первая пометка" || !r.CanDelete {
		t.Fatalf("пометка: %+v", r)
	}

	uid2 := e.user("reader2")
	v2 := Viewer{UserID: uid2, UserLevel: 1}
	reply, err := e.svc.CreateRemark(ctx, v2, slug, RemarkInput{ParentID: &r.ID, Text: "ответ на пометку"})
	if err != nil {
		t.Fatalf("ответ: %v", err)
	}
	if reply.ParentID == nil || *reply.ParentID != r.ID {
		t.Fatalf("ParentID: %+v", reply.ParentID)
	}

	list, err := e.svc.ListRemarks(ctx, viewer(0), slug)
	if err != nil {
		t.Fatalf("список: %v", err)
	}
	if len(list) != 2 || list[0].ID != r.ID || list[1].ID != reply.ID {
		t.Fatalf("список: %+v", list)
	}
	if list[0].CanDelete {
		t.Error("гость не может удалять")
	}

	// чужую не удалить, свою — можно
	if err := e.svc.DeleteRemark(ctx, v2, r.ID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("чужая пометка: %v", err)
	}
	if err := e.svc.DeleteRemark(ctx, v, r.ID); err != nil {
		t.Fatalf("удаление своей: %v", err)
	}
	list, err = e.svc.ListRemarks(ctx, viewer(0), slug)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 0 {
		t.Fatalf("удаление родителя не унесло ответ каскадом: %+v", list)
	}
}

func TestRemarkVisibilityFollowsDocument(t *testing.T) {
	e := newEnv(t)
	slug := e.publishedDoc("О-102", 4) // видит только уровень 4+
	uid := e.user("low")

	if _, err := e.svc.ListRemarks(ctx, Viewer{UserID: uid, UserLevel: 1}, slug); !errors.Is(err, ErrNotFound) {
		t.Fatalf("низкий уровень: %v", err)
	}
	if _, err := e.svc.CreateRemark(ctx, Viewer{UserID: uid, UserLevel: 1}, slug, RemarkInput{Text: "x"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("низкий уровень create: %v", err)
	}
	if _, err := e.svc.ListRemarks(ctx, Viewer{UserID: uid, UserLevel: 4}, slug); err != nil {
		t.Fatalf("уровень 4: %v", err)
	}
}

func TestRemarkOnUnpublishedRejected(t *testing.T) {
	e := newEnv(t)
	rep := e.imp(obj("О-103", "Тест", "draft", 0, ""))
	slug := rep.Items[0].Slug
	uid := e.user("author1")
	v := Viewer{UserID: uid, UserLevel: 1}

	// обычный читатель черновик вообще не видит — как и остальной архив, "его нет"
	if _, err := e.svc.CreateRemark(ctx, v, slug, RemarkInput{Text: "рано"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("черновик, обычный читатель: %v", err)
	}
	// Директорат видит черновик, но комментировать его всё равно нельзя — публичного обсуждения у черновика нет
	dv := Viewer{UserID: uid, UserLevel: 1, Directorate: true}
	if _, err := e.svc.CreateRemark(ctx, dv, slug, RemarkInput{Text: "рано"}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("черновик, Директорат: %v", err)
	}
}

func TestRemarkValidationAndBannedWords(t *testing.T) {
	e := newEnv(t)
	slug := e.publishedDoc("О-104", 0)
	uid := e.user("writer")
	v := Viewer{UserID: uid, UserLevel: 1}

	if _, err := e.svc.CreateRemark(ctx, v, slug, RemarkInput{Text: "   "}); !hasPath(err, "text") {
		t.Fatalf("пустой текст: %v", err)
	}
	if _, err := e.svc.CreateRemark(ctx, v, slug, RemarkInput{Text: "он полный идиот"}); !hasPath(err, "text") {
		t.Fatalf("запрещённое слово не поймано: %v", err)
	}
}

func TestRemarkReplyToUnknownParentRejected(t *testing.T) {
	e := newEnv(t)
	slug := e.publishedDoc("О-105", 0)
	uid := e.user("writer2")
	v := Viewer{UserID: uid, UserLevel: 1}
	bogus := int64(999999)
	if _, err := e.svc.CreateRemark(ctx, v, slug, RemarkInput{ParentID: &bogus, Text: "ответ в никуда"}); !errors.Is(err, ErrRemarkNotFound) {
		t.Fatalf("несуществующий родитель: %v", err)
	}
}

func TestRemarkAwardsXPUpToDailyCap(t *testing.T) {
	e := newEnv(t)
	slug := e.publishedDoc("О-106", 0)
	uid := e.user("commenter")
	v := Viewer{UserID: uid, UserLevel: 1}

	var lastXP int
	for i := 0; i < xp.CommentDailyCap; i++ {
		if _, err := e.svc.CreateRemark(ctx, v, slug, RemarkInput{Text: "пометка"}); err != nil {
			t.Fatalf("пометка %d: %v", i, err)
		}
	}
	if err := e.db.Raw("SELECT xp FROM users WHERE id = ?", uid).Scan(&lastXP).Error; err != nil {
		t.Fatal(err)
	}
	if lastXP != xp.CommentXP*xp.CommentDailyCap {
		t.Fatalf("XP после %d пометок: %d, хотели %d", xp.CommentDailyCap, lastXP, xp.CommentXP*xp.CommentDailyCap)
	}

	// сверх лимита пометка публикуется, но XP не приносит
	if _, err := e.svc.CreateRemark(ctx, v, slug, RemarkInput{Text: "ещё одна пометка"}); err != nil {
		t.Fatalf("пометка сверх лимита: %v", err)
	}
	if err := e.db.Raw("SELECT xp FROM users WHERE id = ?", uid).Scan(&lastXP).Error; err != nil {
		t.Fatal(err)
	}
	if lastXP != xp.CommentXP*xp.CommentDailyCap {
		t.Fatalf("XP выросло сверх лимита: %d", lastXP)
	}
}

func TestRemarkReportAndModeration(t *testing.T) {
	e := newEnv(t)
	slug := e.publishedDoc("О-107", 0)
	author := e.user("author2")
	reporter1 := e.user("rep1")
	reporter2 := e.user("rep2")
	modUID := e.user("mod1")

	r, err := e.svc.CreateRemark(ctx, Viewer{UserID: author, UserLevel: 1}, slug, RemarkInput{Text: "спорная пометка"})
	if err != nil {
		t.Fatal(err)
	}

	plain := Viewer{UserID: modUID, UserLevel: 1}
	if _, err := e.svc.ReportedRemarks(ctx, plain); !errors.Is(err, ErrForbidden) {
		t.Fatalf("без права модерации: %v", err)
	}

	if err := e.svc.ReportRemark(ctx, Viewer{UserID: reporter1, UserLevel: 1}, r.ID); err != nil {
		t.Fatal(err)
	}
	// повторная жалоба от того же читателя не удваивает счётчик
	if err := e.svc.ReportRemark(ctx, Viewer{UserID: reporter1, UserLevel: 1}, r.ID); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.ReportRemark(ctx, Viewer{UserID: reporter2, UserLevel: 1}, r.ID); err != nil {
		t.Fatal(err)
	}

	mod := Viewer{UserID: modUID, UserLevel: 1, ModerateComments: true}
	reported, err := e.svc.ReportedRemarks(ctx, mod)
	if err != nil {
		t.Fatal(err)
	}
	if len(reported) != 1 || reported[0].ID != r.ID || reported[0].ReportCount != 2 {
		t.Fatalf("жалобы: %+v", reported)
	}
	if reported[0].DocumentSlug != slug {
		t.Fatalf("шифр документа не пришёл: %+v", reported[0])
	}

	// модератор удаляет чужую пометку
	if err := e.svc.DeleteRemark(ctx, mod, r.ID); err != nil {
		t.Fatalf("удаление модератором: %v", err)
	}
	list, err := e.svc.ListRemarks(ctx, viewer(0), slug)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 0 {
		t.Fatalf("пометка не удалена: %+v", list)
	}
}
