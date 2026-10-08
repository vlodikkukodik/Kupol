// Package uploads — загрузки (этап 6.1, спецификация §2): JPEG/PNG/WebP и аудио mp3/ogg до 20 МБ.
//
// Картинку libvips (vipsthumbnail) перекодирует в WebP с исправлением поворота и без EXIF, рядом кладётся превью; аудио
// проверяется по сигнатуре и хранится как есть. Файлы отдаются по случайному ключу (не по номеру, чтобы их нельзя было
// перебрать) и только читателю с допуском не ниже уровня загрузки. Загружает член команды с правом писать по одноразовому
// билету (ticket): билет привязан к человеку, живёт пять минут и гаснет после первой загрузки.
package uploads

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"
)

const (
	MaxBytes  = 20 << 20
	TicketTTL = 5 * time.Minute

	maxImageSide = 2400
	thumbSide    = 480
	toolTimeout  = 60 * time.Second
)

type Kind string

const (
	Image Kind = "image"
	Audio Kind = "audio"
)

var (
	ErrNotFound    = errors.New("uploads: не найдено")
	ErrForbidden   = errors.New("uploads: недостаточно прав")
	ErrBadTicket   = errors.New("uploads: билет недействителен")
	ErrTooLarge    = errors.New("uploads: файл слишком большой")
	ErrUnsupported = errors.New("uploads: неподдерживаемый или повреждённый файл")
	ErrInUse       = errors.New("uploads: файл используется в документе")
)

// Upload — запись о файле.
type Upload struct {
	ID        int64     `json:"id" gorm:"primaryKey"`
	Key       string    `json:"key"`
	OwnerID   *int64    `json:"-"`
	Kind      Kind      `json:"kind"`
	Name      string    `json:"name"`
	Mime      string    `json:"mime"`
	Size      int64     `json:"size"`
	Width     *int      `json:"width,omitempty"`
	Height    *int      `json:"height,omitempty"`
	Level     int       `json:"level"`
	CreatedAt time.Time `json:"created_at"`
	// Owner — логин загрузившего (только в списках).
	Owner string `json:"owner,omitempty" gorm:"-"`
}

func (Upload) TableName() string { return "uploads" }

type ticket struct {
	userID  int64
	expires time.Time
}

type Service struct {
	db      *gorm.DB
	dir     string
	now     func() time.Time
	mu      sync.Mutex
	tickets map[string]ticket
}

func NewService(db *gorm.DB, dir string, now func() time.Time) (*Service, error) {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, fmt.Errorf("uploads: каталог %s: %w", dir, err)
	}
	return &Service{db: db, dir: dir, now: now, tickets: map[string]ticket{}}, nil
}

func randomKey(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// NewTicket выдаёт одноразовый билет на загрузку.
func (s *Service) NewTicket(userID int64) (string, time.Time, error) {
	t, err := randomKey(24)
	if err != nil {
		return "", time.Time{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	for k, v := range s.tickets { // заодно чистим просроченные
		if now.After(v.expires) {
			delete(s.tickets, k)
		}
	}
	exp := now.Add(TicketTTL)
	s.tickets[t] = ticket{userID: userID, expires: exp}
	return t, exp, nil
}

// UseTicket гасит билет и возвращает, чей он.
func (s *Service) UseTicket(t string) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.tickets[t]
	delete(s.tickets, t)
	if !ok || s.now().After(v.expires) {
		return 0, ErrBadTicket
	}
	return v.userID, nil
}

func (s *Service) path(key, suffix string) string { return filepath.Join(s.dir, key+suffix) }

// sniff определяет вид и расширение по первым байтам; ok=false — формат не поддерживается.
func sniff(head []byte) (kind Kind, mime, ext string, ok bool) {
	switch ct := http.DetectContentType(head); ct {
	case "image/jpeg", "image/png", "image/webp":
		return Image, "image/webp", ".webp", true
	}
	switch {
	case len(head) >= 4 && string(head[:4]) == "OggS":
		return Audio, "audio/ogg", ".ogg", true
	case len(head) >= 3 && string(head[:3]) == "ID3", len(head) >= 2 && head[0] == 0xFF && head[1]&0xE0 == 0xE0:
		return Audio, "audio/mpeg", ".mp3", true
	}
	return "", "", "", false
}

func run(ctx context.Context, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, toolTimeout)
	defer cancel()
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

// Save проверяет и сохраняет файл. r читается не больше MaxBytes+1 байт.
func (s *Service) Save(ctx context.Context, ownerID int64, filename string, level int, r io.Reader) (*Upload, error) {
	if level < 0 || level > 7 {
		return nil, fmt.Errorf("%w: уровень", ErrUnsupported)
	}
	data, err := io.ReadAll(io.LimitReader(r, MaxBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > MaxBytes {
		return nil, ErrTooLarge
	}
	head := data
	if len(head) > 512 {
		head = head[:512]
	}
	kind, mime, ext, ok := sniff(head)
	if !ok {
		return nil, ErrUnsupported
	}
	key, err := randomKey(16)
	if err != nil {
		return nil, err
	}
	up := &Upload{Key: key, OwnerID: &ownerID, Kind: kind, Name: cleanName(filename), Mime: mime, Level: level, CreatedAt: s.now()}
	var created []string
	fail := func(e error) (*Upload, error) {
		for _, f := range created {
			_ = os.Remove(f)
		}
		return nil, e
	}
	if kind == Image {
		src, err := os.CreateTemp(s.dir, "in-*")
		if err != nil {
			return nil, err
		}
		defer os.Remove(src.Name())
		if _, err := src.Write(data); err != nil {
			src.Close()
			return nil, err
		}
		src.Close()
		main, thumb := s.path(key, ext), s.path(key, "_thumb.webp")
		created = append(created, main, thumb)
		if out, err := run(ctx, "vipsthumbnail", src.Name(), "--size", fmt.Sprintf("%dx%d>", maxImageSide, maxImageSide), "-o", main+"[Q=82,strip]"); err != nil {
			_ = out
			return fail(ErrUnsupported)
		}
		if out, err := run(ctx, "vipsthumbnail", src.Name(), "--size", fmt.Sprintf("%dx%d>", thumbSide, thumbSide), "-o", thumb+"[Q=75,strip]"); err != nil {
			_ = out
			return fail(ErrUnsupported)
		}
		w, h := header(ctx, main, "width"), header(ctx, main, "height")
		if w <= 0 || h <= 0 {
			return fail(ErrUnsupported)
		}
		up.Width, up.Height = &w, &h
		st, err := os.Stat(main)
		if err != nil {
			return fail(err)
		}
		up.Size = st.Size()
	} else {
		p := s.path(key, ext)
		created = append(created, p)
		if err := os.WriteFile(p, data, 0o640); err != nil {
			return fail(err)
		}
		up.Size = int64(len(data))
	}
	if err := s.db.WithContext(ctx).Create(up).Error; err != nil {
		return fail(err)
	}
	return up, nil
}

func header(ctx context.Context, file, field string) int {
	out, err := run(ctx, "vipsheader", "-f", field, file)
	if err != nil {
		return 0
	}
	n, _ := strconv.Atoi(strings.TrimSpace(string(out)))
	return n
}

func cleanName(name string) string {
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	r := []rune(name)
	if len(r) > 120 {
		r = r[:120]
	}
	if len(r) == 0 || string(r) == "." || string(r) == "/" {
		return "file"
	}
	return strings.Map(func(c rune) rune {
		if c < 32 {
			return -1
		}
		return c
	}, string(r))
}

// Open возвращает путь и тип файла для читателя с допуском viewerLevel; thumb — превью картинки.
func (s *Service) Open(ctx context.Context, viewerLevel int, key string, thumb bool) (path, mime string, err error) {
	var up Upload
	if err := s.db.WithContext(ctx).Where("key = ?", key).Take(&up).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", "", ErrNotFound
		}
		return "", "", err
	}
	if up.Level > viewerLevel || (thumb && up.Kind != Image) {
		return "", "", ErrNotFound // закрытое неотличимо от несуществующего
	}
	ext := map[string]string{"image/webp": ".webp", "audio/ogg": ".ogg", "audio/mpeg": ".mp3"}[up.Mime]
	if thumb {
		return s.path(key, "_thumb.webp"), "image/webp", nil
	}
	return s.path(key, ext), up.Mime, nil
}

// Actor — кто просматривает и удаляет в панели.
type Actor struct {
	ID          int64
	Directorate bool
	Manager     bool // вправе удалять чужое (Редактор)
}

// List — загрузки: свои, а Директорат и Редактор видят все.
func (s *Service) List(ctx context.Context, a Actor) ([]Upload, error) {
	q := s.db.WithContext(ctx).Table("uploads u").Select("u.*, o.login::text AS owner").Joins("LEFT JOIN users o ON o.id = u.owner_id")
	if !a.Directorate && !a.Manager {
		q = q.Where("u.owner_id = ?", a.ID)
	}
	var rows []Upload
	if err := q.Order("u.created_at DESC, u.id DESC").Limit(300).Scan(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

// Delete удаляет файл, если на него не ссылается ни один блок документа.
func (s *Service) Delete(ctx context.Context, a Actor, id int64) error {
	var up Upload
	if err := s.db.WithContext(ctx).Take(&up, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrNotFound
		}
		return err
	}
	own := up.OwnerID != nil && *up.OwnerID == a.ID
	if !own && !a.Directorate && !a.Manager {
		return ErrNotFound
	}
	var used int64
	err := s.db.WithContext(ctx).Raw(`SELECT count(*) FROM documents WHERE blocks::text LIKE ? OR translations::text LIKE ?`, "%"+up.Key+"%", "%"+up.Key+"%").Scan(&used).Error
	if err != nil {
		return err
	}
	if used > 0 {
		return ErrInUse
	}
	if err := s.db.WithContext(ctx).Delete(&Upload{}, id).Error; err != nil {
		return err
	}
	for _, suffix := range []string{".webp", "_thumb.webp", ".ogg", ".mp3"} {
		_ = os.Remove(s.path(up.Key, suffix))
	}
	return nil
}

// Exists — есть ли загрузка с таким ключом (для проверки блоков документа).
func (s *Service) Exists(ctx context.Context, key string) (bool, error) {
	var n int64
	err := s.db.WithContext(ctx).Model(&Upload{}).Where("key = ?", key).Count(&n).Error
	return n > 0, err
}
