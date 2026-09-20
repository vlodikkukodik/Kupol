package documents

import "context"

// «Упоминается в»: документы, которые ссылаются на данный блоками doc_link (таблица document_links).
//
// Читатель видит упоминание, только если он вправе видеть и документ-источник (допуск, статус — как в каталоге), и сам блок-ссылку
// (уровень блока не выше допуска). Иначе по списку можно было бы узнать, что закрытый документ существует и о чём-то говорит.

const maxMentions = 100

// Mention — документ, который ссылается на данный.
type Mention struct {
	Code     string `json:"code"`
	Slug     string `json:"slug"`
	Title    string `json:"title"`
	Type     string `json:"type"`
	TypeName string `json:"type_name"`
}

// mentions возвращает упоминания документа с шифром code для читателя v: по шифру, без повторов (несколько ссылок из одного документа —
// одно упоминание), в естественном порядке шифров.
func (s *Service) mentions(ctx context.Context, v Viewer, code string) ([]Mention, error) {
	level := v.Level()
	q := s.db.WithContext(ctx).Table("document_links l").
		Select("d.code, d.slug, d.title, d.type").
		Joins("JOIN documents d ON d.id = l.source_id").
		Where("l.target_code = ? AND l.level <= ? AND d.level <= ? AND d.code IS NOT NULL", code, level, level)
	if !v.SeesUnpublished() {
		q = q.Where("d.status = ?", string(StatusPublished))
	}
	var rows []Document
	err := q.Group("d.id, d.code, d.slug, d.title, d.type").
		Order("d.code COLLATE kupol_natural, d.id").Limit(maxMentions).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make([]Mention, len(rows))
	for i, r := range rows {
		out[i] = Mention{Code: deref(r.Code), Slug: deref(r.Slug), Title: r.Title, Type: r.Type, TypeName: Type(r.Type).Name()}
	}
	return out, nil
}
