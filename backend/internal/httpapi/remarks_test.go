package httpapi_test

import (
	"context"
	"strconv"
	"testing"

	"kupol/internal/accounts"
	"kupol/internal/documents"
)

func TestRemarksEndToEnd(t *testing.T) {
	st := newStack(t, nil)
	ctx := context.Background()
	if _, err := st.docs.Import(ctx, []byte(`{"code":"О-201","type":"object","title":"Тест","status":"published","level":0,"composed":{"year":1979}}`), documents.ImportOptions{}); err != nil {
		t.Fatal(err)
	}

	guest := st.newClient(t)
	reader1 := st.newClient(t)
	reader1.register("remreader1", teamPassword)
	reader2 := st.newClient(t)
	reader2.register("remreader2", teamPassword)
	mod := st.newClient(t)
	mod.register("remmod", teamPassword)
	if _, _, err := st.svc.GrantRole(ctx, "remmod", accounts.RoleModerator, nil); err != nil {
		t.Fatal(err)
	}

	// гость не пишет
	want(t, guest.do("POST", "/api/documents/O-201/remarks", map[string]any{"text": "первая"}), 401, "unauthenticated")

	r := reader1.do("POST", "/api/documents/O-201/remarks", map[string]any{"text": "первая пометка"})
	want(t, r, 201, "")
	remark := r.json()["remark"].(map[string]any)
	id := int64(remark["id"].(float64))
	if remark["author"] != "remreader1" || remark["can_delete"] != true {
		t.Fatalf("пометка: %v", remark)
	}

	reply := reader2.do("POST", "/api/documents/O-201/remarks", map[string]any{"parent_id": id, "text": "ответ"})
	want(t, reply, 201, "")

	list := guest.do("GET", "/api/documents/O-201/remarks", nil)
	want(t, list, 200, "")
	items := list.json()["items"].([]any)
	if len(items) != 2 {
		t.Fatalf("список: %v", items)
	}
	first := items[0].(map[string]any)
	if first["can_delete"] != false {
		t.Errorf("гостю нельзя удалять: %v", first)
	}

	// запрещённое слово
	bad := reader1.do("POST", "/api/documents/O-201/remarks", map[string]any{"text": "ты дебил"})
	want(t, bad, 422, "validation")

	// чужую не удалить
	want(t, reader2.do("DELETE", "/api/remarks/"+strconv.FormatInt(id, 10), nil), 403, "forbidden")

	// жалоба, затем модерация
	want(t, reader2.do("POST", "/api/remarks/"+strconv.FormatInt(id, 10)+"/report", nil), 204, "")
	want(t, reader1.do("POST", "/api/remarks/"+strconv.FormatInt(id, 10)+"/report", nil), 204, "") // на свою тоже можно пожаловаться

	want(t, reader1.do("GET", "/api/team/remarks/reported", nil), 403, "forbidden")
	rep := mod.do("GET", "/api/team/remarks/reported", nil)
	want(t, rep, 200, "")
	reported := rep.json()["items"].([]any)
	if len(reported) != 1 {
		t.Fatalf("жалобы: %v", reported)
	}
	repItem := reported[0].(map[string]any)
	if repItem["report_count"] != float64(2) || repItem["document_code"] != "О-201" {
		t.Fatalf("жалоба: %v", repItem)
	}

	// модератор удаляет чужую пометку
	want(t, mod.do("DELETE", "/api/remarks/"+strconv.FormatInt(id, 10), nil), 204, "")
	list = guest.do("GET", "/api/documents/O-201/remarks", nil)
	if n := len(list.json()["items"].([]any)); n != 0 {
		t.Fatalf("после удаления родителя (каскад) осталось: %d", n)
	}
}
