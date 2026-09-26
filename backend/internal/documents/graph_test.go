package documents

import (
	"fmt"
	"slices"
	"strings"
	"testing"
)

func (e *env) graph(v Viewer, code string, depth int) *GraphResult {
	e.t.Helper()
	g, err := e.svc.Graph(ctx, v, code, depth)
	if err != nil {
		e.t.Fatalf("Graph(%s, уровень %d, глубина %d): %v", code, v.Level(), depth, err)
	}
	return g
}

func nodeCodes(g *GraphResult) []string {
	out := make([]string, len(g.Nodes))
	for i, n := range g.Nodes {
		out[i] = n.Code
	}
	slices.Sort(out)
	return out
}

func edgeKeys(g *GraphResult) []string {
	out := make([]string, len(g.Edges))
	for i, e := range g.Edges {
		out[i] = e.From + ">" + e.To
	}
	slices.Sort(out)
	return out
}

// Доска: О-820 в центре; ссылки в обе стороны, через одного, закрытые документы и блоки-ссылки.
//
//	О-821 → О-820   (открытый входящий)
//	О-820 → О-822   (открытый исходящий)
//	О-820 → О-823   (документ уровня 3)
//	О-824 → О-820   (ссылка-блок уровня 4)
//	О-825 → О-820   (черновик)
//	О-822 → О-826   (через одного)
//	О-826 → О-823   (нить между двумя карточками доски: видна, если видны оба конца)
func TestGraphShowsOnlyWhatViewerMayRead(t *testing.T) {
	e := newEnv(t)
	e.imp(obj("О-820", "Центр", "published", 0, blocks(linkJSON("a", 0, "О-822"), linkJSON("b", 0, "О-823"))))
	e.imp(obj("О-821", "Входящий", "published", 0, blocks(linkJSON("a", 0, "О-820"))))
	e.imp(obj("О-822", "Исходящий", "published", 0, blocks(linkJSON("a", 0, "О-826"))))
	e.imp(obj("О-823", "Уровень 3", "published", 3, blocks(para("p", nil, "текст"))))
	e.imp(obj("О-824", "Закрытая нить", "published", 0, blocks(linkJSON("a", 4, "О-820"))))
	e.imp(obj("О-825", "Черновик", "draft", 0, blocks(linkJSON("a", 0, "О-820"))))
	e.imp(obj("О-826", "Дальний", "published", 0, blocks(linkJSON("a", 0, "О-823"))))

	for level := 0; level <= 7; level++ {
		v := viewerAt(level)
		g := e.graph(v, "О-820", 1)
		wantNodes := []string{"О-820", "О-821", "О-822"}
		wantEdges := []string{"О-820>О-822", "О-821>О-820"}
		if level >= 3 {
			wantNodes = append(wantNodes, "О-823")
			wantEdges = append(wantEdges, "О-820>О-823")
		}
		if level >= 4 {
			wantNodes = append(wantNodes, "О-824")
			wantEdges = append(wantEdges, "О-824>О-820")
		}
		if level == 7 {
			wantNodes = append(wantNodes, "О-825")
			wantEdges = append(wantEdges, "О-825>О-820")
		}
		slices.Sort(wantNodes)
		slices.Sort(wantEdges)
		if got := nodeCodes(g); !slices.Equal(got, wantNodes) {
			t.Errorf("уровень %d, глубина 1: карточки %v, ожидалось %v", level, got, wantNodes)
		}
		if got := edgeKeys(g); !slices.Equal(got, wantEdges) {
			t.Errorf("уровень %d, глубина 1: нити %v, ожидалось %v", level, got, wantEdges)
		}
		// нить всегда соединяет карточки, которые есть на доске
		have := map[string]bool{}
		for _, n := range g.Nodes {
			have[n.Code] = true
		}
		for _, ed := range g.Edges {
			if !have[ed.From] || !have[ed.To] {
				t.Errorf("уровень %d: нить %s>%s без карточки", level, ed.From, ed.To)
			}
		}
	}

	// глубина 2: «через одного» появляется и нити между карточками доски
	g := e.graph(Guest, "О-820", 2)
	if got := nodeCodes(g); !slices.Equal(got, []string{"О-820", "О-821", "О-822", "О-826"}) {
		t.Errorf("гость, глубина 2: %v", got)
	}
	if got := edgeKeys(g); !slices.Equal(got, []string{"О-820>О-822", "О-821>О-820", "О-822>О-826"}) { // нить О-826>О-823 — к закрытому: нет
		t.Errorf("гость, глубина 2: нити %v", got)
	}
	depths := map[string]int{}
	for _, n := range g.Nodes {
		depths[n.Code] = n.Depth
	}
	if depths["О-820"] != 0 || depths["О-821"] != 1 || depths["О-826"] != 2 {
		t.Errorf("глубины: %v", depths)
	}
	g3 := e.graph(viewerAt(3), "О-820", 2)
	if !slices.Contains(edgeKeys(g3), "О-826>О-823") {
		t.Errorf("уровень 3 не видит нить между двумя карточками доски: %v", edgeKeys(g3))
	}

	// закрытый или несуществующий центр — одинаковая ошибка
	for name, code := range map[string]string{"закрытый": "О-823", "нет такого": "О-999", "не шифр": "мусор"} {
		if _, err := e.svc.Graph(ctx, Guest, code, 1); err != ErrNotFound {
			t.Errorf("%s: %v", name, err)
		}
	}
	// ответ не содержит названий закрытого
	raw := fmt.Sprintf("%+v", e.graph(Guest, "О-820", 2))
	for _, secret := range []string{"Уровень 3", "Закрытая нить", "Черновик", "О-823", "О-824", "О-825"} {
		if strings.Contains(raw, secret) {
			t.Errorf("в доске гостя есть %q", secret)
		}
	}
	// параметры
	for _, d := range []int{-1, 3, 9} {
		if _, err := e.svc.Graph(ctx, Guest, "О-820", d); err == nil {
			t.Errorf("глубина %d принята", d)
		}
	}
}

func TestGraphIsCappedAndOrderedNaturally(t *testing.T) {
	e := newEnv(t)
	var bs []string
	for i := 1; i <= maxGraphNodes+10; i++ {
		e.imp(obj(fmt.Sprintf("О-%d", 1000+i), fmt.Sprintf("Спутник %d", i), "published", 0, blocks(linkJSON("a", 0, "О-900"))))
		bs = append(bs, linkJSON(fmt.Sprintf("b%d", i), 0, fmt.Sprintf("О-%d", 1000+i)))
	}
	e.imp(obj("О-900", "Центр", "published", 0, blocks(bs...)))
	g := e.graph(Guest, "О-900", 1)
	if len(g.Nodes) != maxGraphNodes || !g.Truncated {
		t.Fatalf("карточек %d, обрезано=%v", len(g.Nodes), g.Truncated)
	}
	// остались первые по шифру
	codes := nodeCodes(g)
	if !slices.Contains(codes, "О-1001") || slices.Contains(codes, fmt.Sprintf("О-%d", 1000+maxGraphNodes+10)) {
		t.Errorf("обрезка не по порядку шифров: первые %v", codes[:3])
	}
	if len(g.Edges) != 2*(maxGraphNodes-1) { // центр и каждый спутник ссылаются друг на друга
		t.Errorf("нитей %d, ожидалось %d", len(g.Edges), 2*(maxGraphNodes-1))
	}
}

func TestCompareCodesNaturalOrder(t *testing.T) {
	in := []string{"О-10", "О-9", "О-1000", "О-041", "ПРИКАЗ-1978-12", "ПРИКАЗ-1978-2", "О-0"}
	slices.SortFunc(in, compareCodes)
	want := []string{"О-0", "О-9", "О-10", "О-041", "О-1000", "ПРИКАЗ-1978-2", "ПРИКАЗ-1978-12"}
	if !slices.Equal(in, want) {
		t.Errorf("порядок %v", in)
	}
}
