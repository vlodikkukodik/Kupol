package documents

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"sort"
	"strings"
	"testing"
)

// Тесты безопасности чтения. Идея: в каждое строковое поле каждого типа блока кладётся уникальный маркер,
// помнящий свой уровень допуска. Затем для каждого читателя (уровни 0–7) проверяются два свойства:
//   1. в ответе НЕТ ни одного маркера выше его допуска (ничего не утекло);
//   2. в ответе ЕСТЬ все маркеры, которые он вправе видеть (ничего лишнего не закрыто).

// tokenGen выдаёт маркеры вида «Z3q17x» и запоминает уровень каждого.
type tokenGen struct {
	rnd    *rand.Rand
	levels map[string]int
	n      int
	// linkNotes — примечания ссылок: при недоступной цели они намеренно скрываются, поэтому их
	// присутствие в ответе не требуется (а отсутствие выше допуска проверяется как у всех).
	linkNotes map[string]bool
}

func newTokenGen(seed int64) *tokenGen {
	return &tokenGen{rnd: rand.New(rand.NewSource(seed)), levels: map[string]int{}, linkNotes: map[string]bool{}}
}

// exempt — маркеры, присутствие которых не проверяется: цели ссылок (зависит от видимости ссылки) и примечания.
func (g *tokenGen) exempt(targets []linkTarget) map[string]bool {
	out := map[string]bool{}
	for _, tg := range targets {
		out[tg.title], out[tg.code], out[tg.slug] = true, true, true
	}
	for n := range g.linkNotes {
		out[n] = true
	}
	return out
}

// tok — маркер для текстового поля с уровнем level. Суффикс «x» исключает совпадение одного маркера
// с началом другого (Z3q1x и Z3q12x).
func (g *tokenGen) tok(level int) string {
	g.n++
	t := fmt.Sprintf("Z%dq%dx", level, g.n)
	g.levels[t] = level
	return t
}

// idTok — маркер для идентификатора блока (строчные буквы: id других не допускает).
func (g *tokenGen) idTok(level int) string {
	g.n++
	t := fmt.Sprintf("id%dq%dx", level, g.n)
	g.levels[t] = level
	return t
}

// rich — текст из фрагментов со случайными уровнями не ниже base.
func (g *tokenGen) rich(base int) []map[string]any {
	n := 1 + g.rnd.Intn(4)
	out := make([]map[string]any, n)
	for i := range out {
		lvl := base
		if g.rnd.Intn(2) == 0 {
			lvl = base + g.rnd.Intn(MaxLevel-base+1)
		}
		run := map[string]any{"text": g.tok(lvl)}
		if lvl > 0 {
			run["level"] = lvl
		}
		if g.rnd.Intn(4) == 0 {
			run["bold"] = true
		}
		out[i] = run
	}
	return out
}

// linkTargets — документы, на которые ссылаются блоки doc_link; уровень цели = её номер (О-0 … О-7).
type linkTarget struct {
	code, slug, title string
	level             int
}

func makeTargets(g *tokenGen) []linkTarget {
	out := make([]linkTarget, MaxLevel+1)
	for l := 0; l <= MaxLevel; l++ {
		c := ObjectCode(500 + l)
		out[l] = linkTarget{code: c.Canonical, slug: c.Slug, title: g.tok(l), level: l}
		// шифр цели тоже считается закрытым маркером её уровня
		g.levels[c.Canonical] = l
		g.levels[c.Slug] = l
	}
	return out
}

// block собирает блок каждого типа, все текстовые поля которого несут маркеры уровня lvl.
func (g *tokenGen) block(kind string, lvl int, targets []linkTarget) InputBlock {
	var data any
	switch kind {
	case "heading":
		data = map[string]any{"depth": 2, "text": g.tok(lvl)}
	case "paragraph":
		data = map[string]any{"text": g.rich(lvl)}
	case "list":
		data = map[string]any{"ordered": true, "items": []any{g.rich(lvl), g.rich(lvl)}}
	case "quote":
		data = map[string]any{"text": g.rich(lvl), "source": g.tok(lvl)}
	case "dossier_header":
		data = map[string]any{}
	case "experiment_log":
		data = map[string]any{"title": g.tok(lvl), "entries": []any{
			map[string]any{"date": g.tok(lvl), "participants": []string{g.tok(lvl), g.tok(lvl)}, "text": g.rich(lvl)},
			map[string]any{"text": g.rich(lvl)},
		}}
	case "stamp":
		data = map[string]any{"text": g.tok(lvl), "tone": "ink"}
	case "memo":
		data = map[string]any{
			"kind": "memo", "number": g.tok(lvl), "date": g.tok(lvl), "from": g.tok(lvl),
			"to": []string{g.tok(lvl), g.tok(lvl)}, "subject": g.tok(lvl),
			"body": []any{g.rich(lvl), g.rich(lvl)}, "signature": g.tok(lvl),
		}
	case "clipping":
		data = map[string]any{
			"kind": "newspaper", "title": g.tok(lvl), "source": g.tok(lvl), "date": g.tok(lvl),
			"paragraphs": []any{g.rich(lvl), g.rich(lvl)},
		}
	case "clipping_transcript":
		kind = "clipping"
		data = map[string]any{
			"kind": "transcript", "title": g.tok(lvl), "source": g.tok(lvl), "date": g.tok(lvl),
			"lines": []any{
				map[string]any{"speaker": g.tok(lvl), "text": g.rich(lvl)},
				map[string]any{"text": g.rich(lvl)},
			},
		}
	case "table":
		data = map[string]any{"caption": g.tok(lvl), "columns": []string{g.tok(lvl), g.tok(lvl)},
			"rows": []any{[]any{g.rich(lvl), g.rich(lvl)}, []any{g.rich(lvl), g.rich(lvl)}}}
	case "doc_link":
		t := targets[g.rnd.Intn(len(targets))]
		note := g.tok(lvl)
		g.linkNotes[note] = true
		data = map[string]any{"code": t.code, "note": note}
	case "divider":
		data = map[string]any{"style": "stars"}
	case "page":
		data = map[string]any{"number": g.tok(lvl)}
	case "footnote":
		data = map[string]any{"mark": g.tok(lvl), "text": g.rich(lvl)}
	case "appendix":
		data = map[string]any{"number": g.tok(lvl), "title": g.tok(lvl)}
	default:
		panic("неизвестный тип в тесте: " + kind)
	}
	raw, err := json.Marshal(data)
	if err != nil {
		panic(err)
	}
	ib := InputBlock{ID: g.idTok(lvl), Type: kind, Data: raw}
	if lvl > 0 || g.rnd.Intn(2) == 0 {
		l := lvl
		ib.Level = &l
	}
	return ib
}

var allKinds = []string{
	"heading", "paragraph", "list", "quote", "dossier_header", "experiment_log", "stamp", "memo",
	"clipping", "clipping_transcript", "table", "doc_link", "divider", "page", "footnote", "appendix",
}

// resolverFor — как разрешает ссылки сервис: цель видна, если уровень читателя не ниже её уровня.
func resolverFor(targets []linkTarget, viewer int) LinkResolver {
	return func(code string) *LinkTarget {
		for _, t := range targets {
			if t.code == code && t.level <= viewer {
				return &LinkTarget{Code: t.code, Slug: t.slug, Title: t.title, Type: TypeObject}
			}
		}
		return nil
	}
}

// checkVisibility проверяет оба свойства для одного читателя. linkTitles — цели ссылок: их присутствие
// не проверяется (оно зависит от того, есть ли видимая ссылка), а отсутствие — обязательно.
func checkVisibility(t *testing.T, out any, levels map[string]int, viewer int, exemptPresence map[string]bool, label string) {
	t.Helper()
	raw, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)
	var leaked, missing []string
	for tok, lvl := range levels {
		has := strings.Contains(body, tok)
		switch {
		case lvl > viewer && has:
			leaked = append(leaked, fmt.Sprintf("%s(уровень %d)", tok, lvl))
		case lvl <= viewer && !has && !exemptPresence[tok]:
			missing = append(missing, fmt.Sprintf("%s(уровень %d)", tok, lvl))
		}
	}
	sort.Strings(leaked)
	sort.Strings(missing)
	if len(leaked) > 0 {
		t.Errorf("%s: читатель с допуском %d видит закрытое: %v", label, viewer, leaked)
	}
	if len(missing) > 0 {
		t.Errorf("%s: читатель с допуском %d не получил открытое: %v", label, viewer, missing)
	}
}

// Каждый тип блока на каждом уровне: точное свойство «видно ровно то, что положено».
func TestEveryBlockKindEveryLevel(t *testing.T) {
	g := newTokenGen(1)
	targets := makeTargets(g)

	var in []InputBlock
	for _, kind := range allKinds {
		for lvl := 0; lvl <= MaxLevel; lvl++ {
			if kind == "dossier_header" && lvl > 0 {
				continue // шапка в документе одна
			}
			in = append(in, g.block(kind, lvl, targets))
		}
	}
	var p Problems
	blocks := NormalizeBlocks(in, &p)
	if p.Any() {
		t.Fatalf("тестовый документ не прошёл проверку:\n%v", p.List())
	}
	if len(blocks) != len(in) {
		t.Fatalf("блоков %d из %d", len(blocks), len(in))
	}

	exempt := g.exempt(targets)

	for viewer := 0; viewer <= MaxLevel; viewer++ {
		out, err := RenderBlocks(blocks, 0, viewer, resolverFor(targets, viewer))
		if err != nil {
			t.Fatalf("допуск %d: %v", viewer, err)
		}
		checkVisibility(t, out, g.levels, viewer, exempt, fmt.Sprintf("допуск %d", viewer))
	}
}

// Случайные документы: смесь типов и уровней, у блоков и у отдельных фрагментов текста.
func TestRandomDocumentsNeverLeak(t *testing.T) {
	for seed := int64(100); seed < 400; seed++ {
		g := newTokenGen(seed)
		targets := makeTargets(g)
		n := 1 + g.rnd.Intn(25)
		var in []InputBlock
		header := false
		for i := 0; i < n; i++ {
			kind := allKinds[g.rnd.Intn(len(allKinds))]
			if kind == "dossier_header" {
				if header {
					continue // шапка в документе одна
				}
				header = true
			}
			in = append(in, g.block(kind, g.rnd.Intn(MaxLevel+1), targets))
		}
		var p Problems
		blocks := NormalizeBlocks(in, &p)
		if p.Any() {
			t.Fatalf("seed %d: %v", seed, p.List())
		}
		exempt := g.exempt(targets)
		for viewer := 0; viewer <= MaxLevel; viewer++ {
			out, err := RenderBlocks(blocks, 0, viewer, resolverFor(targets, viewer))
			if err != nil {
				t.Fatalf("seed %d, допуск %d: %v", seed, viewer, err)
			}
			checkVisibility(t, out, g.levels, viewer, exempt, fmt.Sprintf("seed %d, допуск %d", seed, viewer))
			if t.Failed() {
				return
			}
		}
	}
}

// Уровень документа поднимает уровень всех его блоков: блок с более низким уровнем не открывается раньше документа.
func TestDocumentLevelRaisesBlockLevels(t *testing.T) {
	g := newTokenGen(7)
	targets := makeTargets(g)
	in := []InputBlock{
		g.block("paragraph", 0, targets),
		g.block("paragraph", 2, targets),
		g.block("paragraph", 5, targets),
	}
	var p Problems
	blocks := NormalizeBlocks(in, &p)
	if p.Any() {
		t.Fatal(p.List())
	}
	// документ уровня 3: блоки уровня 0 и 2 ведут себя как уровень 3
	for viewer := 3; viewer <= MaxLevel; viewer++ {
		out, _ := RenderBlocks(blocks, 3, viewer, nil)
		raw, _ := json.Marshal(out)
		body := string(raw)
		for tok, lvl := range g.levels {
			eff := max(lvl, 3)
			// уровни в g.levels у фрагментов уже выше или равны уровню блока; блоки 0 и 2 поднимаются до 3
			if strings.HasPrefix(tok, "id") {
				continue
			}
			if eff > viewer && strings.Contains(body, tok) {
				t.Errorf("документ уровня 3, допуск %d: виден маркер %s (эффективный уровень %d)", viewer, tok, eff)
			}
		}
	}
	// читателю ниже уровня документа блоки не показываются вовсе (в этой функции такой вызов не делается сервисом,
	// но защита обязана держаться и здесь)
	out, _ := RenderBlocks(blocks, 3, 1, nil)
	raw, _ := json.Marshal(out)
	for tok := range g.levels {
		if strings.Contains(string(raw), tok) {
			t.Errorf("допуск 1 ниже уровня документа 3: виден %s", tok)
		}
	}
}

// Закрытый блок — это только метка уровня: без идентификатора, типа, данных и следов длины.
func TestRedactedBlockCarriesOnlyLevel(t *testing.T) {
	g := newTokenGen(3)
	targets := makeTargets(g)
	secret := g.block("memo", 4, targets)
	secret.ID = "secret-memo-id"
	in := []InputBlock{g.block("heading", 0, targets), secret, g.block("paragraph", 4, targets), g.block("stamp", 6, targets)}
	var p Problems
	blocks := NormalizeBlocks(in, &p)
	if p.Any() {
		t.Fatal(p.List())
	}
	out, err := RenderBlocks(blocks, 0, 2, nil)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(out)
	body := string(raw)
	for _, must := range []string{`{"type":"redacted","data":{"level":4}}`, `{"type":"redacted","data":{"level":6}}`} {
		if !strings.Contains(body, must) {
			t.Errorf("нет метки %s в %s", must, body)
		}
	}
	for _, mustNot := range []string{"secret-memo-id", `"type":"memo"`, `"type":"paragraph"`, `"type":"stamp"`} {
		if strings.Contains(body, mustNot) {
			t.Errorf("в ответе закрытого блока есть %q: %s", mustNot, body)
		}
	}
	// два соседних блока уровня 4 (записка и абзац) слились в одну метку
	if strings.Count(body, `"level":4}`) != 1 {
		t.Errorf("соседние закрытые блоки одного уровня должны сливаться: %s", body)
	}
	if len(out) != 3 { // заголовок, метка 4, метка 6
		t.Errorf("блоков в ответе %d, ожидалось 3: %s", len(out), body)
	}
}

// Закрытые фрагменты текста не выдают ни текста, ни длины; соседние одного уровня сливаются.
func TestRedactedRunsCarryNoTextAndNoLength(t *testing.T) {
	r := Rich{
		{Text: "Открыто. "},
		{Text: "СЕКРЕТ-КОРОТКИЙ", Level: 3},
		{Text: "СЕКРЕТ-ОЧЕНЬ-ДЛИННЫЙ-ФРАГМЕНТ-ТЕКСТА-ДЛЯ-ПРОВЕРКИ-ДЛИНЫ", Level: 3},
		{Text: " Снова открыто. ", Bold: true},
		{Text: "ДРУГОЙ", Level: 5},
	}
	got := r.render(2)
	raw, _ := json.Marshal(got)
	want := `[{"text":"Открыто. "},{"redacted":true,"level":3},{"text":" Снова открыто. ","bold":true},{"redacted":true,"level":5}]`
	if string(raw) != want {
		t.Errorf("получено %s\nожидалось %s", raw, want)
	}
	if strings.Contains(string(raw), "СЕКРЕТ") || strings.Contains(string(raw), "ДРУГОЙ") {
		t.Error("текст закрытого фрагмента попал в ответ")
	}
	// читатель выше — видит всё
	full, _ := json.Marshal(r.render(5))
	if strings.Contains(string(full), "redacted") || !strings.Contains(string(full), "ДРУГОЙ") {
		t.Errorf("читатель уровня 5 должен видеть всё: %s", full)
	}
}

// Ссылка на закрытый или несуществующий документ неотличима и ничего не раскрывает.
func TestDocLinkDoesNotRevealHiddenOrMissingTargets(t *testing.T) {
	mk := func(code string) []Block {
		var p Problems
		bl := NormalizeBlocks([]InputBlock{{Type: "doc_link", Data: json.RawMessage(`{"code":"` + code + `","note":"Смотрите досье объекта Х — он опасен"}`)}}, &p)
		if p.Any() {
			t.Fatal(p.List())
		}
		return bl
	}
	hidden := ObjectCode(77)
	resolver := func(code string) *LinkTarget {
		switch code {
		case hidden.Canonical:
			return nil // существует, но читатель его не видит
		case "О-078":
			return &LinkTarget{Code: "О-078", Slug: "O-078", Title: "Открытый", Type: TypeObject}
		}
		return nil // не существует
	}

	render := func(code string) string {
		out, err := RenderBlocks(mk(code), 0, 0, resolver)
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := json.Marshal(out)
		return string(raw)
	}
	hiddenOut := render(hidden.Canonical)
	missingOut := render("О-999")
	strip := func(s string) string { // id блока одинаков (b1), различается только шифр в исходных данных
		return s
	}
	if strip(hiddenOut) != strip(missingOut) {
		t.Errorf("закрытая и несуществующая цели различимы:\n%s\n%s", hiddenOut, missingOut)
	}
	for _, leak := range []string{"077", "О-", "опасен", "Смотрите"} {
		if strings.Contains(hiddenOut, leak) {
			t.Errorf("недоступная ссылка раскрывает %q: %s", leak, hiddenOut)
		}
	}
	if !strings.Contains(hiddenOut, `"available":false`) {
		t.Errorf("ожидалась метка недоступности: %s", hiddenOut)
	}
	visible := render("О-078")
	for _, must := range []string{`"available":true`, `"code":"О-078"`, `"slug":"O-078"`, `"title":"Открытый"`, `"type_name":"Объект"`, "опасен"} {
		if !strings.Contains(visible, must) {
			t.Errorf("у доступной ссылки нет %s: %s", must, visible)
		}
	}
}

// Неизвестное поле в сохранённых данных не пропускается в ответ: чтение строгое.
func TestRenderRejectsUnknownStoredFields(t *testing.T) {
	blocks := []Block{{ID: "b1", Type: "paragraph", Data: json.RawMessage(`{"text":[{"text":"открыто"}],"secret":"утечка"}`)}}
	if out, err := RenderBlocks(blocks, 0, 7, nil); err == nil {
		raw, _ := json.Marshal(out)
		t.Errorf("данные с неизвестным полем приняты: %s", raw)
	}
	blocks = []Block{{ID: "b1", Type: "hologram", Data: json.RawMessage(`{}`)}}
	if _, err := RenderBlocks(blocks, 0, 7, nil); err == nil {
		t.Error("неизвестный тип блока принят при чтении")
	}
}

// Собственный уровень блока не может быть ниже уровня документа, а уровень 7 недоступен никому, кроме Директората.
func TestLevelSevenIsDirectorateOnly(t *testing.T) {
	g := newTokenGen(9)
	in := []InputBlock{g.block("paragraph", 7, nil)}
	var p Problems
	blocks := NormalizeBlocks(in, &p)
	if p.Any() {
		t.Fatal(p.List())
	}
	for viewer := 0; viewer <= 6; viewer++ {
		out, _ := RenderBlocks(blocks, 0, viewer, nil)
		raw, _ := json.Marshal(out)
		for tok := range g.levels {
			if strings.Contains(string(raw), tok) {
				t.Errorf("допуск %d видит уровень 7: %s", viewer, tok)
			}
		}
		if !strings.Contains(string(raw), `"level":7`) {
			t.Errorf("допуск %d: нет метки уровня 7: %s", viewer, raw)
		}
	}
	out, _ := RenderBlocks(blocks, 0, 7, nil)
	raw, _ := json.Marshal(out)
	if strings.Contains(string(raw), "redacted") {
		t.Errorf("Директорат не должен видеть меток закрытия: %s", raw)
	}
}
