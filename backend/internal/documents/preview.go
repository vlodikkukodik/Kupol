package documents

import (
	"context"
	"strings"

	"kupol/internal/i18n"
)

// Предпросмотр «глазами уровня N» (team panel, шаг 3.4).
//
// Автор видит не «как это могло бы выглядеть», а то, что сервер действительно отдал бы читателю этого уровня:
// тот же RenderBlocks, тот же разбор ссылок и те же правила доступа к документу, что и при обычном чтении. Поэтому
// предпросмотр не может разойтись с боем: подмены допуска на клиенте здесь нет вообще.
//
// Предпросмотр строится по содержимому, которое прислал редактор (несохранённые правки), а без него — по сохранённому.
// Он никуда ничего не пишет (запись о прочтении не создаётся), а неполный черновик не мешает: блоки с замечаниями просто
// не попадают в предпросмотр, замечания возвращаются рядом.

// Что увидел бы читатель, открыв документ по прямой ссылке.
const (
	PreviewOpen     = "open"      // документ открыт
	PreviewNotFound = "not_found" // «Дело не найдено» (закрытый документ с режимом direct_link: not_found)
	PreviewDenied   = "denied"    // «Доступ запрещён» и нужный уровень (direct_link: forbidden)
)

// PreviewResult — документ так, как его увидит читатель уровня Level.
type PreviewResult struct {
	Level         int          `json:"level"`
	Access        string       `json:"access" tstype:"'open' | 'not_found' | 'denied'"`
	RequiredLevel int          `json:"required_level"`
	Document      *OutDocument `json:"document,omitempty"`
	// Problems — замечания к содержимому; блоки с замечаниями в предпросмотр не вошли.
	Problems []Problem `json:"problems"`
}

// previewViewer — читатель заданного уровня: 0 — Гражданин без входа, 1–6 — пользователь, 7 — Директорат.
func previewViewer(level int, userID int64) Viewer {
	switch {
	case level <= LevelPublic:
		return Guest
	case level >= LevelDirectorate:
		return Viewer{UserID: userID, Directorate: true}
	default:
		return Viewer{UserID: userID, UserLevel: level}
	}
}

// TeamPreview показывает документ глазами читателя уровня level (0–7). c — содержимое из редактора; nil — сохранённое.
// Видеть предпросмотр вправе тот, кто вправе видеть документ в team panel (менять его для этого не нужно).
func (s *Service) TeamPreview(ctx context.Context, a Actor, id int64, level int, c *Content) (*PreviewResult, error) {
	if level < LevelPublic || level > MaxLevel {
		return nil, queryError("level", "уровень должен быть от 0 до 7 (7 — Директорат)")
	}
	stored, err := s.viewable(ctx, s.db, a, id)
	if err != nil {
		return nil, err
	}
	d := *stored
	content := contentFromDocument(&d)
	if c != nil {
		content = *c
	}

	var probs Problems
	code := ""
	if d.Code != nil {
		code = *d.Code
	}
	input := content.input(code, d.Type, d.Status)
	pr := input.prepare("$", &probs)
	if pr != nil {
		applyContent(&d, pr.Doc)
	} else {
		// Содержимое не проходит проверку целиком (черновик): берём то, что верно само по себе, остальное — как сохранено.
		if strings.TrimSpace(content.Title) != "" {
			d.Title = content.Title
		}
		if content.Level != nil && *content.Level >= 0 && *content.Level <= MaxLevel {
			d.Level = *content.Level
		}
		if DirectLink(content.DirectLink).Valid() && content.DirectLink != "" {
			d.DirectLink = content.DirectLink
		}
		if content.Grif != "" {
			d.Grif = content.Grif
		}
		var skipped Problems
		d.Blocks = BlockList(normalizeBlocksAt(content.Blocks, &skipped, ""))
	}

	v := previewViewer(level, a.UserID)
	res := &PreviewResult{Level: level, Access: PreviewOpen, Problems: LocalizeProblems(i18n.From(ctx), probs.List())}
	if res.Problems == nil {
		res.Problems = []Problem{}
	}
	if d.Level > v.Level() {
		res.RequiredLevel = d.Level
		res.Access = PreviewNotFound
		if DirectLink(d.DirectLink) == DirectLinkForbidden {
			res.Access = PreviewDenied
		}
		return res, nil
	}

	resolve, err := s.linkResolver(ctx, v, d.Blocks)
	if err != nil {
		return nil, err
	}
	blocks, err := RenderBlocks(d.Blocks, d.Level, v.Level(), resolve)
	if err != nil {
		return nil, err
	}
	var author *string
	if d.AuthorID != nil {
		var u struct{ Login string }
		if err := s.db.WithContext(ctx).Table("users").Select("login").Where("id = ?", *d.AuthorID).Scan(&u).Error; err != nil {
			return nil, err
		}
		if u.Login != "" {
			author = &u.Login
		}
	}
	res.Document = outDocument(&d, author, blocks, v)
	return res, nil
}
