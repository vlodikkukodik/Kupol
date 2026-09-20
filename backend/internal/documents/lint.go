package documents

import (
	"bytes"
	"context"
	"fmt"
	"sort"

	"gorm.io/gorm"
)

// Линтер канона (team panel, шаг 3.5): проверяет сохранённый документ перед отправкой на проверку и перед публикацией.
//
// Ошибки не дают отправить документ на проверку и опубликовать его; предупреждения — только подсказка: автор решает сам.
// Форму содержимого (обязательные поля, длины, допустимые значения) проверяет сервер при сохранении, здесь — то, что видно
// лишь в целом: ссылки на другие документы, шапка досье, повторы. Уникальность шифра и номера О-№ обеспечивает сама база
// (уникальные индексы), а номер объекта присваивается при публикации, поэтому «дублей номеров» линтер не ищет.

// Тяжесть замечания линтера.
const (
	LintError   = "error"
	LintWarning = "warning"
)

// Коды замечаний (для тестов и интерфейса).
const (
	LintNoBlocks          = "no_blocks"
	LintBrokenLink        = "broken_link"
	LintUnpublishedLink   = "unpublished_link"
	LintSelfLink          = "self_link"
	LintNoDossierHeader   = "no_dossier_header"
	LintDuplicateBlock    = "duplicate_block"
	LintUnknownDepartment = "unknown_department"
)

// LintIssue — одно замечание. BlockID — блок, к которому оно относится (пусто — к документу в целом).
type LintIssue struct {
	Severity string `json:"severity" tstype:"'error' | 'warning'"`
	Code     string `json:"code"`
	Message  string `json:"message"`
	BlockID  string `json:"block_id,omitempty"`
}

// LintReport — итог проверки. Замечания упорядочены: сначала ошибки, затем предупреждения, внутри — по порядку блоков.
type LintReport struct {
	Issues   []LintIssue `json:"issues"`
	Errors   int         `json:"errors"`
	Warnings int         `json:"warnings"`
}

// HasErrors — есть ли замечания, которые не дают двигать документ дальше.
func (r *LintReport) HasErrors() bool { return r.Errors > 0 }

// Summary — коротко о замечаниях-ошибках (для сообщения об отказе).
func (r *LintReport) Summary() string {
	msgs := []string{}
	for _, i := range r.Issues {
		if i.Severity == LintError {
			msgs = append(msgs, i.Message)
		}
	}
	switch len(msgs) {
	case 0:
		return ""
	case 1:
		return msgs[0]
	default:
		return fmt.Sprintf("%s (и ещё %d)", msgs[0], len(msgs)-1)
	}
}

// LintFailedError — документ не прошёл проверку канона (в отчёте есть ошибки).
type LintFailedError struct{ Report *LintReport }

func (e *LintFailedError) Error() string {
	return "documents: документ не прошёл проверку канона: " + e.Report.Summary()
}

// lintDocument проверяет документ d. db — соединение или транзакция, в которой читаются цели ссылок.
func lintDocument(db *gorm.DB, d *Document) (*LintReport, error) {
	rep := &LintReport{Issues: []LintIssue{}}
	add := func(sev, code, blockID, format string, args ...any) {
		rep.Issues = append(rep.Issues, LintIssue{Severity: sev, Code: code, BlockID: blockID, Message: fmt.Sprintf(format, args...)})
	}

	if len(d.Blocks) == 0 {
		add(LintError, LintNoBlocks, "", "В документе нет ни одного блока: публиковать нечего")
	}

	// ссылки: какие документы существуют и в каком они статусе
	type link struct{ blockID, code string }
	var links []link
	codes := map[string]bool{}
	for _, b := range d.Blocks {
		if b.Type != "doc_link" {
			continue
		}
		v, err := kinds["doc_link"].decode(b.Data)
		if err != nil {
			return nil, fmt.Errorf("блок %s: %w", b.ID, err)
		}
		code := v.(*docLinkData).Code
		links = append(links, link{b.ID, code})
		codes[code] = true
	}
	if d.Department != nil && *d.Department != "" {
		codes[*d.Department] = true
	}
	status := map[string]string{}
	if len(codes) > 0 {
		list := make([]string, 0, len(codes))
		for c := range codes {
			list = append(list, c)
		}
		var rows []struct{ Code, Status string }
		if err := db.Model(&Document{}).Select("code, status").Where("code IN ?", list).Scan(&rows).Error; err != nil {
			return nil, err
		}
		for _, r := range rows {
			status[r.Code] = r.Status
		}
	}
	for _, l := range links {
		st, ok := status[l.code]
		switch {
		case d.Code != nil && l.code == *d.Code:
			add(LintWarning, LintSelfLink, l.blockID, "Ссылка на сам документ %s", l.code)
		case !ok:
			add(LintError, LintBrokenLink, l.blockID, "Ссылка на %s: такого документа нет в архиве", l.code)
		case st != string(StatusPublished):
			add(LintWarning, LintUnpublishedLink, l.blockID, "Ссылка на %s: документ ещё не опубликован, читатели увидят её как недоступную", l.code)
		}
	}
	if d.Department != nil && *d.Department != "" {
		if _, ok := status[*d.Department]; !ok {
			add(LintWarning, LintUnknownDepartment, "", "Отдел %s: такого документа нет в архиве", *d.Department)
		}
	}

	// шапка досье: у объекта её ждёт читатель
	if Type(d.Type) == TypeObject && len(d.Blocks) > 0 {
		has := false
		for _, b := range d.Blocks {
			if b.Type == "dossier_header" {
				has = true
			}
		}
		if !has {
			add(LintWarning, LintNoDossierHeader, "", "У объекта нет блока «Шапка досье»: свойства (класс опасности, отдел…) читатель не увидит")
		}
	}

	// подряд идущие одинаковые блоки — почти всегда случайная вставка дважды
	for i := 1; i < len(d.Blocks); i++ {
		a, b := d.Blocks[i-1], d.Blocks[i]
		if a.Type == b.Type && a.Type != "divider" && a.Type != "page" && a.Type != "dossier_header" &&
			bytes.Equal(a.Data, b.Data) && equalLevel(a.Level, b.Level) {
			add(LintWarning, LintDuplicateBlock, b.ID, "Блок %d повторяет предыдущий", i+1)
		}
	}

	sort.SliceStable(rep.Issues, func(i, j int) bool { return rep.Issues[i].Severity == LintError && rep.Issues[j].Severity != LintError })
	for _, i := range rep.Issues {
		if i.Severity == LintError {
			rep.Errors++
		} else {
			rep.Warnings++
		}
	}
	return rep, nil
}

func equalLevel(a, b *int) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// TeamLint проверяет сохранённый документ. Видеть замечания вправе тот, кто видит документ в team panel.
func (s *Service) TeamLint(ctx context.Context, a Actor, id int64) (*LintReport, error) {
	d, err := s.viewable(ctx, s.db, a, id)
	if err != nil {
		return nil, err
	}
	return lintDocument(s.db.WithContext(ctx), d)
}
