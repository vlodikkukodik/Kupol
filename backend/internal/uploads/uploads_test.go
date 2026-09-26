package uploads

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"os"
	"strings"
	"testing"
	"time"

	"kupol/internal/testutil"
)

var ctx = context.Background()

func newSvc(t *testing.T) (*Service, int64) {
	t.Helper()
	db := testutil.NewMigratedDB(t)
	s, err := NewService(db, t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	var uid int64
	if err := db.Raw(`INSERT INTO users (login, password_hash, backup_code_hash, created_at, password_changed_at) VALUES ('uploader', 'h', 'b', ?, ?) RETURNING id`, now, now).Scan(&uid).Error; err != nil {
		t.Fatal(err)
	}
	return s, uid
}

func pngBytes(w, h int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for x := 0; x < w; x++ {
		for y := 0; y < h; y++ {
			img.Set(x, y, color.RGBA{uint8(x), uint8(y), 90, 255})
		}
	}
	var b bytes.Buffer
	_ = png.Encode(&b, img)
	return b.Bytes()
}

func TestSaveImageConvertsToWebPWithThumb(t *testing.T) {
	s, uid := newSvc(t)
	up, err := s.Save(ctx, uid, "../../фото.png", 2, bytes.NewReader(pngBytes(3000, 1000)))
	if err != nil {
		t.Fatal(err)
	}
	if up.Kind != Image || up.Mime != "image/webp" || up.Name != "фото.png" || up.Level != 2 {
		t.Fatalf("запись: %+v", up)
	}
	if *up.Width != maxImageSide || *up.Height != 800 {
		t.Errorf("размер после уменьшения: %dx%d", *up.Width, *up.Height)
	}
	for _, suffix := range []string{".webp", "_thumb.webp"} {
		raw, err := os.ReadFile(s.path(up.Key, suffix))
		if err != nil || !bytes.HasPrefix(raw, []byte("RIFF")) || !strings.Contains(string(raw[:16]), "WEBP") {
			t.Errorf("%s: не WebP (%v)", suffix, err)
		}
	}
	// закрытое неотличимо от несуществующего
	if _, _, err := s.Open(ctx, 1, up.Key, false); err != ErrNotFound {
		t.Errorf("низкий допуск: %v", err)
	}
	if p, mime, err := s.Open(ctx, 2, up.Key, true); err != nil || mime != "image/webp" || !strings.HasSuffix(p, "_thumb.webp") {
		t.Errorf("превью: %s %s %v", p, mime, err)
	}
}

func TestSaveRejectsGarbageAndOversize(t *testing.T) {
	s, uid := newSvc(t)
	if _, err := s.Save(ctx, uid, "x.png", 0, strings.NewReader("это не картинка")); err != ErrUnsupported {
		t.Errorf("мусор: %v", err)
	}
	if _, err := s.Save(ctx, uid, "x.png", 0, bytes.NewReader(append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 100)...))); err != ErrUnsupported {
		t.Errorf("битый PNG: %v", err)
	}
	if _, err := s.Save(ctx, uid, "big.ogg", 0, bytes.NewReader(append([]byte("OggS"), make([]byte, MaxBytes)...))); err != ErrTooLarge {
		t.Errorf("слишком большой: %v", err)
	}
	if _, err := s.Save(ctx, uid, "x.ogg", 9, strings.NewReader("OggS")); err == nil {
		t.Error("уровень 9 принят")
	}
	if entries, _ := os.ReadDir(s.dir); len(entries) != 0 {
		t.Errorf("неудачные загрузки оставили файлы: %d", len(entries))
	}
}

func TestSaveAudioAndDelete(t *testing.T) {
	s, uid := newSvc(t)
	up, err := s.Save(ctx, uid, "запись.mp3", 0, strings.NewReader("ID3\x03\x00\x00\x00\x00\x00\x00audio-data"))
	if err != nil || up.Mime != "audio/mpeg" || up.Kind != Audio {
		t.Fatalf("аудио: %+v %v", up, err)
	}
	if _, _, err := s.Open(ctx, 0, up.Key, true); err != ErrNotFound {
		t.Errorf("превью у аудио: %v", err)
	}
	// чужой не удаляет, владелец — да
	if err := s.Delete(ctx, Actor{ID: uid + 100}, up.ID); err != ErrNotFound {
		t.Errorf("чужой: %v", err)
	}
	// пока на файл ссылается документ — 409
	if err := s.db.Exec(`INSERT INTO documents (type, title, status, level, composed_year, revision, created_at, updated_at, blocks)
		VALUES ('memo', 'x', 'draft', 0, 1979, 1, now(), now(), ?::jsonb)`, `[{"id":"a1","type":"audio","data":{"upload":"`+up.Key+`"}}]`).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(ctx, Actor{ID: uid}, up.ID); err != ErrInUse {
		t.Errorf("используется: %v", err)
	}
	s.db.Exec("DELETE FROM documents")
	if err := s.Delete(ctx, Actor{ID: uid}, up.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Open(ctx, 7, up.Key, false); err != ErrNotFound {
		t.Errorf("после удаления: %v", err)
	}
}

func TestTicketIsSingleUseAndExpires(t *testing.T) {
	s, uid := newSvc(t)
	tk, _, err := s.NewTicket(uid)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := s.UseTicket(tk); err != nil || got != uid {
		t.Fatalf("первое использование: %d %v", got, err)
	}
	if _, err := s.UseTicket(tk); err != ErrBadTicket {
		t.Errorf("повтор: %v", err)
	}
	tk2, _, _ := s.NewTicket(uid)
	s.now = func() time.Time { return time.Now().UTC().Add(TicketTTL + time.Second) }
	if _, err := s.UseTicket(tk2); err != ErrBadTicket {
		t.Errorf("просроченный: %v", err)
	}
}
