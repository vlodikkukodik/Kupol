// Package sitelog — журналы сайтов: доступ (каждый запрос) и ошибки (404, 403 и прочие отказы).
// Пишет веб-шлюз, читает панель. Файлы лежат вне каталога сайта ({dir}/{адрес сайта}/access.log и error.log),
// поэтому пользователь не может их подменить или удалить, а шлюз остаётся без записи в каталог сайтов.
// Формат — одна JSON-строка на событие; файлы вращаются по размеру, старые копии удаляются.
package sitelog

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	KindAccess = "access"
	KindError  = "error"

	// MaxFileSize — размер, после которого файл вращается. Вместе с Keep даёт потолок на диске:
	// (Keep+1)·MaxFileSize на сайт и вид журнала.
	MaxFileSize = 4 << 20
	Keep        = 3

	maxOpen   = 128 // одновременно открытых файлов; при переполнении закрываются все (следующая запись откроет нужный)
	maxPath   = 1024
	maxRef    = 512
	maxUA     = 256
	maxDetail = 512
)

// Entry — событие журнала. Для доступа заполнены Method/Status/Bytes/Ms, для ошибок — Code/Detail.
type Entry struct {
	T       time.Time `json:"t"`
	IP      string    `json:"ip"`
	Host    string    `json:"host"` // имя, по которому пришёл запрос: адрес сайта или свой домен
	Method  string    `json:"m,omitempty"`
	Path    string    `json:"p"`
	Status  int       `json:"s,omitempty"`
	Bytes   int64     `json:"b,omitempty"`
	Ms      int64     `json:"ms,omitempty"`
	Referer string    `json:"ref,omitempty"`
	UA      string    `json:"ua,omitempty"`
	Code    string    `json:"code,omitempty"`
	Detail  string    `json:"d,omitempty"`
}

// siteRe: имя каталога журнала — адрес сайта. Строгий шаблон, никаких «..» и слэшей.
var siteRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9.-]{0,251}[a-z0-9])?$`)

// ValidSite сообщает, годится ли строка как адрес сайта для имени каталога журналов (без «..», слэшей и заглавных).
func ValidSite(s string) bool { return validSite(s) }

func validSite(s string) bool { return siteRe.MatchString(s) && !strings.Contains(s, "..") }

func validKind(k string) bool { return k == KindAccess || k == KindError }

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return strings.ToValidUTF8(s[:n], "")
}

func (e Entry) clipped() Entry {
	e.Path = clip(e.Path, maxPath)
	e.Referer = clip(e.Referer, maxRef)
	e.UA = clip(e.UA, maxUA)
	e.Detail = clip(e.Detail, maxDetail)
	return e
}

// Text — строка для скачивания: обычный текст, который читается и без панели.
func (e Entry) Text(kind string) string {
	ts := e.T.UTC().Format(time.RFC3339)
	if kind == KindError {
		return fmt.Sprintf("%s [%s] %s %s %s %s", ts, e.Code, e.IP, e.Host, strconv.Quote(e.Path), strconv.Quote(e.Detail))
	}
	return fmt.Sprintf("%s %s [%s] %s %s %d %d %s %s %dms", e.IP, e.Host, ts, e.Method, strconv.Quote(e.Path), e.Status, e.Bytes,
		strconv.Quote(e.Referer), strconv.Quote(e.UA), e.Ms)
}

// ---- запись ----

type openFile struct {
	f    *os.File
	size int64
}

// Writer пишет журналы сайтов. Безопасен для одновременного использования.
type Writer struct {
	mu      sync.Mutex
	root    *os.Root
	files   map[string]*openFile // «сайт/вид» → открытый файл
	max     int64
	lastErr time.Time
}

// NewWriter открывает (создавая при необходимости) каталог журналов.
func NewWriter(dir string) (*Writer, error) {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	return &Writer{root: root, files: map[string]*openFile{}, max: MaxFileSize}, nil
}

// SetMaxSize меняет порог вращения (нужен тестам).
func (w *Writer) SetMaxSize(n int64) { w.max = n }

func (w *Writer) Close() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.closeAll()
	_ = w.root.Close()
}

func (w *Writer) closeAll() {
	for k, of := range w.files {
		_ = of.f.Close()
		delete(w.files, k)
	}
}

// Write добавляет событие в журнал сайта. Ошибки записи не должны ломать отдачу сайта:
// они только попадают в журнал службы (не чаще раза в минуту).
func (w *Writer) Write(site, kind string, e Entry) {
	if !validSite(site) || !validKind(kind) {
		return
	}
	line, err := json.Marshal(e.clipped())
	if err != nil {
		return
	}
	line = append(line, '\n')
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := w.write(site, kind, line); err != nil && time.Since(w.lastErr) > time.Minute {
		w.lastErr = time.Now()
		log.Printf("журнал сайта %s (%s): %v", site, kind, err)
	}
}

func (w *Writer) write(site, kind string, line []byte) error {
	key := site + "/" + kind
	of := w.files[key]
	if of == nil {
		if len(w.files) >= maxOpen {
			w.closeAll()
		}
		if err := w.root.Mkdir(site, 0o750); err != nil && !errors.Is(err, fs.ErrExist) {
			return err
		}
		f, err := w.root.OpenFile(site+"/"+kind+".log", os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o640)
		if err != nil {
			return err
		}
		fi, err := f.Stat()
		if err != nil {
			_ = f.Close()
			return err
		}
		of = &openFile{f: f, size: fi.Size()}
		w.files[key] = of
	}
	n, err := of.f.Write(line)
	of.size += int64(n)
	if err != nil {
		_ = of.f.Close()
		delete(w.files, key)
		return err
	}
	if of.size >= w.max {
		w.rotate(site, kind)
	}
	return nil
}

// rotate: access.log → access.log.1 → … → access.log.{Keep}, самый старый удаляется.
func (w *Writer) rotate(site, kind string) {
	key := site + "/" + kind
	if of := w.files[key]; of != nil {
		_ = of.f.Close()
		delete(w.files, key)
	}
	base := site + "/" + kind + ".log"
	_ = w.root.Remove(base + "." + strconv.Itoa(Keep))
	for i := Keep - 1; i >= 1; i-- {
		_ = w.root.Rename(base+"."+strconv.Itoa(i), base+"."+strconv.Itoa(i+1))
	}
	_ = w.root.Rename(base, base+".1")
}

// Sweep удаляет журналы сайтов, которых больше нет (keep вернул false). Возвращает адреса удалённых сайтов.
func (w *Writer) Sweep(keep func(site string) bool) []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	d, err := w.root.Open(".")
	if err != nil {
		return nil
	}
	entries, err := d.ReadDir(-1)
	_ = d.Close()
	if err != nil {
		return nil
	}
	var removed []string
	for _, ent := range entries {
		name := ent.Name()
		if !ent.IsDir() || !validSite(name) || keep(name) {
			continue
		}
		for _, kind := range []string{KindAccess, KindError} {
			if of := w.files[name+"/"+kind]; of != nil {
				_ = of.f.Close()
				delete(w.files, name+"/"+kind)
			}
		}
		if w.root.RemoveAll(name) == nil {
			removed = append(removed, name)
		}
	}
	return removed
}

// ---- чтение ----

// Query — что показать из журнала.
type Query struct {
	Kind   string
	Status string    // только для доступа: "2xx", "3xx", "4xx", "5xx" или пусто (любой)
	Text   string    // подстрока без учёта регистра в адресе, IP, браузере, источнике, коде и подробностях
	Limit  int       // 1..MaxLimit
	Before time.Time // только события строго раньше (постраничная выдача); нулевое — с самого нового
	Since  time.Time // только события не раньше (сайт мог быть создан заново с тем же адресом)
}

const MaxLimit = 500

// Page — страница журнала, новые события сверху.
type Page struct {
	Entries []Entry `json:"entries"`
	HasMore bool    `json:"has_more"`
}

var statusClasses = map[string][2]int{"2xx": {200, 299}, "3xx": {300, 399}, "4xx": {400, 499}, "5xx": {500, 599}}

// ValidStatus сообщает, допустим ли фильтр по классу ответа.
func ValidStatus(s string) bool {
	if s == "" {
		return true
	}
	_, ok := statusClasses[s]
	return ok
}

func (q Query) match(e Entry) bool {
	if !q.Since.IsZero() && e.T.Before(q.Since) {
		return false
	}
	if !q.Before.IsZero() && !e.T.Before(q.Before) {
		return false
	}
	if r, ok := statusClasses[q.Status]; ok && q.Kind == KindAccess && (e.Status < r[0] || e.Status > r[1]) {
		return false
	}
	if q.Text != "" {
		hay := strings.ToLower(strings.Join([]string{e.Path, e.IP, e.Host, e.UA, e.Referer, e.Code, e.Detail}, "\n"))
		if !strings.Contains(hay, strings.ToLower(q.Text)) {
			return false
		}
	}
	return true
}

// fileNames — файлы журнала от нового к старому.
func fileNames(site, kind string) []string {
	base := site + "/" + kind + ".log"
	names := []string{base}
	for i := 1; i <= Keep; i++ {
		names = append(names, base+"."+strconv.Itoa(i))
	}
	return names
}

func readLines(root *os.Root, name string) ([][]byte, error) {
	f, err := root.Open(name)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, MaxFileSize*2)) // файл вращается на MaxFileSize; двойной запас на случай сбоя вращения
	if err != nil {
		return nil, err
	}
	return bytes.Split(bytes.TrimRight(data, "\n"), []byte("\n")), nil
}

func openLogs(dir, site, kind string) (*os.Root, error) {
	if !validSite(site) || !validKind(kind) {
		return nil, errors.New("sitelog: недопустимое имя")
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	return root, nil
}

// Read возвращает страницу журнала сайта. Нет каталога или файла — пустая страница, не ошибка.
func Read(dir, site string, q Query) (Page, error) {
	page := Page{Entries: []Entry{}}
	root, err := openLogs(dir, site, q.Kind)
	if err != nil || root == nil {
		return page, err
	}
	defer func() { _ = root.Close() }()
	if q.Limit < 1 || q.Limit > MaxLimit {
		q.Limit = 100
	}
	for _, name := range fileNames(site, q.Kind) {
		lines, err := readLines(root, name)
		if err != nil {
			return page, err
		}
		for _, line := range slices.Backward(lines) {
			var e Entry
			if len(line) == 0 || json.Unmarshal(line, &e) != nil {
				continue // оборванная запись при сбое: пропускаем
			}
			if !q.match(e) {
				continue
			}
			if len(page.Entries) == q.Limit {
				page.HasMore = true
				return page, nil
			}
			page.Entries = append(page.Entries, e)
		}
	}
	return page, nil
}

// WriteText выдаёт журнал целиком (от старых событий к новым) обычным текстом, для скачивания.
func WriteText(dir, site, kind string, since time.Time, w io.Writer) error {
	root, err := openLogs(dir, site, kind)
	if err != nil || root == nil {
		return err
	}
	defer func() { _ = root.Close() }()
	out := bufio.NewWriter(w)
	names := fileNames(site, kind)
	for _, name := range slices.Backward(names) {
		lines, err := readLines(root, name)
		if err != nil {
			return err
		}
		for _, l := range lines {
			var e Entry
			if len(l) == 0 || json.Unmarshal(l, &e) != nil || (!since.IsZero() && e.T.Before(since)) {
				continue
			}
			if _, err := out.WriteString(e.Text(kind) + "\n"); err != nil {
				return err
			}
		}
	}
	return out.Flush()
}
