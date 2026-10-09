package httpapi_test

import (
	"context"
	"strconv"
	"testing"

	"kupol/internal/documents"
)

// TestPDFPrintEndToEnd — печать дела (этап 6.2): вошедший ставит джобу, следит за состоянием
// и забирает готовый PDF; чужая печать неотличима от несуществующей.
func TestPDFPrintEndToEnd(t *testing.T) {
	st := newStack(t, nil)
	ctx := context.Background()
	if _, err := st.docs.Import(ctx, []byte(`{"code":"О-301","type":"object","title":"Печать","status":"published","level":0,"composed":{"year":1980}}`), documents.ImportOptions{}); err != nil {
		t.Fatal(err)
	}

	guest := st.newClient(t)
	owner := st.newClient(t)
	owner.register("pdfowner", teamPassword)
	other := st.newClient(t)
	other.register("pdfother", teamPassword)

	// гость не печатает
	want(t, guest.do("POST", "/api/documents/O-301/pdfs", nil), 401, "unauthenticated")

	// неизвестное дело — 404 и без джобы
	want(t, owner.do("POST", "/api/documents/O-999/pdfs", nil), 404, "not_found")

	// постановка в очередь
	r := owner.do("POST", "/api/documents/O-301/pdfs", nil)
	want(t, r, 202, "")
	id := int64(r.json()["id"].(float64))
	if id == 0 {
		t.Fatalf("номер джобы: %v", r.json())
	}

	// состояние: сначала queued
	stt := owner.do("GET", "/api/pdfs/"+strconv.FormatInt(id, 10), nil)
	want(t, stt, 200, "")
	if got := stt.json()["state"]; got != "queued" {
		t.Errorf("состояние: %v", got)
	}
	if got := stt.json()["ref"]; got != "O-301" {
		t.Errorf("шифр: %v", got)
	}

	// файла ещё нет
	want(t, owner.do("GET", "/api/pdfs/"+strconv.FormatInt(id, 10)+"/file", nil), 404, "not_found")

	// чужим — не видно
	want(t, other.do("GET", "/api/pdfs/"+strconv.FormatInt(id, 10), nil), 404, "not_found")
	want(t, other.do("GET", "/api/pdfs/"+strconv.FormatInt(id, 10)+"/file", nil), 404, "not_found")

	// воркер доделал
	st.pdfs.finish(id, []byte("%PDF-1.4 тест"))

	ready := owner.do("GET", "/api/pdfs/"+strconv.FormatInt(id, 10), nil)
	want(t, ready, 200, "")
	if got := ready.json()["state"]; got != "ready" {
		t.Fatalf("состояние после воркера: %v", got)
	}

	f := owner.do("GET", "/api/pdfs/"+strconv.FormatInt(id, 10)+"/file", nil)
	want(t, f, 200, "")
	if ct := f.Header().Get("Content-Type"); ct != "application/pdf" {
		t.Errorf("Content-Type: %q", ct)
	}
	if body := f.Body.String(); body != "%PDF-1.4 тест" {
		t.Errorf("тело: %q", body)
	}
	// чужой файл так и остался скрыт
	want(t, other.do("GET", "/api/pdfs/"+strconv.FormatInt(id, 10)+"/file", nil), 404, "not_found")

	// мусорный номер — 404
	want(t, owner.do("GET", "/api/pdfs/999999999", nil), 404, "not_found")
	want(t, owner.do("GET", "/api/pdfs/abc", nil), 404, "not_found")
}
