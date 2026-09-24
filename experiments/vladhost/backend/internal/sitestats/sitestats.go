// Package sitestats — суточная статистика сайтов. Шлюз считает запросы в памяти и раз в полминуты сохраняет счётчики
// в {dir}/{адрес сайта}/stats.json; панель читает этот файл. В отличие от журнала доступа, который вращается и хранит
// дни, счётчики живут 90 суток и занимают килобайты.
//
// Что считается за сутки (по UTC): запросы, трафик, классы ответов, боты, просмотры страниц, уникальные посетители,
// популярные страницы и источники переходов. IP посетителей не хранятся: для «уникальных» откладывается
// усечённый хеш с солью сайта, и только за текущие сутки — со сменой даты набор хешей удаляется.
package sitestats

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log"
	"net/url"
	"os"
	"path"
	"sort"
	"strings"
	"sync"
	"time"

	"vladhost/internal/sitelog"
)

const (
	// RetentionDays — сколько суток хранятся счётчики.
	RetentionDays = 90

	maxPaths    = 300   // разных страниц за сутки; остальные копятся в OtherKey
	maxRefs     = 100   // разных источников за сутки
	maxSeen     = 10000 // хешей посетителей за сутки; выше — новые уже не различаются
	maxKeyLen   = 200
	OtherKey    = "…"
	fileName    = "stats.json"
	maxFileSize = 8 << 20
)

// Hit — один обработанный запрос.
type Hit struct {
	T       time.Time
	IP      string
	Method  string
	Path    string // без строки запроса
	Referer string
	UA      string
	Host    string // имя, по которому пришёл запрос
	Status  int
	Bytes   int64
}

// Day — счётчики одних суток.
type Day struct {
	Hits     int            `json:"h"`  // все запросы, включая ботов
	Bots     int            `json:"bt"` // из них от ботов и программ
	Pages    int            `json:"pg"` // просмотры страниц людьми
	Visitors int            `json:"v"`  // уникальные посетители-люди
	Bytes    int64          `json:"b"`
	S2       int            `json:"s2"`
	S3       int            `json:"s3"`
	S4       int            `json:"s4"`
	S5       int            `json:"s5"`
	Paths    map[string]int `json:"paths,omitempty"`
	Refs     map[string]int `json:"refs,omitempty"`
}

type siteFile struct {
	Salt    string          `json:"salt"`
	Days    map[string]*Day `json:"days"`
	SeenDay string          `json:"seen_day,omitempty"`
	Seen    []uint64        `json:"seen,omitempty"`
}

type siteState struct {
	f     siteFile
	seen  map[uint64]struct{}
	dirty bool
}

// Aggregator считает запросы всех сайтов. Безопасен для одновременного использования.
type Aggregator struct {
	mu    sync.Mutex
	root  *os.Root
	sites map[string]*siteState
	now   func() time.Time
}

// New открывает каталог статистики (тот же, где лежат журналы).
func New(dir string) (*Aggregator, error) {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	return &Aggregator{root: root, sites: map[string]*siteState{}, now: time.Now}, nil
}

func (a *Aggregator) Close() {
	_ = a.Flush()
	_ = a.root.Close()
}

var botMarks = []string{
	"bot", "crawl", "spider", "slurp", "curl/", "wget", "python-requests", "python-urllib", "go-http-client",
	"headless", "monitor", "uptime", "facebookexternalhit", "preview", "scanner", "httpclient", "okhttp", "libwww", "java/",
}

// IsBot: пустой User-Agent и типичные метки роботов и утилит.
func IsBot(ua string) bool {
	ua = strings.ToLower(ua)
	if ua == "" {
		return true
	}
	for _, m := range botMarks {
		if strings.Contains(ua, m) {
			return true
		}
	}
	return false
}

// isPage: просмотром страницы считается запрос HTML-документа, а не картинки, скрипта или стиля.
func isPage(p string) bool {
	switch strings.ToLower(path.Ext(p)) {
	case "", ".html", ".htm", ".xhtml", ".php":
		return true
	}
	return false
}

// pageKey приводит адрес страницы к виду для рейтинга: /a/index.html и /a/ — одна страница.
func pageKey(p string) string {
	if i := strings.IndexAny(p, "?#"); i >= 0 {
		p = p[:i]
	}
	if p == "" || p[0] != '/' {
		p = "/" + p
	}
	switch strings.ToLower(path.Base(p)) {
	case "index.html", "index.htm":
		p = path.Dir(p)
		if p != "/" {
			p += "/"
		}
	}
	if len(p) > maxKeyLen {
		p = strings.ToValidUTF8(p[:maxKeyLen], "")
	}
	return p
}

// refHost: источник перехода — только имя сайта, без адреса страницы. Свои имена не считаются.
func refHost(ref string, own ...string) string {
	if ref == "" {
		return ""
	}
	u, err := url.Parse(ref)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return ""
	}
	h := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	if h == "" || len(h) > 100 {
		return ""
	}
	for _, o := range own {
		if h == o {
			return ""
		}
	}
	return h
}

func dayKey(t time.Time) string { return t.UTC().Format("2006-01-02") }

func newSalt() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err) // источник случайности недоступен: продолжать небезопасно
	}
	return hex.EncodeToString(b)
}

// load читает счётчики сайта с диска (один раз на запуск); нет файла или он повреждён — начинаем заново.
func (a *Aggregator) load(site string) *siteState {
	st := &siteState{seen: map[uint64]struct{}{}}
	if f, err := a.root.Open(site + "/" + fileName); err == nil {
		data, rerr := io.ReadAll(io.LimitReader(f, maxFileSize))
		_ = f.Close()
		if rerr == nil && json.Unmarshal(data, &st.f) == nil && st.f.Salt != "" && st.f.Days != nil {
			for _, v := range st.f.Seen {
				st.seen[v] = struct{}{}
			}
		} else {
			st.f = siteFile{}
		}
	}
	if st.f.Salt == "" || st.f.Days == nil {
		st.f = siteFile{Salt: newSalt(), Days: map[string]*Day{}}
		st.seen = map[uint64]struct{}{}
	}
	return st
}

func inc(m *map[string]int, key string, limit int) {
	if *m == nil {
		*m = map[string]int{}
	}
	if _, ok := (*m)[key]; !ok && len(*m) >= limit {
		key = OtherKey
	}
	(*m)[key]++
}

// Record учитывает запрос сайта. Неверный адрес сайта игнорируется.
func (a *Aggregator) Record(site string, h Hit) {
	if !sitelog.ValidSite(site) {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	st := a.sites[site]
	if st == nil {
		st = a.load(site)
		a.sites[site] = st
	}
	day := dayKey(h.T)
	d := st.f.Days[day]
	if d == nil {
		d = &Day{}
		st.f.Days[day] = d
	}
	st.dirty = true

	d.Hits++
	d.Bytes += h.Bytes
	switch {
	case h.Status >= 500:
		d.S5++
	case h.Status >= 400:
		d.S4++
	case h.Status >= 300:
		d.S3++
	default:
		d.S2++
	}
	if IsBot(h.UA) {
		d.Bots++
		return
	}

	// Уникальные посетители: хеш с солью сайта и датой, набор только за текущие сутки.
	if st.f.SeenDay != day {
		st.f.SeenDay, st.f.Seen, st.seen = day, nil, map[uint64]struct{}{}
	}
	sum := sha256.Sum256([]byte(st.f.Salt + "|" + day + "|" + h.IP))
	key := binary.BigEndian.Uint64(sum[:8])
	if _, ok := st.seen[key]; !ok && len(st.seen) < maxSeen {
		st.seen[key] = struct{}{}
		st.f.Seen = append(st.f.Seen, key)
		d.Visitors++
	}

	if h.Method == "GET" && h.Status >= 200 && h.Status < 300 && isPage(h.Path) {
		d.Pages++
		inc(&d.Paths, pageKey(h.Path), maxPaths)
		if r := refHost(h.Referer, strings.ToLower(h.Host)); r != "" {
			inc(&d.Refs, r, maxRefs)
		}
	}
}

// Forget забывает сайт (его каталог удалён): иначе следующий Flush создал бы каталог заново.
func (a *Aggregator) Forget(site string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.sites, site)
}

// Flush сохраняет изменившиеся счётчики на диск и удаляет данные старше RetentionDays.
func (a *Aggregator) Flush() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	var first error
	cutoff := dayKey(a.now().AddDate(0, 0, -RetentionDays))
	for site, st := range a.sites {
		for k := range st.f.Days {
			if k < cutoff {
				delete(st.f.Days, k)
				st.dirty = true
			}
		}
		if !st.dirty {
			continue
		}
		if err := a.write(site, st); err != nil {
			if first == nil {
				first = err
			}
			continue
		}
		st.dirty = false
	}
	return first
}

func (a *Aggregator) write(site string, st *siteState) error {
	data, err := json.Marshal(st.f)
	if err != nil {
		return err
	}
	if err := a.root.Mkdir(site, 0o750); err != nil && !errors.Is(err, fs.ErrExist) {
		return err
	}
	tmp := site + "/" + fileName + ".tmp"
	f, err := a.root.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o640)
	if err != nil {
		return err
	}
	_, werr := f.Write(data)
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		_ = a.root.Remove(tmp)
		return werr
	}
	return a.root.Rename(tmp, site+"/"+fileName)
}

// Run сохраняет счётчики каждые every и один раз при остановке (ctx.Done).
func (a *Aggregator) Run(done <-chan struct{}, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-done:
			if err := a.Flush(); err != nil {
				log.Printf("статистика сайтов: %v", err)
			}
			return
		case <-t.C:
			if err := a.Flush(); err != nil {
				log.Printf("статистика сайтов: %v", err)
			}
		}
	}
}

// ---- чтение (панель) ----

// Item — строка рейтинга.
type Item struct {
	Key   string `json:"key"`
	Count int    `json:"count"`
}

// Point — точка графика: одни сутки.
type Point struct {
	Date     string `json:"date"`
	Hits     int    `json:"hits"`
	Pages    int    `json:"pages"`
	Visitors int    `json:"visitors"`
	Bots     int    `json:"bots"`
	Bytes    int64  `json:"bytes"`
	S2       int    `json:"s2"`
	S3       int    `json:"s3"`
	S4       int    `json:"s4"`
	S5       int    `json:"s5"`
}

// Summary — статистика за период.
type Summary struct {
	Days     []Point `json:"days"`
	Total    Point   `json:"total"`
	TopPages []Item  `json:"top_pages"`
	TopRefs  []Item  `json:"top_refs"`
}

// ValidPeriod: допустимая длина периода в сутках.
func ValidPeriod(days int) bool { return days == 7 || days == 30 || days == RetentionDays }

const topN = 10

func top(m map[string]int) []Item {
	items := make([]Item, 0, len(m))
	for k, v := range m {
		items = append(items, Item{k, v})
	}
	sort.Slice(items, func(i, j int) bool {
		// «Прочее» всегда в конце, остальное — по убыванию, при равенстве по имени (стабильный вывод).
		if (items[i].Key == OtherKey) != (items[j].Key == OtherKey) {
			return items[j].Key == OtherKey
		}
		if items[i].Count != items[j].Count {
			return items[i].Count > items[j].Count
		}
		return items[i].Key < items[j].Key
	})
	if len(items) > topN {
		items = items[:topN]
	}
	return items
}

// Read собирает статистику сайта за последние days суток (включая сегодня). Сутки раньше since (создание сайта)
// пропускаются: адрес мог принадлежать прежнему сайту. Нет файла — нулевая статистика, не ошибка.
func Read(dir, site string, days int, since, now time.Time) (Summary, error) {
	sum := Summary{Days: make([]Point, 0, days), TopPages: []Item{}, TopRefs: []Item{}}
	if !sitelog.ValidSite(site) {
		return sum, errors.New("sitestats: недопустимый адрес сайта")
	}
	var f siteFile
	if root, err := os.OpenRoot(dir); err == nil {
		defer func() { _ = root.Close() }()
		if fh, err := root.Open(site + "/" + fileName); err == nil {
			data, rerr := io.ReadAll(io.LimitReader(fh, maxFileSize))
			_ = fh.Close()
			if rerr != nil {
				return sum, rerr
			}
			if err := json.Unmarshal(data, &f); err != nil {
				return sum, err
			}
		}
	}
	first := dayKey(since)
	pages, refs := map[string]int{}, map[string]int{}
	for i := days - 1; i >= 0; i-- {
		date := dayKey(now.AddDate(0, 0, -i))
		p := Point{Date: date}
		if d := f.Days[date]; d != nil && date >= first {
			p = Point{date, d.Hits, d.Pages, d.Visitors, d.Bots, d.Bytes, d.S2, d.S3, d.S4, d.S5}
			for k, v := range d.Paths {
				pages[k] += v
			}
			for k, v := range d.Refs {
				refs[k] += v
			}
		}
		sum.Days = append(sum.Days, p)
		sum.Total.Hits += p.Hits
		sum.Total.Pages += p.Pages
		sum.Total.Visitors += p.Visitors
		sum.Total.Bots += p.Bots
		sum.Total.Bytes += p.Bytes
		sum.Total.S2 += p.S2
		sum.Total.S3 += p.S3
		sum.Total.S4 += p.S4
		sum.Total.S5 += p.S5
	}
	sum.TopPages, sum.TopRefs = top(pages), top(refs)
	return sum, nil
}
