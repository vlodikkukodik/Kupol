package documents

import (
	"context"
	"time"

	"gorm.io/gorm"
)

// Рабочий стол команды (team panel, шаг 3.6): что ждёт человека прямо сейчас.
//
// Автору — его документы по статусам, возвращённые на доработку (с причиной и числом открытых замечаний), черновики и то,
// что ждёт проверки. Рецензенту (право «проверять» и Директорату) — ещё и очередь на проверку: чужие документы, отправленные
// раньше — выше. Статистики просмотров и оценок здесь нет: она появится вместе с читательскими отметками (этап 4).

// Сколько документов показывается в каждом списке рабочего стола (остальное — в общем списке документов).
const (
	dashboardListLimit  = 8
	dashboardQueueLimit = 20
)

// DashboardItem — документ в списках рабочего стола.
type DashboardItem struct {
	ID        int64     `json:"id"`
	Code      *string   `json:"code,omitempty"`
	Type      string    `json:"type"`
	TypeName  string    `json:"type_name"`
	Title     string    `json:"title"`
	Status    string    `json:"status"`
	Revision  int       `json:"revision"`
	Author    *string   `json:"author,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
	// SubmittedAt — когда документ отправили на проверку (последний раз); у документов «на проверке».
	SubmittedAt *time.Time `json:"submitted_at,omitempty"`
	// OpenComments — комментарии рецензента, ещё не отмеченные исправленными.
	OpenComments int `json:"open_comments"`
	// ReturnNote и ReturnedBy — причина и рецензент, если документ вернули на доработку и автор его ещё не отправил заново.
	ReturnNote string  `json:"return_note,omitempty"`
	ReturnedBy *string `json:"returned_by,omitempty"`
}

// DashboardCounts — сколько у человека документов в каждом статусе.
type DashboardCounts struct {
	Draft     int `json:"draft"`
	Review    int `json:"review"`
	Published int `json:"published"`
	Archived  int `json:"archived"`
}

// Dashboard — рабочий стол человека.
type Dashboard struct {
	Counts DashboardCounts `json:"counts"`
	// Returned — мои черновики, которые вернули на доработку; Drafts — остальные мои черновики; InReview — мои документы на проверке.
	Returned []DashboardItem `json:"returned"`
	Drafts   []DashboardItem `json:"drafts"`
	InReview []DashboardItem `json:"in_review"`
	// Queue — очередь на проверку (только рецензентам): чужие документы, давно ждущие — первыми; QueueTotal — сколько всего.
	Queue      []DashboardItem `json:"queue"`
	QueueTotal int             `json:"queue_total"`
	CanReview  bool            `json:"can_review"`
	CanWrite   bool            `json:"can_write"`
}

type dashRow struct {
	Document
	AuthorLogin *string
	SubmittedAt *time.Time
}

// TeamDashboard собирает рабочий стол человека. Видны только документы, которые он вправе видеть (те же правила, что у списка).
func (s *Service) TeamDashboard(ctx context.Context, a Actor) (*Dashboard, error) {
	db := s.db.WithContext(ctx)
	out := &Dashboard{Returned: []DashboardItem{}, Drafts: []DashboardItem{}, InReview: []DashboardItem{}, Queue: []DashboardItem{}, CanWrite: a.CanWrite || a.Directorate}
	out.CanReview = a.Directorate || a.CanReview

	// счётчики моих документов
	var counts []struct {
		Status string
		N      int
	}
	if err := db.Model(&Document{}).Select("status, count(*) AS n").Where("author_id = ?", a.UserID).Group("status").Scan(&counts).Error; err != nil {
		return nil, err
	}
	for _, c := range counts {
		switch Status(c.Status) {
		case StatusDraft:
			out.Counts.Draft = c.N
		case StatusReview:
			out.Counts.Review = c.N
		case StatusPublished:
			out.Counts.Published = c.N
		case StatusArchived:
			out.Counts.Archived = c.N
		}
	}

	// мои черновики и документы на проверке: возвращённые отделяются от прочих черновиков по последнему событию рецензии
	mine, err := s.dashRows(db.Table("documents d").Where("d.author_id = ? AND d.status IN ?", a.UserID, []string{string(StatusDraft), string(StatusReview)}).Order("d.updated_at DESC, d.id DESC").Limit(200))
	if err != nil {
		return nil, err
	}
	items, err := s.dashItems(db, a, mine)
	if err != nil {
		return nil, err
	}
	for _, it := range items {
		switch {
		case it.Status == string(StatusReview):
			if len(out.InReview) < dashboardListLimit {
				out.InReview = append(out.InReview, it)
			}
		case it.ReturnNote != "":
			if len(out.Returned) < dashboardListLimit {
				out.Returned = append(out.Returned, it)
			}
		default:
			if len(out.Drafts) < dashboardListLimit {
				out.Drafts = append(out.Drafts, it)
			}
		}
	}

	// очередь на проверку
	if out.CanReview {
		// запрос строится заново для каждого использования: gorm-выражение после Count переиспользовать небезопасно
		queue := func() *gorm.DB {
			q := db.Table("documents d").Where("d.status = ?", string(StatusReview))
			if !a.Directorate {
				q = q.Where("(d.author_id IS NULL OR d.author_id <> ?)", a.UserID) // свой документ рецензент не проверяет
			}
			return q
		}
		var total int64
		if err := queue().Count(&total).Error; err != nil {
			return nil, err
		}
		out.QueueTotal = int(total)
		rows, err := s.dashRows(queue().Order("submitted_at ASC NULLS LAST, d.id ASC").Limit(dashboardQueueLimit))
		if err != nil {
			return nil, err
		}
		if out.Queue, err = s.dashItems(db, a, rows); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// dashRows читает документы из запроса вместе с логином автора и временем последней отправки на проверку.
func (s *Service) dashRows(q *gorm.DB) ([]dashRow, error) {
	var rows []dashRow
	err := q.Select(`d.*, u.login::text AS author_login,
			(SELECT max(e.created_at) FROM review_events e WHERE e.document_id = d.id AND e.kind = 'submit') AS submitted_at`).
		Joins("LEFT JOIN users u ON u.id = d.author_id").Scan(&rows).Error
	return rows, err
}

// dashItems превращает строки в элементы списка: добавляет число открытых замечаний и причину возврата.
func (s *Service) dashItems(db *gorm.DB, a Actor, rows []dashRow) ([]DashboardItem, error) {
	ids := make([]int64, len(rows))
	for i, r := range rows {
		ids[i] = r.ID
	}
	open := map[int64]int{}
	last := map[int64]struct {
		Kind    string
		Comment string
		Actor   *string
	}{}
	if len(ids) > 0 {
		var oc []struct {
			DocumentID int64
			N          int
		}
		if err := db.Table("review_comments").Select("document_id, count(*) AS n").Where("document_id IN ? AND resolved_at IS NULL", ids).Group("document_id").Scan(&oc).Error; err != nil {
			return nil, err
		}
		for _, o := range oc {
			open[o.DocumentID] = o.N
		}
		var ev []struct {
			DocumentID int64
			Kind       string
			Comment    string
			ActorLogin *string
		}
		err := db.Raw(`SELECT DISTINCT ON (e.document_id) e.document_id, e.kind, e.comment, u.login::text AS actor_login
			FROM review_events e LEFT JOIN users u ON u.id = e.actor_id
			WHERE e.document_id IN ? ORDER BY e.document_id, e.id DESC`, ids).Scan(&ev).Error
		if err != nil {
			return nil, err
		}
		for _, e := range ev {
			last[e.DocumentID] = struct {
				Kind    string
				Comment string
				Actor   *string
			}{e.Kind, e.Comment, e.ActorLogin}
		}
	}
	out := make([]DashboardItem, len(rows))
	for i, r := range rows {
		it := DashboardItem{
			ID: r.ID, Code: r.Code, Type: r.Type, TypeName: Type(r.Type).NameIn(a.Lang), Title: r.Title, Status: r.Status, Revision: r.Revision,
			Author: r.AuthorLogin, UpdatedAt: r.UpdatedAt.UTC(), OpenComments: open[r.ID],
		}
		if r.SubmittedAt != nil {
			t := r.SubmittedAt.UTC()
			it.SubmittedAt = &t
		}
		// «Возвращён» — пока последнее событие рецензии именно возврат: после повторной отправки причина уже не висит
		if l, ok := last[r.ID]; ok && l.Kind == string(ReviewReturn) && r.Status == string(StatusDraft) {
			it.ReturnNote, it.ReturnedBy = l.Comment, l.Actor
		}
		out[i] = it
	}
	return out, nil
}
