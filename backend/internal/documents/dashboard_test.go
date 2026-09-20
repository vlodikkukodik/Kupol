package documents

import (
	"fmt"
	"testing"
)

func ids(items []DashboardItem) []int64 {
	out := make([]int64, len(items))
	for i, it := range items {
		out[i] = it.ID
	}
	return out
}

func equalIDs(a, b []int64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func (e *env) dashboard(a Actor) *Dashboard {
	e.t.Helper()
	d, err := e.svc.TeamDashboard(ctx, a)
	if err != nil {
		e.t.Fatalf("TeamDashboard: %v", err)
	}
	return d
}

func TestDashboardAuthorSeesOwnWorkByState(t *testing.T) {
	e := newEnv(t)
	a := e.actors()

	plain := e.objectDraft(a.owner, "Обычный черновик")
	returned := e.submit(a.owner, e.objectDraft(a.owner, "Возвращённый"))
	if _, err := e.svc.AddComment(ctx, a.editor, returned.ID, nil, "Первое замечание"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.AddComment(ctx, a.editor, returned.ID, nil, "Второе замечание"); err != nil {
		t.Fatal(err)
	}
	e.decide(a.editor, returned, VerdictReturn, "Уточните место обнаружения")
	waiting := e.submit(a.owner, e.objectDraft(a.owner, "Ждёт проверки"))
	published := e.decide(a.editor, e.submit(a.owner, e.objectDraft(a.owner, "Опубликованный")), VerdictApprove, "")
	_ = published
	e.objectDraft(a.other, "Чужой черновик") // не должен попасть на стол владельца

	d := e.dashboard(a.owner)
	if d.Counts != (DashboardCounts{Draft: 2, Review: 1, Published: 1}) {
		t.Errorf("счётчики: %+v", d.Counts)
	}
	if !equalIDs(ids(d.Drafts), []int64{plain.ID}) || !equalIDs(ids(d.InReview), []int64{waiting.ID}) || !equalIDs(ids(d.Returned), []int64{returned.ID}) {
		t.Errorf("списки: черновики %v, на проверке %v, возвращённые %v", ids(d.Drafts), ids(d.InReview), ids(d.Returned))
	}
	r := d.Returned[0]
	if r.ReturnNote != "Уточните место обнаружения" || r.ReturnedBy == nil || *r.ReturnedBy != "editor" || r.OpenComments != 2 {
		t.Errorf("возвращённый: %+v", r)
	}
	if d.InReview[0].SubmittedAt == nil || d.Drafts[0].SubmittedAt != nil || d.Drafts[0].ReturnNote != "" {
		t.Errorf("метки времени и причины у обычных документов: %+v %+v", d.InReview[0], d.Drafts[0])
	}
	if d.CanReview || len(d.Queue) != 0 || d.QueueTotal != 0 || !d.CanWrite {
		t.Errorf("автору без права проверять очередь не положена: %+v", d)
	}

	// автор исправил замечания и отправил снова: «возвращённым» документ больше не считается
	cur, _ := e.svc.TeamGet(ctx, a.owner, returned.ID)
	e.submit(a.owner, cur)
	d = e.dashboard(a.owner)
	if len(d.Returned) != 0 || len(d.InReview) != 2 || d.Counts.Review != 2 || d.Counts.Draft != 1 {
		t.Errorf("после повторной отправки: возвращённые %d, на проверке %d, счётчики %+v", len(d.Returned), len(d.InReview), d.Counts)
	}
}

func TestDashboardQueueIsForReviewersAndOldestFirst(t *testing.T) {
	e := newEnv(t)
	a := e.actors()

	first := e.submit(a.owner, e.objectDraft(a.owner, "Первым отправлен"))
	e.advance(60_000_000_000) // минута
	second := e.submit(a.other, e.objectDraft(a.other, "Вторым отправлен"))
	e.advance(60_000_000_000)
	mine := e.submit(a.editor, e.objectDraft(a.editor, "Документ самого Редактора"))
	e.objectDraft(a.owner, "Черновик в очередь не попадает")

	d := e.dashboard(a.editor)
	if !d.CanReview || d.QueueTotal != 2 || !equalIDs(ids(d.Queue), []int64{first.ID, second.ID}) {
		t.Fatalf("очередь Редактора: всего %d, %v", d.QueueTotal, ids(d.Queue))
	}
	if d.Queue[0].Author == nil || *d.Queue[0].Author != "owner" || d.Queue[0].SubmittedAt == nil || !d.Queue[0].SubmittedAt.Before(*d.Queue[1].SubmittedAt) {
		t.Errorf("элемент очереди: %+v", d.Queue[0])
	}
	// собственный документ Редактор не проверяет: он в «на проверке», но не в очереди
	if !equalIDs(ids(d.InReview), []int64{mine.ID}) {
		t.Errorf("свой документ Редактора: %v", ids(d.InReview))
	}

	// Директорат проверяет всё, в том числе своё
	dm := e.submit(a.director, e.objectDraft(a.director, "Документ Директората"))
	dd := e.dashboard(a.director)
	if dd.QueueTotal != 4 || dd.Queue[len(dd.Queue)-1].ID != dm.ID {
		t.Errorf("очередь Директората: всего %d, %v", dd.QueueTotal, ids(dd.Queue))
	}

	// автор, модератор и посторонний очереди не видят вовсе
	for name, who := range map[string]Actor{"автор": a.owner, "модератор": a.moderator, "посторонний": a.plain} {
		if got := e.dashboard(who); got.CanReview || len(got.Queue) != 0 || got.QueueTotal != 0 {
			t.Errorf("%s видит очередь: %+v", name, got)
		}
	}
}

func TestDashboardListsAreLimitedButCountsAreNot(t *testing.T) {
	e := newEnv(t)
	a := e.actors()
	for i := 0; i < dashboardListLimit+4; i++ {
		e.objectDraft(a.owner, fmt.Sprintf("Черновик %d", i))
	}
	d := e.dashboard(a.owner)
	if len(d.Drafts) != dashboardListLimit || d.Counts.Draft != dashboardListLimit+4 {
		t.Errorf("показано %d, всего %d", len(d.Drafts), d.Counts.Draft)
	}
	if d.Drafts[0].Title != fmt.Sprintf("Черновик %d", dashboardListLimit+3) {
		t.Errorf("новые правки — сверху: %q", d.Drafts[0].Title)
	}
	// у человека без документов рабочий стол пуст, но не nil: интерфейс не должен ловить null
	empty := e.dashboard(a.plain)
	if empty.Drafts == nil || empty.InReview == nil || empty.Returned == nil || empty.Queue == nil || (empty.Counts != DashboardCounts{}) {
		t.Errorf("пустой рабочий стол: %+v", empty)
	}
}
