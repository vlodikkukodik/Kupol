package documents

import (
	"context"
	"slices"
)

// Граф связей («доска с нитками»): документы — карточки, ссылки doc_link между ними — нити. Строится по document_links.
//
// Читатель видит в графе только то, что вправе видеть при чтении: карточку — если может открыть документ (допуск, опубликованность),
// нить — если видны оба её конца И сам блок-ссылка (его уровень не выше допуска). Закрытый документ не появляется даже как пустая
// карточка «неизвестно что», а нить к нему не рисуется: по «обрывку» можно было бы судить, что закрытое существует.

const (
	maxGraphDepth = 2
	maxGraphNodes = 40
)

// GraphNode — карточка на доске.
type GraphNode struct {
	Code     string `json:"code"`
	Slug     string `json:"slug"`
	Title    string `json:"title"`
	Type     string `json:"type"`
	TypeName string `json:"type_name"`
	Level    int    `json:"level"`
	Year     int    `json:"year"`
	// Depth — расстояние от центра в нитях: 0 — сам документ, 1 — связанные с ним напрямую, 2 — через одного.
	Depth int `json:"depth"`
}

// GraphEdge — нить: документ From ссылается на To.
type GraphEdge struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// GraphResult — доска вокруг одного документа.
type GraphResult struct {
	Center string      `json:"center"`
	Depth  int         `json:"depth"`
	Nodes  []GraphNode `json:"nodes"`
	Edges  []GraphEdge `json:"edges"`
	// Truncated — связей больше, чем помещается на доске: показаны первые по шифру.
	Truncated bool `json:"truncated,omitempty"`
}

// Graph строит доску вокруг документа ref на depth нитей (1 или 2). Документ, которого читатель не видит, — ErrNotFound.
func (s *Service) Graph(ctx context.Context, v Viewer, ref string, depth int) (*GraphResult, error) {
	if depth == 0 {
		depth = 1
	}
	if depth < 1 || depth > maxGraphDepth {
		return nil, queryError("depth", "глубина — от 1 до %d", maxGraphDepth)
	}
	c, err := ParseCode(ref)
	if err != nil {
		return nil, ErrNotFound
	}
	db := s.db.WithContext(ctx)
	var root Document
	err = visible(db.Model(&Document{}).Where("code IS NOT NULL AND slug = ?", c.Slug), v).Select(listColumns).Take(&root).Error
	if err != nil {
		return nil, ErrNotFound // и «нет», и «не видно»: существование закрытого не раскрывается
	}
	center := deref(root.Code)

	level := v.Level()
	statusCond, statusArgs := "", []any{}
	if !v.SeesUnpublished() {
		statusCond = " AND d.status = ? AND t.status = ?"
		statusArgs = []any{string(StatusPublished), string(StatusPublished)}
	}
	// Нити между документами, видимыми читателю; условие на концы (in) добавляет вызывающий.
	edgeSQL := "SELECT DISTINCT d.code AS a, t.code AS b FROM document_links l" +
		" JOIN documents d ON d.id = l.source_id JOIN documents t ON t.code = l.target_code" +
		" WHERE l.level <= ? AND d.level <= ? AND t.level <= ? AND d.code IS NOT NULL" + statusCond
	type pair struct{ A, B string }
	baseArgs := append([]any{level, level, level}, statusArgs...)

	depthOf := map[string]int{center: 0}
	order := []string{center}
	frontier := []string{center}
	truncated := false
	for d := 1; d <= depth && len(frontier) > 0; d++ {
		var pairs []pair
		args := append(slices.Clone(baseArgs), frontier, frontier)
		if err := db.Raw(edgeSQL+" AND (d.code IN ? OR t.code IN ?)", args...).Scan(&pairs).Error; err != nil {
			return nil, err
		}
		var found []string
		for _, p := range pairs {
			for _, code := range []string{p.A, p.B} {
				if _, seen := depthOf[code]; !seen && !slices.Contains(found, code) {
					found = append(found, code)
				}
			}
		}
		sortCodesNatural(found)
		frontier = frontier[:0:0]
		for _, code := range found {
			if len(order) >= maxGraphNodes {
				truncated = true
				break
			}
			depthOf[code] = d
			order = append(order, code)
			frontier = append(frontier, code)
		}
	}

	var pairs []pair
	if err := db.Raw(edgeSQL+" AND d.code IN ? AND t.code IN ?", append(slices.Clone(baseArgs), order, order)...).Scan(&pairs).Error; err != nil {
		return nil, err
	}
	edges := make([]GraphEdge, 0, len(pairs))
	for _, p := range pairs {
		edges = append(edges, GraphEdge{From: p.A, To: p.B})
	}
	slices.SortFunc(edges, func(x, y GraphEdge) int {
		if c := compareCodes(x.From, y.From); c != 0 {
			return c
		}
		return compareCodes(x.To, y.To)
	})

	var docs []Document
	if err := db.Model(&Document{}).Where("code IN ?", order).Select(listColumns).Find(&docs).Error; err != nil {
		return nil, err
	}
	byCode := make(map[string]*Document, len(docs))
	for i := range docs {
		byCode[deref(docs[i].Code)] = &docs[i]
	}
	nodes := make([]GraphNode, 0, len(order))
	for _, code := range order {
		d, ok := byCode[code]
		if !ok {
			continue
		}
		nodes = append(nodes, GraphNode{
			Code: code, Slug: deref(d.Slug), Title: d.Title, Type: d.Type, TypeName: Type(d.Type).NameIn(v.Lang),
			Level: d.Level, Year: d.ComposedYear, Depth: depthOf[code],
		})
	}
	return &GraphResult{Center: center, Depth: depth, Nodes: nodes, Edges: edges, Truncated: truncated}, nil
}

// sortCodesNatural сортирует шифры так же, как каталог: О-9 раньше О-10.
func sortCodesNatural(codes []string) {
	slices.SortFunc(codes, compareCodes)
}

// compareCodes — естественный порядок: числа внутри шифра сравниваются как числа («О-9» раньше «О-10», даже если разрядов разное число).
func compareCodes(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	i, j := 0, 0
	for i < len(ra) && j < len(rb) {
		if isDigit(ra[i]) && isDigit(rb[j]) {
			si := i
			for i < len(ra) && isDigit(ra[i]) {
				i++
			}
			sj := j
			for j < len(rb) && isDigit(rb[j]) {
				j++
			}
			na, nb := trimZeros(string(ra[si:i])), trimZeros(string(rb[sj:j]))
			if len(na) != len(nb) {
				return len(na) - len(nb)
			}
			if na != nb {
				if na < nb {
					return -1
				}
				return 1
			}
			continue
		}
		if ra[i] != rb[j] {
			if ra[i] < rb[j] {
				return -1
			}
			return 1
		}
		i++
		j++
	}
	return (len(ra) - i) - (len(rb) - j)
}

func isDigit(r rune) bool { return r >= '0' && r <= '9' }

func trimZeros(s string) string {
	for len(s) > 1 && s[0] == '0' {
		s = s[1:]
	}
	return s
}
