package httpapi_test

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func testPNG() []byte {
	img := image.NewRGBA(image.Rect(0, 0, 64, 32))
	for x := 0; x < 64; x++ {
		img.Set(x, 5, color.RGBA{200, 0, 0, 255})
	}
	var b bytes.Buffer
	_ = png.Encode(&b, img)
	return b.Bytes()
}

// putFile отправляет multipart на путь загрузки от имени клиента c (тем же Origin, что у сайта).
func putFile(st *stack, c *client, path, name string, data []byte, level string) *httptest.ResponseRecorder {
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, _ := mw.CreateFormFile("file", name)
	fw.Write(data)
	if level != "" {
		mw.WriteField("level", level)
	}
	mw.Close()
	req := httptest.NewRequest(http.MethodPost, path, &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()
	st.r.ServeHTTP(rec, req)
	return rec
}

func TestUploadsOverHTTP(t *testing.T) {
	st, actors := teamStackWithActors(t)
	author, plain, guest := actors["author"], actors["plain"], actors["guest"]

	if r := plain.client.do("POST", "/api/team/uploads/ticket", nil); r.Code != 403 {
		t.Errorf("билет без права писать: %d", r.Code)
	}
	if r := guest.client.do("POST", "/api/team/uploads/ticket", nil); r.Code != 401 {
		t.Errorf("гость: %d", r.Code)
	}
	ticket := func() string {
		r := author.client.do("POST", "/api/team/uploads/ticket", nil)
		if r.Code != 200 {
			t.Fatalf("билет: %d %s", r.Code, r.Body)
		}
		return r.json()["path"].(string)
	}

	// мусор — 422, билет при этом сгорел
	p := ticket()
	if rec := putFile(st, author.client, p, "x.png", []byte("не картинка"), ""); rec.Code != 422 {
		t.Errorf("мусор: %d %s", rec.Code, rec.Body)
	}
	if rec := putFile(st, author.client, p, "x.png", testPNG(), ""); rec.Code != 403 {
		t.Errorf("повторный билет: %d", rec.Code)
	}

	// картинка уровня 2
	rec := putFile(st, author.client, ticket(), "схема.png", testPNG(), "2")
	if rec.Code != 201 {
		t.Fatalf("загрузка: %d %s", rec.Code, rec.Body)
	}
	var out struct {
		Upload struct {
			Key  string `json:"key"`
			ID   int64  `json:"id"`
			Mime string `json:"mime"`
		} `json:"upload"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if out.Upload.Mime != "image/webp" || len(out.Upload.Key) != 32 {
		t.Fatalf("запись: %+v", out)
	}

	// отдача: гость и низкий допуск — 404, допущенный — WebP
	fileURL := "/api/uploads/" + out.Upload.Key + "/file"
	if r := guest.client.do("GET", fileURL, nil); r.Code != 404 {
		t.Errorf("гость: %d", r.Code)
	}
	if r := actors["moderator"].client.do("GET", fileURL, nil); r.Code != 404 {
		t.Errorf("уровень 1: %d", r.Code)
	}
	ok := actors["director"].client.do("GET", "/api/uploads/"+out.Upload.Key+"/thumb", nil)
	if ok.Code != 200 || !strings.HasPrefix(ok.Body.String(), "RIFF") || ok.Header().Get("Content-Type") != "image/webp" {
		t.Errorf("превью: %d %q", ok.Code, ok.Header().Get("Content-Type"))
	}
	if ok.Header().Get("X-Content-Type-Options") == "" {
		t.Error("нет nosniff")
	}

	// список: автор видит своё, чужому — пусто
	if items := author.client.do("GET", "/api/team/uploads", nil).json()["items"].([]any); len(items) != 1 {
		t.Errorf("список автора: %v", items)
	}
	if items := actors["moderator"].client.do("GET", "/api/team/uploads", nil); items.Code != 403 {
		t.Errorf("список без права писать: %d", items.Code)
	}

	// удаление
	del := "/api/team/uploads/" + itoa64(out.Upload.ID)
	if r := actors["editor"].client.do("DELETE", del, nil); r.Code != 204 {
		t.Errorf("редактор удаляет чужое: %d %s", r.Code, r.Body)
	}
	if r := author.client.do("DELETE", del, nil); r.Code != 404 {
		t.Errorf("повторное удаление: %d", r.Code)
	}
	if r := actors["director"].client.do("GET", fileURL, nil); r.Code != 404 {
		t.Errorf("после удаления: %d", r.Code)
	}
}
