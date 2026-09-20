package documents

import (
	"context"
	"strings"
	"unicode"

	"gorm.io/gorm"
)

// Построение индекса поиска (таблица document_search, миграция 0011). Запросы к индексу — в search.go.
//
// Главное правило то же, что у чтения документов: закрытого от читателя в результатах поиска не существует. Поэтому каждая
// строка индекса несёт свой уровень, а текст блока раскладывается по уровням его фрагментов: открытые фрагменты абзаца и
// закрытый фрагмент внутри него — разные строки. Что включать в индекс, решает явный список ниже (allow-list): новый вид
// блока сначала не индексируется вовсе, пока для него не написано извлечение (это проверяет тест).

// searchIndexVersion — версия правил построения производных данных (индекс поиска и обратные ссылки). При смене (другие
// поля, другая разбивка) сервер сам перестраивает их при старте (EnsureSearchIndex). 1 — индекс поиска, 2 — и обратные ссылки.
const searchIndexVersion = 2

type searchKind string

const (
	searchTitle searchKind = "title"
	searchMeta  searchKind = "meta"
	searchBlock searchKind = "block"
)

// searchRow — строка индекса. Колонка tsv вычисляется базой из body, поэтому в структуре её нет.
type searchRow struct {
	ID         int64 `gorm:"primaryKey"`
	DocumentID int64
	Kind       string
	Ord        int
	BlockID    string
	Level      int
	Body       string
	Norm       string // Body в нижнем регистре: по нему строится tsvector (см. миграцию 0011)
}

func (searchRow) TableName() string { return "document_search" }

// lowerText — нижний регистр посимвольно: число знаков не меняется, поэтому позиции в Norm и в Body совпадают
// (на этом держится возврат регистра в сниппете, restoreCase).
func lowerText(s string) string { return strings.Map(unicode.ToLower, s) }

func newRow(docID int64, kind searchKind, ord int, blockID string, level int, body string) searchRow {
	body = cleanForIndex(body)
	return searchRow{DocumentID: docID, Kind: string(kind), Ord: ord, BlockID: blockID, Level: level, Body: body, Norm: lowerText(body)}
}

// textPieces собирает текст блока по уровням: level → текст.
type textPieces struct {
	blockLevel int
	byLevel    map[int]*strings.Builder
}

func newPieces(blockLevel int) *textPieces {
	return &textPieces{blockLevel: blockLevel, byLevel: map[int]*strings.Builder{}}
}

func (t *textPieces) builder(level int) *strings.Builder {
	if level < t.blockLevel {
		level = t.blockLevel
	}
	b := t.byLevel[level]
	if b == nil {
		b = &strings.Builder{}
		t.byLevel[level] = b
	}
	return b
}

// plain — строка без уровней фрагментов: она открыта настолько, насколько открыт блок.
func (t *textPieces) plain(s string) {
	s = cleanForIndex(s)
	if strings.TrimSpace(s) == "" {
		return
	}
	b := t.builder(0)
	if b.Len() > 0 {
		b.WriteByte('\n')
	}
	b.WriteString(s)
}

func (t *textPieces) plainAll(items ...string) {
	for _, s := range items {
		t.plain(s)
	}
}

// rich — форматированный текст: у каждого фрагмента свой уровень. Соседние фрагменты одного уровня склеиваются (слово
// может быть разбито выделением: «жир» + «ный»); если между ними был фрагмент другого уровня, ставится пробел, чтобы
// слова по обе стороны закрытого фрагмента не слились и не образовали ложную фразу.
func (t *textPieces) rich(r Rich) {
	first := map[int]bool{}
	last := -1
	for _, run := range r {
		// Пробелы фрагмента сохраняются как есть: соседние фрагменты одного уровня («Обычный » + «жирный») склеиваются
		// без потери пробела между словами. Лишние пробелы уберёт cleanForIndex при сборке строки.
		text := stripControl(run.Text)
		if text == "" {
			continue
		}
		level := run.Level
		if level < t.blockLevel {
			level = t.blockLevel
		}
		b := t.builder(level)
		if !first[level] && b.Len() > 0 {
			b.WriteByte('\n') // новое поле в тот же уровень — отдельная строка текста
		} else if last != level && b.Len() > 0 {
			b.WriteByte(' ')
		}
		first[level] = true
		last = level
		b.WriteString(text)
	}
}

func (t *textPieces) richAll(items []Rich) {
	for _, r := range items {
		t.rich(r)
	}
}

// stripControl заменяет управляющие знаки (среди них — служебные метки подсветки сниппета) пробелами.
func stripControl(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)
}

// cleanForIndex — stripControl и схлопывание пробелов.
func cleanForIndex(s string) string {
	return strings.Join(strings.Fields(stripControl(s)), " ")
}

// searchExcluded — виды блоков, которые в индекс не попадают, и почему.
var searchExcluded = map[string]string{
	"dossier_header": "нет собственного текста: строится из свойств документа",
	"divider":        "нет текста",
	"page":           "только номер страницы",
	// Ссылка раскрывает читателю шифр и примечание только если цель ему доступна; в индексе доступность не проверить,
	// поэтому ссылки в поиск не идут (найти документ можно по его собственному названию и шифру).
	"doc_link": "шифр и примечание видны читателю только при доступной цели",
}

// blockPieces извлекает текст блока. false — вид блока не индексируется (см. searchExcluded) либо неизвестен.
func blockPieces(b Block) (*textPieces, bool) {
	bl := 0
	if b.Level != nil {
		bl = *b.Level
	}
	t := newPieces(bl)
	switch b.Type {
	case "heading":
		t.plain(decodeData[headingData](b.Data).Text)
	case "paragraph":
		t.rich(decodeData[paragraphData](b.Data).Text)
	case "list":
		t.richAll(decodeData[listData](b.Data).Items)
	case "quote":
		d := decodeData[quoteData](b.Data)
		t.rich(d.Text)
		t.plain(d.Source)
	case "experiment_log":
		d := decodeData[experimentLogData](b.Data)
		t.plain(d.Title)
		for _, e := range d.Entries {
			t.plainAll(e.Participants...)
			t.rich(e.Text)
		}
	case "stamp":
		t.plain(decodeData[stampData](b.Data).Text)
	case "memo":
		d := decodeData[memoData](b.Data)
		t.plainAll(d.Number, d.From, d.Subject, d.Signature)
		t.plainAll(d.To...)
		t.richAll(d.Body)
	case "clipping":
		d := decodeData[clippingData](b.Data)
		t.plainAll(d.Title, d.Source)
		t.richAll(d.Paragraphs)
		for _, l := range d.Lines {
			t.plain(l.Speaker)
			t.rich(l.Text)
		}
	case "table":
		d := decodeData[tableData](b.Data)
		t.plain(d.Caption)
		t.plainAll(d.Columns...)
		for _, row := range d.Rows {
			t.richAll(row)
		}
	case "footnote":
		t.rich(decodeData[footnoteData](b.Data).Text)
	case "appendix":
		t.plain(decodeData[appendixData](b.Data).Title)
	default:
		return nil, false
	}
	return t, true
}

// searchRows строит строки индекса документа.
func searchRows(d *Document) []searchRow {
	var rows []searchRow
	title := d.Title
	if d.Code != nil {
		title += " " + *d.Code
	}
	if cleanForIndex(title) != "" {
		rows = append(rows, newRow(d.ID, searchTitle, 0, "", 0, title))
	}
	if d.DiscoveryPlace != nil && cleanForIndex(*d.DiscoveryPlace) != "" {
		rows = append(rows, newRow(d.ID, searchMeta, 0, "", 0, *d.DiscoveryPlace))
	}
	for i, b := range d.Blocks {
		t, ok := blockPieces(b)
		if !ok {
			continue
		}
		for level := 0; level <= MaxLevel; level++ {
			if sb := t.byLevel[level]; sb != nil && cleanForIndex(sb.String()) != "" {
				rows = append(rows, newRow(d.ID, searchBlock, i+1, b.ID, level, sb.String()))
			}
		}
	}
	return rows
}

// linkRow — обратная ссылка (таблица document_links, миграция 0012).
type linkRow struct {
	ID         int64 `gorm:"primaryKey"`
	SourceID   int64
	BlockID    string
	Level      int
	TargetCode string
}

func (linkRow) TableName() string { return "document_links" }

// linkRows — ссылки документа на другие документы: по строке на пару «блок, цель». Ссылка на самого себя не считается.
func linkRows(d *Document) []linkRow {
	var rows []linkRow
	seen := map[string]bool{}
	for _, b := range d.Blocks {
		spec, ok := kinds[b.Type]
		if !ok || spec.links == nil {
			continue
		}
		v, err := spec.decode(b.Data)
		if err != nil {
			continue // блок, который не разобрать, ссылок не даёт; чтение такого документа всё равно скажет об ошибке
		}
		level := 0
		if b.Level != nil {
			level = *b.Level
		}
		for _, code := range spec.links(v) {
			if code == "" || (d.Code != nil && code == *d.Code) || seen[b.ID+"\x00"+code] {
				continue
			}
			seen[b.ID+"\x00"+code] = true
			rows = append(rows, linkRow{SourceID: d.ID, BlockID: b.ID, Level: level, TargetCode: code})
		}
	}
	return rows
}

// reindexDocument заменяет производные от текста данные документа: строки индекса поиска и обратные ссылки. Вызывается
// в транзакции, где документ записан, — они не отстают от текста.
func reindexDocument(tx *gorm.DB, d *Document) error {
	if err := tx.Exec("DELETE FROM document_search WHERE document_id = ?", d.ID).Error; err != nil {
		return err
	}
	if err := tx.Exec("DELETE FROM document_links WHERE source_id = ?", d.ID).Error; err != nil {
		return err
	}
	if rows := searchRows(d); len(rows) > 0 {
		if err := tx.CreateInBatches(&rows, 200).Error; err != nil {
			return err
		}
	}
	if links := linkRows(d); len(links) > 0 {
		return tx.CreateInBatches(&links, 200).Error
	}
	return nil
}

// searchLock — ключ advisory-блокировки перестройки индекса: два сервера при старте не строят его одновременно.
const searchLock = 7301

// EnsureSearchIndex перестраивает индекс, если он построен по другим правилам (или не построен: документы, появившиеся до
// поиска). Вызывается при старте сервера; при совпадении версии ничего не делает.
func (s *Service) EnsureSearchIndex(ctx context.Context) error {
	var v int
	err := s.db.WithContext(ctx).Raw("SELECT COALESCE((SELECT version FROM document_search_state), 0)").Scan(&v).Error
	if err != nil {
		return err
	}
	if v == searchIndexVersion {
		return nil
	}
	return s.rebuildSearchIndex(ctx, false)
}

// ReindexAll принудительно перестраивает индекс (команда `kupol doc reindex`); возвращает число документов.
func (s *Service) ReindexAll(ctx context.Context) (int, error) {
	n := 0
	err := s.rebuild(ctx, true, &n)
	return n, err
}

func (s *Service) rebuildSearchIndex(ctx context.Context, force bool) error {
	n := 0
	if err := s.rebuild(ctx, force, &n); err != nil {
		return err
	}
	s.log.Info("индекс поиска перестроен", "documents", n, "version", searchIndexVersion)
	return nil
}

func (s *Service) rebuild(ctx context.Context, force bool, count *int) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(?)", int64(searchLock)).Error; err != nil {
			return err
		}
		if !force {
			var v int
			if err := tx.Raw("SELECT COALESCE((SELECT version FROM document_search_state), 0)").Scan(&v).Error; err != nil {
				return err
			}
			if v == searchIndexVersion {
				return nil // параллельный запуск успел раньше
			}
		}
		if err := tx.Exec("DELETE FROM document_search").Error; err != nil {
			return err
		}
		if err := tx.Exec("DELETE FROM document_links").Error; err != nil {
			return err
		}
		var after int64
		for {
			var docs []Document
			if err := tx.Where("id > ?", after).Order("id").Limit(100).Find(&docs).Error; err != nil {
				return err
			}
			if len(docs) == 0 {
				break
			}
			for i := range docs {
				if err := reindexDocument(tx, &docs[i]); err != nil {
					return err
				}
				*count++
			}
			after = docs[len(docs)-1].ID
		}
		return tx.Exec(`INSERT INTO document_search_state (singleton, version) VALUES (true, ?)
			ON CONFLICT (singleton) DO UPDATE SET version = EXCLUDED.version`, searchIndexVersion).Error
	})
}
