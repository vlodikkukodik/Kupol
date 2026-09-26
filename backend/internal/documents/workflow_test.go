package documents

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"kupol/internal/audit"
)

// ---------------------------------------------------------------- помощники

// objectDraft — черновик Объекта без шифра: номер О-№ ему присвоят при публикации.
func (e *env) objectDraft(a Actor, title string, blocks ...InputBlock) *TeamDocument {
	e.t.Helper()
	if blocks == nil {
		blocks = []InputBlock{paragraph("b1", "Описание объекта.")}
	}
	d, err := e.svc.TeamCreate(ctx, a, CreateInput{Type: "object", Content: memoContent(title, blocks...)})
	if err != nil {
		e.t.Fatalf("TeamCreate(object): %v", err)
	}
	return d
}

func (e *env) submit(a Actor, d *TeamDocument) *TeamDocument {
	e.t.Helper()
	out, err := e.svc.Submit(ctx, a, d.ID, d.Revision)
	if err != nil {
		e.t.Fatalf("Submit: %v", err)
	}
	return out
}

func (e *env) decide(a Actor, d *TeamDocument, v Verdict, comment string) *TeamDocument {
	e.t.Helper()
	out, err := e.svc.Decide(ctx, a, d.ID, v, comment, d.Revision)
	if err != nil {
		e.t.Fatalf("Decide(%s): %v", v, err)
	}
	return out
}

func wantErr[T error](t *testing.T, err error, what string) T {
	t.Helper()
	var target T
	if !errors.As(err, &target) {
		t.Fatalf("%s: ожидалась ошибка %T, получено %v", what, target, err)
	}
	return target
}

func linkBlock(id, code string) InputBlock {
	return rawBlock(id, "doc_link", fmt.Sprintf(`{"code":%q}`, code))
}

// ---------------------------------------------------------------- права

func TestWorkflowPermissionMatrix(t *testing.T) {
	e := newEnv(t)
	a := e.actors()
	mine := func(u Actor) *int64 { id := u.UserID; return &id }

	type row struct {
		who    string
		actor  Actor
		status Status
		owned  bool
		want   Workflow
	}
	rows := []row{
		// Автор: отправляет и забирает своё
		{"владелец", a.owner, StatusDraft, true, Workflow{Submit: true}},
		{"владелец", a.owner, StatusReview, true, Workflow{Withdraw: true}},
		{"владелец", a.owner, StatusPublished, true, Workflow{}},
		{"владелец", a.owner, StatusArchived, true, Workflow{}},
		// Редактор: чужое на проверке — вердикт и комментарии; опубликованное — архив; чужой черновик не видит
		{"редактор", a.editor, StatusReview, false, Workflow{Review: true, Comment: true}},
		{"редактор", a.editor, StatusPublished, false, Workflow{Archive: true}},
		{"редактор", a.editor, StatusArchived, false, Workflow{Unarchive: true}},
		// свой документ Редактор не проверяет, но отправляет как Автор
		{"редактор-владелец", a.editor, StatusDraft, true, Workflow{Submit: true}},
		{"редактор-владелец", a.editor, StatusReview, true, Workflow{Withdraw: true}},
		// Директорат: всё, включая проверку своего
		{"Директорат", a.director, StatusDraft, false, Workflow{Submit: true}},
		{"Директорат", a.director, StatusReview, false, Workflow{Withdraw: true, Review: true, Comment: true}},
		{"Директорат", a.director, StatusReview, true, Workflow{Withdraw: true, Review: true, Comment: true}},
		{"Директорат", a.director, StatusPublished, false, Workflow{Archive: true}},
		{"Директорат", a.director, StatusArchived, false, Workflow{Unarchive: true}},
		// чужой Автор, модератор и посторонний ничего не могут
		{"чужой автор", a.other, StatusReview, false, Workflow{}},
		{"модератор", a.moderator, StatusReview, false, Workflow{}},
		{"посторонний", a.plain, StatusPublished, false, Workflow{}},
	}
	for _, r := range rows {
		d := &Document{Status: string(r.status)}
		if r.owned {
			d.AuthorID = mine(r.actor)
		}
		if got := r.actor.Workflow(d); got != r.want {
			t.Errorf("%s, %s, свой=%v: права %+v, ожидалось %+v", r.who, r.status, r.owned, got, r.want)
		}
	}
}

// ---------------------------------------------------------------- путь документа

func TestSubmitReviewPublishHappyPath(t *testing.T) {
	e := newEnv(t)
	a := e.actors()
	d := e.objectDraft(a.owner, "Объект для публикации")
	if d.Code != nil {
		t.Fatalf("у черновика объекта не должно быть шифра, есть %v", *d.Code)
	}
	if !d.Workflow.Submit || d.Workflow.Review {
		t.Errorf("права автора на черновик: %+v", d.Workflow)
	}

	// перед отправкой автор держал документ в работе — после отправки замок снят
	if _, err := e.svc.TakeLock(ctx, a.owner, d.ID); err != nil {
		t.Fatal(err)
	}
	sent := e.submit(a.owner, d)
	if sent.Status != "review" || sent.Lock != nil {
		t.Fatalf("после отправки: статус %s, замок %+v", sent.Status, sent.Lock)
	}
	if !sent.Workflow.Withdraw || sent.Workflow.Submit {
		t.Errorf("права автора на документе на проверке: %+v", sent.Workflow)
	}

	pub := e.decide(a.editor, sent, VerdictApprove, "")
	if pub.Status != "published" || pub.Code == nil || !strings.HasPrefix(*pub.Code, "О-") || pub.PublishedAt == nil {
		t.Fatalf("после принятия: %s, шифр %v, дата %v", pub.Status, pub.Code, pub.PublishedAt)
	}
	// читатель видит опубликованное сразу и по присвоенному шифру
	got, err := e.svc.Get(ctx, Guest, *pub.Code)
	if err != nil || got.Title != "Объект для публикации" {
		t.Fatalf("читатель не открыл опубликованный документ: %v %+v", err, got)
	}

	// ход рецензии и снимки: отправка и принятие — постоянные записи, с автором
	review, err := e.svc.TeamReview(ctx, a.owner, d.ID)
	if err != nil {
		t.Fatal(err)
	}
	kinds := []string{}
	for _, ev := range review.Events {
		kinds = append(kinds, ev.Kind)
	}
	if strings.Join(kinds, ",") != "submit,approve" {
		t.Errorf("ход рецензии: %v", kinds)
	}
	if review.Events[1].Actor == nil || *review.Events[1].Actor != "editor" {
		t.Errorf("не записан, кто принял: %+v", review.Events[1])
	}
	statusSnaps := 0
	for _, v := range e.versions(d.ID) {
		if v.Kind == string(VersionStatus) {
			statusSnaps++
			if !v.Permanent || v.AuthorID == nil {
				t.Errorf("снимок смены статуса должен храниться всегда и с автором: %+v", v)
			}
		}
	}
	if statusSnaps != 2 {
		t.Errorf("снимков смены статуса %d, ожидалось 2", statusSnaps)
	}
	for _, action := range []audit.Action{audit.DocumentSubmitted, audit.DocumentPublished} {
		if rows := e.auditRows(action); len(rows) != 1 || rows[0].DocumentID == nil || *rows[0].DocumentID != d.ID {
			t.Errorf("журнал %s: %+v", action, rows)
		}
	}
}

func TestReviewerCannotReviewOwnDocumentButDirectorateCan(t *testing.T) {
	e := newEnv(t)
	a := e.actors()
	d := e.submit(a.editor, e.objectDraft(a.editor, "Свой документ Редактора"))

	if _, err := e.svc.Decide(ctx, a.editor, d.ID, VerdictApprove, "", d.Revision); !errors.Is(err, ErrSelfReview) {
		t.Fatalf("Редактор принял собственный документ: %v", err)
	}
	if _, err := e.svc.AddComment(ctx, a.editor, d.ID, nil, "Сам себе"); !errors.Is(err, ErrSelfReview) {
		t.Errorf("Редактор прокомментировал собственный документ: %v", err)
	}
	if got := e.decide(a.director, d, VerdictApprove, ""); got.Status != "published" {
		t.Errorf("Директорат не опубликовал: %s", got.Status)
	}
	if st := e.mustGetStatus(d.ID); st != "published" {
		t.Errorf("статус в БД %s", st)
	}
}

func (e *env) mustGetStatus(id int64) string {
	e.t.Helper()
	var st string
	if err := e.db.Raw("SELECT status FROM documents WHERE id = ?", id).Scan(&st).Error; err != nil {
		e.t.Fatal(err)
	}
	return st
}

func TestReturnAndRejectNeedAReason(t *testing.T) {
	e := newEnv(t)
	a := e.actors()

	d := e.submit(a.owner, e.objectDraft(a.owner, "Вернуть"))
	for _, blank := range []string{"", "   ", "\n\t"} {
		ve := wantErr[*ValidationError](t, func() error {
			_, err := e.svc.Decide(ctx, a.editor, d.ID, VerdictReturn, blank, d.Revision)
			return err
		}(), "возврат без причины")
		if ve.Problems[0].Path != "comment" {
			t.Errorf("замечание не к полю comment: %+v", ve.Problems)
		}
	}
	if st := e.mustGetStatus(d.ID); st != "review" {
		t.Fatalf("возврат без причины изменил статус: %s", st)
	}
	back := e.decide(a.editor, d, VerdictReturn, "  Уточните место обнаружения  ")
	if back.Status != "draft" {
		t.Fatalf("после возврата: %s", back.Status)
	}
	// возвращённое — снова личный черновик автора: он правит, Редактор чужого черновика уже не видит
	if own, err := e.svc.TeamGet(ctx, a.owner, d.ID); err != nil || !own.CanEdit || !own.Workflow.Submit {
		t.Fatalf("автор после возврата: %v %+v", err, own)
	}
	if _, err := e.svc.TeamGet(ctx, a.editor, d.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("Редактор видит чужой черновик после возврата: %v", err)
	}
	review, _ := e.svc.TeamReview(ctx, a.owner, d.ID)
	last := review.Events[len(review.Events)-1]
	if last.Kind != "return" || last.Comment != "Уточните место обнаружения" {
		t.Errorf("причина возврата не сохранена и не обрезана: %+v", last)
	}

	// автор исправляет и отправляет снова; второй заход — отклонение
	fixed := mustSave(t, e, a.owner, back, memoContent("Вернуть", paragraph("b1", "Место: подвал.")))
	again := e.submit(a.owner, fixed.Document)
	if _, err := e.svc.Decide(ctx, a.editor, again.ID, VerdictReject, "", again.Revision); err == nil {
		t.Error("отклонение без причины прошло")
	}
	rej := e.decide(a.editor, again, VerdictReject, "Дублирует О-12")
	if rej.Status != "archived" {
		t.Errorf("отклонённый документ должен уйти в архив, статус %s", rej.Status)
	}
	if rows := e.auditRows(audit.DocumentReturned); len(rows) != 1 {
		t.Errorf("журнал возврата: %+v", rows)
	}
	if rows := e.auditRows(audit.DocumentRejected); len(rows) != 1 {
		t.Errorf("журнал отклонения: %+v", rows)
	}

	// слишком длинная причина
	long := e.submit(a.owner, e.objectDraft(a.owner, "Длинно"))
	ve := wantErr[*ValidationError](t, func() error {
		_, err := e.svc.Decide(ctx, a.editor, long.ID, VerdictReturn, strings.Repeat("я", MaxReviewText+1), long.Revision)
		return err
	}(), "слишком длинная причина")
	if ve.Problems[0].Path != "comment" {
		t.Errorf("%+v", ve.Problems)
	}
}

func TestWithdrawOnlyByOwner(t *testing.T) {
	e := newEnv(t)
	a := e.actors()
	d := e.submit(a.owner, e.objectDraft(a.owner, "Забрать"))

	for name, who := range map[string]Actor{"чужой автор": a.other, "модератор": a.moderator} {
		if _, err := e.svc.Withdraw(ctx, who, d.ID, ""); !errors.Is(err, ErrNotFound) {
			t.Errorf("%s: %v", name, err)
		}
	}
	// Редактор документ видит, но забрать его у автора не может: у него для этого вердикт «вернуть»
	if _, err := e.svc.Withdraw(ctx, a.editor, d.ID, ""); !errors.Is(err, ErrForbidden) {
		t.Errorf("Редактор забрал чужой документ: %v", err)
	}
	back, err := e.svc.Withdraw(ctx, a.owner, d.ID, "Ещё не готово")
	if err != nil || back.Status != "draft" {
		t.Fatalf("автор не забрал документ: %v %+v", err, back)
	}
	if _, err := e.svc.Withdraw(ctx, a.owner, d.ID, ""); err == nil {
		t.Error("забрать документ, который уже черновик, — не ошибка?")
	} else {
		wantErr[*StateError](t, err, "повторный отзыв")
	}
}

func TestTransitionsRespectStatus(t *testing.T) {
	e := newEnv(t)
	a := e.actors()
	draft := e.objectDraft(a.owner, "Черновик")

	// вердикт по черновику, архив черновика, второй Submit — не тот статус
	_, err := e.svc.Decide(ctx, a.director, draft.ID, VerdictApprove, "", draft.Revision)
	wantErr[*StateError](t, err, "вердикт по черновику")
	_, err = e.svc.Archive(ctx, a.director, draft.ID, "")
	wantErr[*StateError](t, err, "архив черновика")
	sent := e.submit(a.owner, draft)
	_, err = e.svc.Submit(ctx, a.owner, sent.ID, sent.Revision)
	wantErr[*StateError](t, err, "повторная отправка")
	// неизвестный вердикт
	_, err = e.svc.Decide(ctx, a.editor, sent.ID, Verdict("maybe"), "", sent.Revision)
	wantErr[*ValidationError](t, err, "неизвестный вердикт")
	// чужой Автор и посторонний ничего не знают о документе на проверке
	if _, err := e.svc.Decide(ctx, a.other, sent.ID, VerdictApprove, "", sent.Revision); !errors.Is(err, ErrNotFound) {
		t.Errorf("чужой Автор вынес вердикт: %v", err)
	}
	if _, err := e.svc.Decide(ctx, a.plain, sent.ID, VerdictApprove, "", sent.Revision); !errors.Is(err, ErrNotFound) {
		t.Errorf("посторонний вынес вердикт: %v", err)
	}
	// сам Автор вердикт не выносит
	if _, err := e.svc.Decide(ctx, a.owner, sent.ID, VerdictApprove, "", sent.Revision); !errors.Is(err, ErrForbidden) {
		t.Errorf("Автор принял собственный документ: %v", err)
	}
}

func TestArchiveAndUnarchive(t *testing.T) {
	e := newEnv(t)
	a := e.actors()
	pub := e.decide(a.editor, e.submit(a.owner, e.objectDraft(a.owner, "В архив")), VerdictApprove, "")

	if _, err := e.svc.Archive(ctx, a.owner, pub.ID, ""); !errors.Is(err, ErrForbidden) {
		t.Errorf("Автор убрал опубликованное в архив: %v", err)
	}
	arch, err := e.svc.Archive(ctx, a.editor, pub.ID, "Устарело")
	if err != nil || arch.Status != "archived" {
		t.Fatalf("архив: %v %+v", err, arch)
	}
	if _, err := e.svc.Get(ctx, Guest, *pub.Code); !errors.Is(err, ErrNotFound) {
		t.Errorf("архивный документ виден читателю: %v", err)
	}
	back, err := e.svc.Unarchive(ctx, a.editor, pub.ID, "")
	if err != nil || back.Status != "published" {
		t.Fatalf("возврат из архива: %v %+v", err, back)
	}
	if back.PublishedAt == nil || !back.PublishedAt.Equal(*pub.PublishedAt) {
		t.Errorf("дата первой публикации изменилась: %v → %v", pub.PublishedAt, back.PublishedAt)
	}
	if _, err := e.svc.Get(ctx, Guest, *pub.Code); err != nil {
		t.Errorf("документ после возврата из архива не виден: %v", err)
	}
}

// ---------------------------------------------------------------- редакции и замки

func TestSubmitAndDecideAreBoundToTheRevisionSeen(t *testing.T) {
	e := newEnv(t)
	a := e.actors()
	d := e.objectDraft(a.owner, "Редакции")

	// автор сохранил новую редакцию в другой вкладке: отправлять то, чего он не видел, нельзя
	mustSave(t, e, a.owner, d, memoContent("Редакции", paragraph("b1", "Новее.")))
	ce := wantErr[*ConflictError](t, func() error { _, err := e.svc.Submit(ctx, a.owner, d.ID, d.Revision); return err }(), "отправка устаревшей редакции")
	if ce.CurrentRevision != d.Revision+1 {
		t.Errorf("текущая редакция в ошибке: %d", ce.CurrentRevision)
	}

	cur, _ := e.svc.TeamGet(ctx, a.owner, d.ID)
	sent := e.submit(a.owner, cur)
	// пока рецензент читал, автор поправил текст: вердикт по прочитанному не выносится
	mustSave(t, e, a.owner, sent, memoContent("Редакции", paragraph("b1", "Ещё правка.")))
	_, err := e.svc.Decide(ctx, a.editor, sent.ID, VerdictApprove, "", sent.Revision)
	wantErr[*ConflictError](t, err, "вердикт по устаревшей редакции")
	if st := e.mustGetStatus(d.ID); st != "review" {
		t.Errorf("конфликт изменил статус: %s", st)
	}
}

func TestTransitionsRespectForeignLocksAndReleaseOwn(t *testing.T) {
	e := newEnv(t)
	a := e.actors()
	sent := e.submit(a.owner, e.objectDraft(a.owner, "Замки"))

	// автор снова правит документ на проверке: рецензент не может вынести вердикт «из-под рук»
	if _, err := e.svc.TakeLock(ctx, a.owner, sent.ID); err != nil {
		t.Fatal(err)
	}
	le := wantErr[*LockedError](t, func() error {
		_, err := e.svc.Decide(ctx, a.editor, sent.ID, VerdictApprove, "", sent.Revision)
		return err
	}(), "вердикт при чужом замке")
	if le.Holder != "owner" {
		t.Errorf("кто держит замок: %q", le.Holder)
	}
	if err := e.svc.ReleaseLock(ctx, a.owner, sent.ID); err != nil {
		t.Fatal(err)
	}
	pub := e.decide(a.editor, sent, VerdictApprove, "")
	if pub.Lock != nil || e.count("SELECT count(*) FROM document_locks WHERE document_id = ?", sent.ID) != 0 {
		t.Error("после перехода остался замок")
	}
}

// ---------------------------------------------------------------- линтер

func TestLintBlocksBrokenLinksAndDoesNotBlockWarnings(t *testing.T) {
	e := newEnv(t)
	a := e.actors()

	// цель ссылки: опубликованный и неопубликованный документы
	pub := e.decide(a.editor, e.submit(a.owner, e.objectDraft(a.owner, "Цель")), VerdictApprove, "")
	unpub := e.previewDoc(a.owner, memoContent("Не опубликован", paragraph("b1", "текст")))

	d := e.objectDraft(a.owner, "Со ссылками",
		paragraph("p", "Текст"),
		linkBlock("ok", *pub.Code),
		linkBlock("draft", *unpub.Code),
		linkBlock("broken", "О-9998"),
	)
	rep, err := e.svc.TeamLint(ctx, a.owner, d.ID)
	if err != nil {
		t.Fatal(err)
	}
	byCode := map[string]LintIssue{}
	for _, i := range rep.Issues {
		byCode[i.Code] = i
	}
	if i := byCode[LintBrokenLink]; i.Severity != LintError || i.BlockID != "broken" || !strings.Contains(i.Message, "О-9998") {
		t.Errorf("битая ссылка: %+v", i)
	}
	if i := byCode[LintUnpublishedLink]; i.Severity != LintWarning || i.BlockID != "draft" {
		t.Errorf("ссылка на неопубликованное: %+v", i)
	}
	if i := byCode[LintNoDossierHeader]; i.Severity != LintWarning {
		t.Errorf("нет шапки досье: %+v", i)
	}
	if rep.Errors != 1 || rep.Warnings != 2 || rep.Issues[0].Severity != LintError {
		t.Errorf("итоги и порядок замечаний: %+v", rep)
	}
	for _, i := range rep.Issues {
		if i.BlockID == "ok" {
			t.Errorf("исправная ссылка получила замечание: %+v", i)
		}
	}

	// ошибка не пропускает отправку; статус не меняется
	lf := wantErr[*LintFailedError](t, func() error { _, err := e.svc.Submit(ctx, a.owner, d.ID, d.Revision); return err }(), "отправка с битой ссылкой")
	if lf.Report.Errors != 1 || !strings.Contains(lf.Error(), "О-9998") {
		t.Errorf("отчёт в ошибке: %v", lf)
	}
	if st := e.mustGetStatus(d.ID); st != "draft" {
		t.Errorf("статус после отказа: %s", st)
	}

	// исправили — предупреждения отправке не мешают
	fixed := mustSave(t, e, a.owner, d, memoContent("Со ссылками", paragraph("p", "Текст"), linkBlock("draft", *unpub.Code)))
	if sent := e.submit(a.owner, fixed.Document); sent.Status != "review" {
		t.Errorf("отправка только с предупреждениями: %s", sent.Status)
	}
}

func TestApproveChecksTheCanonAgain(t *testing.T) {
	e := newEnv(t)
	a := e.actors()
	target := e.previewDoc(a.owner, memoContent("Цель", paragraph("b1", "текст")))
	d := e.submit(a.owner, e.objectDraft(a.owner, "Со ссылкой", paragraph("p", "Текст"), linkBlock("l", *target.Code)))

	// цель ссылки удалили, пока документ ждал проверки
	if err := e.svc.Delete(ctx, *target.Code); err != nil {
		t.Fatal(err)
	}
	wantErr[*LintFailedError](t, func() error { _, err := e.svc.Decide(ctx, a.editor, d.ID, VerdictApprove, "", d.Revision); return err }(), "принятие с битой ссылкой")
	if st := e.mustGetStatus(d.ID); st != "review" {
		t.Errorf("статус после отказа: %s", st)
	}
	// вернуть можно: линтер проверяет только принятие и отправку
	if got := e.decide(a.editor, d, VerdictReturn, "Ссылка ведёт в никуда"); got.Status != "draft" {
		t.Errorf("возврат не удался: %s", got.Status)
	}
}

func TestLintEmptyDocumentAndDuplicateBlocks(t *testing.T) {
	e := newEnv(t)
	a := e.actors()
	d := e.objectDraft(a.owner, "Пустой", paragraph("p", "Один"), paragraph("q", "Один"), rawBlock("s1", "divider", `{"style":"line"}`), rawBlock("s2", "divider", `{"style":"line"}`))
	rep, _ := e.svc.TeamLint(ctx, a.owner, d.ID)
	dups := 0
	for _, i := range rep.Issues {
		if i.Code == LintDuplicateBlock {
			dups++
			if i.BlockID != "q" {
				t.Errorf("повтор указан у блока %q, ожидался q (разделители повторяться могут)", i.BlockID)
			}
		}
	}
	if dups != 1 {
		t.Errorf("повторов найдено %d: %+v", dups, rep.Issues)
	}
	// документ без блоков: сервер хранит такой черновик, но линтер его не пропускает
	empty, err := e.svc.TeamCreate(ctx, a.owner, CreateInput{Type: "object", Content: memoContent("Без блоков", []InputBlock{}...)})
	if err != nil {
		t.Fatal(err)
	}
	if empty.Content.Blocks == nil || len(empty.Content.Blocks) != 0 {
		// memoContent подставляет абзац при nil; здесь нужен именно пустой список
		empty.Content.Blocks = nil
	}
	if _, err := e.svc.TeamSave(ctx, a.owner, empty.ID, empty.Revision, Content{Title: "Без блоков", Composed: &Composed{Year: 1979}, Blocks: []InputBlock{}}); err != nil {
		t.Fatal(err)
	}
	cur, _ := e.svc.TeamGet(ctx, a.owner, empty.ID)
	lf := wantErr[*LintFailedError](t, func() error { _, err := e.svc.Submit(ctx, a.owner, cur.ID, cur.Revision); return err }(), "отправка пустого документа")
	if lf.Report.Issues[0].Code != LintNoBlocks {
		t.Errorf("%+v", lf.Report.Issues)
	}
}

func TestLintVisibility(t *testing.T) {
	e := newEnv(t)
	a := e.actors()
	d := e.objectDraft(a.owner, "Личный")
	if _, err := e.svc.TeamLint(ctx, a.other, d.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("чужой Автор увидел линтер чужого черновика: %v", err)
	}
	if _, err := e.svc.TeamLint(ctx, a.director, d.ID); err != nil {
		t.Errorf("Директорат: %v", err)
	}
}

// ---------------------------------------------------------------- номера О-№

func TestObjectNumbersAreAssignedInOrderAndNeverRepeat(t *testing.T) {
	e := newEnv(t)
	a := e.actors()

	first := e.decide(a.editor, e.submit(a.owner, e.objectDraft(a.owner, "Первый")), VerdictApprove, "")
	second := e.decide(a.editor, e.submit(a.owner, e.objectDraft(a.owner, "Второй")), VerdictApprove, "")
	var c1, c2 int
	fmt.Sscanf(strings.TrimPrefix(*first.Code, "О-"), "%d", &c1)
	fmt.Sscanf(strings.TrimPrefix(*second.Code, "О-"), "%d", &c2)
	if c2 != c1+1 {
		t.Errorf("номера не по порядку: %s, %s", *first.Code, *second.Code)
	}

	// параллельная публикация: ни одного повтора номера
	const n = 6
	docs := make([]*TeamDocument, n)
	for i := range docs {
		docs[i] = e.submit(a.owner, e.objectDraft(a.owner, fmt.Sprintf("Параллельный %d", i)))
	}
	var wg sync.WaitGroup
	codes := make([]string, n)
	errs := make([]error, n)
	for i := range docs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out, err := e.svc.Decide(ctx, a.editor, docs[i].ID, VerdictApprove, "", docs[i].Revision)
			if err == nil {
				codes[i] = *out.Code
			}
			errs[i] = err
		}()
	}
	wg.Wait()
	seen := map[string]bool{}
	for i, c := range codes {
		if errs[i] != nil {
			t.Fatalf("параллельная публикация %d: %v", i, errs[i])
		}
		if seen[c] {
			t.Errorf("номер %s выдан дважды: %v", c, codes)
		}
		seen[c] = true
	}
}

func TestOtherTypesKeepTheirCodeOnPublication(t *testing.T) {
	e := newEnv(t)
	a := e.actors()
	m := e.draft(a.owner, "Меморандум")
	code := *m.Code
	pub := e.decide(a.editor, e.submit(a.owner, m), VerdictApprove, "")
	if *pub.Code != code {
		t.Errorf("шифр меморандума изменился: %s → %s", code, *pub.Code)
	}
}

// ---------------------------------------------------------------- комментарии

func TestCommentsLifecycle(t *testing.T) {
	e := newEnv(t)
	a := e.actors()
	d := e.objectDraft(a.owner, "С комментариями", paragraph("p1", "Первый"), paragraph("p2", "Второй"))

	// пока документ черновик — комментировать нельзя
	if _, err := e.svc.AddComment(ctx, a.editor, d.ID, nil, "рано"); !errors.Is(err, ErrNotFound) {
		t.Errorf("чужой черновик: %v", err) // Редактор чужого черновика не видит вовсе
	}
	if _, err := e.svc.AddComment(ctx, a.director, d.ID, nil, "рано"); err == nil {
		t.Error("комментарий к черновику принят")
	} else {
		wantErr[*StateError](t, err, "комментарий к черновику")
	}

	sent := e.submit(a.owner, d)
	block := "p2"
	c, err := e.svc.AddComment(ctx, a.editor, sent.ID, &block, "  Уточнить дату  ")
	if err != nil {
		t.Fatal(err)
	}
	if c.Body != "Уточнить дату" || c.BlockID == nil || *c.BlockID != "p2" || c.Author == nil || *c.Author != "editor" || c.Revision != sent.Revision || c.Resolved {
		t.Fatalf("комментарий: %+v", c)
	}
	general, err := e.svc.AddComment(ctx, a.editor, sent.ID, nil, "В целом хорошо")
	if err != nil || general.BlockID != nil {
		t.Fatalf("комментарий к документу: %v %+v", err, general)
	}

	// неверные данные
	ve := wantErr[*ValidationError](t, func() error {
		bad := "нет-такого"
		_, err := e.svc.AddComment(ctx, a.editor, sent.ID, &bad, "x")
		return err
	}(), "блок не из документа")
	if ve.Problems[0].Path != "block_id" {
		t.Errorf("%+v", ve.Problems)
	}
	for _, body := range []string{"", "  \n", strings.Repeat("я", MaxReviewText+1)} {
		ve := wantErr[*ValidationError](t, func() error { _, err := e.svc.AddComment(ctx, a.editor, sent.ID, nil, body); return err }(), "текст комментария")
		if ve.Problems[0].Path != "body" {
			t.Errorf("%+v", ve.Problems)
		}
	}
	// автор документа рецензентом не является
	if _, err := e.svc.AddComment(ctx, a.owner, sent.ID, nil, "сам"); !errors.Is(err, ErrForbidden) {
		t.Errorf("Автор прокомментировал собственный документ: %v", err)
	}
	// посторонний не видит документ вовсе
	if _, err := e.svc.TeamReview(ctx, a.other, sent.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("чужой Автор увидел рецензию: %v", err)
	}

	// свой комментарий автор комментария удаляет сам, пока документ на проверке
	temp, err := e.svc.AddComment(ctx, a.editor, sent.ID, nil, "Лишнее")
	if err != nil {
		t.Fatal(err)
	}
	if !temp.CanDelete {
		t.Error("автору комментария не предложено удаление")
	}
	if err := e.svc.DeleteComment(ctx, a.editor, sent.ID, temp.ID); err != nil {
		t.Errorf("автор комментария не смог его удалить: %v", err)
	}

	// вердикт «вернуть»: комментарии остаются, автор их видит и отмечает исправленными
	back := e.decide(a.editor, sent, VerdictReturn, "См. комментарии")
	info, err := e.svc.TeamReview(ctx, a.owner, back.ID)
	if err != nil || len(info.Comments) != 2 || info.Open != 2 {
		t.Fatalf("рецензия для автора: %v %+v", err, info)
	}
	if !info.Comments[0].CanResolve || info.Comments[0].CanDelete {
		t.Errorf("права автора на чужой комментарий: %+v", info.Comments[0])
	}
	done, err := e.svc.SetCommentResolved(ctx, a.owner, back.ID, c.ID, true)
	if err != nil || !done.Resolved || done.ResolvedBy == nil || *done.ResolvedBy != "owner" || done.ResolvedAt == nil {
		t.Fatalf("отметка «исправлено»: %v %+v", err, done)
	}
	if info, _ = e.svc.TeamReview(ctx, a.owner, back.ID); info.Open != 1 {
		t.Errorf("открытых комментариев %d, ожидался 1", info.Open)
	}
	reopened, err := e.svc.SetCommentResolved(ctx, a.director, back.ID, c.ID, false)
	if err != nil || reopened.Resolved || reopened.ResolvedBy != nil {
		t.Fatalf("возврат в открытые: %v %+v", err, reopened)
	}
	// посторонний отмечать не вправе (документа он не видит), другой Автор тоже
	if _, err := e.svc.SetCommentResolved(ctx, a.other, back.ID, c.ID, true); !errors.Is(err, ErrNotFound) {
		t.Errorf("чужой Автор отметил комментарий: %v", err)
	}

	// удаление: свой — автор комментария; чужой — только Директорат
	if err := e.svc.DeleteComment(ctx, a.owner, back.ID, c.ID); !errors.Is(err, ErrForbidden) {
		t.Errorf("автор документа удалил чужой комментарий: %v", err)
	}
	// после возврата рецензент чужой черновик уже не видит; автор комментария удаляет его, пока документ на проверке
	if err := e.svc.DeleteComment(ctx, a.director, back.ID, general.ID); err != nil {
		t.Errorf("Директорат не смог удалить: %v", err)
	}
	if err := e.svc.DeleteComment(ctx, a.director, back.ID, c.ID); err != nil {
		t.Errorf("Директорат не смог удалить: %v", err)
	}
	if err := e.svc.DeleteComment(ctx, a.director, back.ID, c.ID); !errors.Is(err, ErrCommentNotFound) {
		t.Errorf("повторное удаление: %v", err)
	}
	// комментарий чужого документа по своему id не достать
	other := e.submit(a.owner, e.objectDraft(a.owner, "Другой"))
	if _, err := e.svc.SetCommentResolved(ctx, a.editor, other.ID, c.ID, true); !errors.Is(err, ErrCommentNotFound) {
		t.Errorf("комментарий другого документа доступен по чужому id: %v", err)
	}
}

func TestCommentsSurviveBlockRemovalAndDieWithDocument(t *testing.T) {
	e := newEnv(t)
	a := e.actors()
	sent := e.submit(a.owner, e.objectDraft(a.owner, "Блок исчезнет", paragraph("gone", "Скоро удалят"), paragraph("stay", "Останется")))
	b := "gone"
	if _, err := e.svc.AddComment(ctx, a.editor, sent.ID, &b, "Спорный абзац"); err != nil {
		t.Fatal(err)
	}
	// автор убрал блок: комментарий остаётся (в интерфейсе — «блок удалён»)
	mustSave(t, e, a.owner, sent, memoContent("Блок исчезнет", paragraph("stay", "Останется")))
	info, _ := e.svc.TeamReview(ctx, a.owner, sent.ID)
	if len(info.Comments) != 1 || info.Comments[0].BlockID == nil || *info.Comments[0].BlockID != "gone" {
		t.Errorf("комментарий пропал вместе с блоком: %+v", info.Comments)
	}

	if err := e.db.Exec("DELETE FROM documents WHERE id = ?", sent.ID).Error; err != nil {
		t.Fatal(err)
	}
	for _, tbl := range []string{"review_comments", "review_events"} {
		if n := e.count("SELECT count(*) FROM "+tbl+" WHERE document_id = ?", sent.ID); n != 0 {
			t.Errorf("%s: записи удалённого документа остались (%d)", tbl, n)
		}
	}
}

func TestReviewAuthorsAnonymisedWhenAccountDeleted(t *testing.T) {
	e := newEnv(t)
	a := e.actors()
	sent := e.submit(a.owner, e.objectDraft(a.owner, "Аккаунт уйдёт"))
	if _, err := e.svc.AddComment(ctx, a.editor, sent.ID, nil, "Комментарий останется"); err != nil {
		t.Fatal(err)
	}
	e.decide(a.editor, sent, VerdictReturn, "Причина останется")
	if err := e.db.Exec("DELETE FROM users WHERE id = ?", a.editor.UserID).Error; err != nil {
		t.Fatal(err)
	}
	info, err := e.svc.TeamReview(ctx, a.owner, sent.ID)
	if err != nil || len(info.Comments) != 1 || info.Comments[0].Author != nil || info.Comments[0].Body != "Комментарий останется" {
		t.Fatalf("комментарий удалённого рецензента: %v %+v", err, info.Comments)
	}
	last := info.Events[len(info.Events)-1]
	if last.Actor != nil || last.Comment != "Причина останется" {
		t.Errorf("событие удалённого рецензента: %+v", last)
	}
}

// ---------------------------------------------------------------- ограничения схемы

func TestReviewSchemaConstraints(t *testing.T) {
	e := newEnv(t)
	a := e.actors()
	d := e.objectDraft(a.owner, "Схема")
	now := e.clock()
	for name, q := range map[string]string{
		"комментарий без текста":     `INSERT INTO review_comments (document_id, revision, body, created_at) VALUES (?, 1, '   ', ?)`,
		"пустой идентификатор блока": `INSERT INTO review_comments (document_id, block_id, revision, body, created_at) VALUES (?, '', 1, 'x', ?)`,
		"возврат без причины":        `INSERT INTO review_events (document_id, kind, revision, comment, created_at) VALUES (?, 'return', 1, ' ', ?)`,
		"отклонение без причины":     `INSERT INTO review_events (document_id, kind, revision, comment, created_at) VALUES (?, 'reject', 1, '', ?)`,
		"неизвестный вид события":    `INSERT INTO review_events (document_id, kind, revision, created_at) VALUES (?, 'approved', 1, ?)`,
		"редакция меньше единицы":    `INSERT INTO review_events (document_id, kind, revision, created_at) VALUES (?, 'submit', 0, ?)`,
		"отметил без времени":        `INSERT INTO review_comments (document_id, revision, body, resolved_by, created_at) VALUES (?, 1, 'x', ?, ?)`,
	} {
		args := []any{d.ID, now}
		if name == "отметил без времени" {
			args = []any{d.ID, a.owner.UserID, now}
		}
		if err := e.db.Exec(q, args...).Error; err == nil {
			t.Errorf("%s: запись принята", name)
		}
	}
	// корректные записи проходят
	if err := e.db.Exec(`INSERT INTO review_events (document_id, kind, revision, comment, created_at) VALUES (?, 'return', 1, 'причина', ?)`, d.ID, now).Error; err != nil {
		t.Errorf("корректное событие отклонено: %v", err)
	}
	if err := e.db.Exec(`INSERT INTO review_comments (document_id, block_id, revision, body, created_at) VALUES (?, 'b1', 1, 'ok', ?)`, d.ID, now).Error; err != nil {
		t.Errorf("корректный комментарий отклонён: %v", err)
	}
}

// Ответы серверу удобно сравнивать как JSON: права попадают в TeamDocument и не теряются при сериализации.
func TestWorkflowIsPartOfTeamDocumentJSON(t *testing.T) {
	e := newEnv(t)
	a := e.actors()
	d := e.objectDraft(a.owner, "JSON")
	raw, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"workflow":{"submit":true,"withdraw":false,"review":false,"comment":false,"archive":false,"unarchive":false}`) {
		t.Errorf("workflow в ответе: %s", raw)
	}
}
