package pdf

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"kupol/internal/documents"
	"kupol/internal/testutil"
	"kupol/internal/uploads"
)

// TestQueuePrintsDocument — печать целиком: джоба → воркер → typst → готовый PDF на диске.
func TestQueuePrintsDocument(t *testing.T) {
	requireTypst(t)
	ctx := context.Background()
	db := testutil.NewMigratedDB(t)

	docs := documents.NewService(db, testutil.Logger(), nil)
	if _, err := docs.Import(ctx, []byte(`{"code":"О-501","type":"object","title":"Очередь","status":"published","level":0,"composed":{"year":1981}}`), documents.ImportOptions{}); err != nil {
		t.Fatal(err)
	}
	ups, err := uploads.NewService(db, t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	q, err := NewQueue(ctx, QueueConfig{
		DB: db, Dir: t.TempDir(), Documents: docs, Uploads: ups, SiteOrigin: "https://kupol.test", Log: testutil.Logger(),
	})
	if err != nil {
		t.Fatalf("очередь: %v", err)
	}
	if err := q.Start(ctx); err != nil {
		t.Fatalf("старт: %v", err)
	}
	t.Cleanup(func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		q.Stop(stopCtx)
	})

	id, err := q.Enqueue(ctx, Args{Ref: "О-501", UserID: 42, UserLevel: 2, Lang: "ru", SiteOrigin: "https://kupol.test"})
	if err != nil {
		t.Fatalf("постановка: %v", err)
	}

	// ждём конца печати (typst — секунды)
	var st *Status
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		st, err = q.Status(ctx, id)
		if err != nil {
			t.Fatalf("статус: %v", err)
		}
		if st.State == StateReady || st.State == StateFailed {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if st.State != StateReady {
		t.Fatalf("состояние: %s (хотелось ready)", st.State)
	}
	if st.UserID != 42 || st.Ref != "О-501" {
		t.Errorf("аргументы джобы: %+v", st)
	}

	data, err := os.ReadFile(q.File(id))
	if err != nil {
		t.Fatalf("файл: %v", err)
	}
	if !strings.HasPrefix(string(data), "%PDF") {
		t.Errorf("не PDF: %.20q", data)
	}

	// чужой номер не найден; мусорный — тоже
	if _, err := q.Status(ctx, 999999999); err == nil {
		t.Errorf("неизвестная джобы нет ошибки")
	}
}

// TestQueueRejectsNoTypst — без typst очередь поднимается, но печать падает воркером, а не сервером.
func TestQueueRejectsNoTypst(t *testing.T) {
	if _, err := DefaultRunner(); err == nil {
		t.Skip("typst есть — ветка «нет typst» недостижима")
	}
	ctx := context.Background()
	db := testutil.NewMigratedDB(t)
	docs := documents.NewService(db, testutil.Logger(), nil)
	ups, err := uploads.NewService(db, t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	q, err := NewQueue(ctx, QueueConfig{DB: db, Dir: t.TempDir(), Documents: docs, Uploads: ups, Log: testutil.Logger()})
	if err != nil {
		t.Fatalf("очередь без typst должна подниматься: %v", err)
	}
	if err := q.Start(ctx); err != nil {
		t.Fatalf("старт: %v", err)
	}
	t.Cleanup(func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		q.Stop(stopCtx)
	})
	id, err := q.Enqueue(ctx, Args{Ref: "О-000"})
	if err != nil {
		t.Fatalf("постановка: %v", err)
	}
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		st, err := q.Status(ctx, id)
		if err != nil {
			t.Fatalf("статус: %v", err)
		}
		if st.State == StateFailed {
			return // джоба упала, сервер жив
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Errorf("джоба без typst так и не упала")
}
