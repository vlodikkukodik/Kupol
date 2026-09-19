package documents

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"

	"kupol/internal/testutil"
)

var ctx = context.Background()

type env struct {
	t   *testing.T
	svc *Service
	db  *gorm.DB
	now time.Time
	mu  sync.Mutex
}

func newEnv(t *testing.T) *env {
	t.Helper()
	e := &env{t: t, db: testutil.NewMigratedDB(t), now: time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)}
	e.svc = NewService(e.db, testutil.Logger(), e.clock)
	return e
}

func (e *env) clock() time.Time {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.now
}

func (e *env) advance(d time.Duration) {
	e.mu.Lock()
	e.now = e.now.Add(d)
	e.mu.Unlock()
}

// doc собирает JSON документа-объекта; extra — дополнительные поля верхнего уровня (уже с запятой в начале).
func obj(code, title, status string, level int, extra string) string {
	c := ""
	if code != "" {
		c = fmt.Sprintf(`"code": %q,`, code)
	}
	return fmt.Sprintf(`{%s "type":"object","title":%q,"status":%q,"level":%d,"composed":{"year":1979}%s}`, c, title, status, level, extra)
}

func (e *env) imp(raw string, opt ...ImportOptions) *ImportReport {
	e.t.Helper()
	var o ImportOptions
	if len(opt) > 0 {
		o = opt[0]
	}
	rep, err := e.svc.Import(ctx, []byte(raw), o)
	if err != nil {
		e.t.Fatalf("Import: %v\nфайл: %s", err, raw)
	}
	return rep
}

func (e *env) impErr(raw string, opt ...ImportOptions) error {
	e.t.Helper()
	var o ImportOptions
	if len(opt) > 0 {
		o = opt[0]
	}
	_, err := e.svc.Import(ctx, []byte(raw), o)
	if err == nil {
		e.t.Fatalf("ожидалась ошибка загрузки:\n%s", raw)
	}
	return err
}

func (e *env) count(q string, args ...any) int64 {
	e.t.Helper()
	var n int64
	if err := e.db.Raw(q, args...).Scan(&n).Error; err != nil {
		e.t.Fatal(err)
	}
	return n
}

func (e *env) user(login string) int64 {
	e.t.Helper()
	var id int64
	err := e.db.Raw(`INSERT INTO users (login, password_hash, backup_code_hash, created_at, password_changed_at)
		VALUES (?, 'h', 'b', ?, ?) RETURNING id`, login, e.now, e.now).Scan(&id).Error
	if err != nil {
		e.t.Fatal(err)
	}
	return id
}

func viewer(level int) Viewer {
	if level == 0 {
		return Guest
	}
	return Viewer{UserID: int64(1000 + level), UserLevel: level}
}

func problemPaths(err error) []string {
	var ve *ValidationError
	if !errors.As(err, &ve) {
		return nil
	}
	var out []string
	for _, p := range ve.Problems {
		out = append(out, p.Path)
	}
	return out
}

func hasPath(err error, path string) bool {
	for _, p := range problemPaths(err) {
		if p == path {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------- загрузка

func TestImportCreatesAndStoresEverything(t *testing.T) {
	e := newEnv(t)
	author := e.user("Архивариус")
	rep := e.imp(`{
		"code": "o-41", "type": "object", "title": "Объект «Купол»", "status": "published", "level": 2,
		"direct_link": "forbidden", "grif": "Форма КУПОЛ-1",
		"composed": {"year": 1979, "month": 3, "day": 14},
		"props": {"danger_class": 3, "deviation_points": 12, "department": "otd-2", "category": "entity",
		          "containment_status": "contained", "discovery_place": "Полигон №7"},
		"blocks": [{"type": "dossier_header"}, {"type": "paragraph", "data": {"text": "Описание."}}]
	}`, ImportOptions{AuthorLogin: "Архивариус"})

	if len(rep.Items) != 1 {
		t.Fatalf("элементов %d", len(rep.Items))
	}
	it := rep.Items[0]
	if it.Code != "О-041" || it.Slug != "O-041" || !it.Created || it.AssignedCode || it.Revision != 1 || it.Status != "published" {
		t.Errorf("итог: %+v", it)
	}

	var d Document
	if err := e.db.Where("slug = ?", "O-041").Take(&d).Error; err != nil {
		t.Fatal(err)
	}
	if d.Level != 2 || d.DirectLink != "forbidden" || d.Grif != "Форма КУПОЛ-1" || d.ComposedYear != 1979 ||
		*d.ComposedMonth != 3 || *d.ComposedDay != 14 || *d.DangerClass != 3 || *d.DeviationPoints != 12 ||
		*d.Department != "ОТД-2" || *d.Category != "entity" || *d.ContainmentStatus != "contained" ||
		*d.DiscoveryPlace != "Полигон №7" || *d.ObjectNumber != 41 || d.AuthorID == nil || *d.AuthorID != author ||
		len(d.Blocks) != 2 || d.PublishedAt == nil || !d.PublishedAt.Equal(e.now) || !d.CreatedAt.Equal(e.now) {
		t.Errorf("сохранено неверно: %+v", d)
	}
}

func TestImportUpdatesInPlaceAndKeepsHistoryFields(t *testing.T) {
	e := newEnv(t)
	author := e.user("Автор")
	e.imp(obj("О-1", "Первая редакция", "published", 0, ""), ImportOptions{AuthorLogin: "Автор"})
	published := e.clock()

	e.advance(48 * time.Hour)
	rep := e.imp(obj("О-1", "Вторая редакция", "published", 0, ""))
	it := rep.Items[0]
	if it.Created || it.Revision != 2 {
		t.Errorf("обновление: %+v", it)
	}

	var d Document
	e.db.Where("slug = 'O-001'").Take(&d)
	if d.Title != "Вторая редакция" || d.Revision != 2 {
		t.Errorf("обновление не применено: %+v", d)
	}
	if !d.CreatedAt.Equal(published) || !d.PublishedAt.Equal(published) {
		t.Errorf("created_at и published_at должны сохраниться: %v / %v", d.CreatedAt, d.PublishedAt)
	}
	if !d.UpdatedAt.Equal(e.clock()) {
		t.Errorf("updated_at: %v", d.UpdatedAt)
	}
	if d.AuthorID == nil || *d.AuthorID != author {
		t.Error("автор должен сохраниться, если при повторной загрузке не указан другой")
	}
	if n := e.count("SELECT count(*) FROM documents"); n != 1 {
		t.Errorf("документов %d: обновление не должно создавать копии", n)
	}
}

func TestImportRoundTripThroughExport(t *testing.T) {
	e := newEnv(t)
	e.imp(`{"documents": [
		{"code":"О-7","type":"object","title":"Объект","status":"published","level":1,"composed":{"year":1979,"month":5},
		 "props":{"danger_class":2,"category":"place","department":"ОТД-2"},
		 "blocks":[
		   {"id":"head","type":"dossier_header"},
		   {"id":"intro","type":"paragraph","level":0,"data":{"text":[{"text":"Открыто. "},{"text":"секрет","level":4,"bold":true}]}},
		   {"type":"table","level":3,"data":{"caption":"П.о.","columns":["Дата","П.о."],"rows":[["1979","12"],["1980",""]]}},
		   {"type":"doc_link","data":{"code":"приказ-1978-2","note":"См. также"}}
		 ]},
		{"code":"ПРИКАЗ-1978-2","type":"order","title":"Приказ","status":"published","composed":{"year":1978}},
		{"code":"ОТД-2","type":"unit","title":"Отдел 2","status":"published","composed":{"year":1975}}
	]}`)

	first, err := e.svc.Export(ctx, "O-007")
	if err != nil {
		t.Fatal(err)
	}
	// загрузка выгрузки без изменений: тот же документ, редакция выросла
	rep := e.imp(string(first))
	if rep.Items[0].Created || rep.Items[0].Revision != 2 {
		t.Errorf("повторная загрузка выгрузки: %+v", rep.Items[0])
	}
	second, err := e.svc.Export(ctx, "О-007")
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Errorf("выгрузка изменилась после загрузки:\n%s\n---\n%s", first, second)
	}
	// в выгрузке — канонический вид: строки стали фрагментами, шифры канонизированы, id сохранены
	for _, must := range []string{`"code": "О-007"`, `"id": "intro"`, `"id": "head"`, `"code": "ПРИКАЗ-1978-02"`, `"department": "ОТД-2"`, `"bold": true`} {
		if !strings.Contains(string(first), must) {
			t.Errorf("в выгрузке нет %s:\n%s", must, first)
		}
	}
}

func TestImportBatchIsAtomic(t *testing.T) {
	e := newEnv(t)
	err := e.impErr(`{"documents": [
		` + obj("О-1", "Хороший", "published", 0, "") + `,
		{"code":"О-2","type":"object","title":"","composed":{"year":1979},"blocks":[{"type":"hologram"}]}
	]}`)
	if n := e.count("SELECT count(*) FROM documents"); n != 0 {
		t.Errorf("сохранено %d документов, хотя пакет с ошибкой", n)
	}
	for _, p := range []string{"documents[1].title", "documents[1].blocks[0].type"} {
		if !hasPath(err, p) {
			t.Errorf("нет замечания %s: %v", p, problemPaths(err))
		}
	}

	// ошибка на этапе сохранения (несуществующий автор) откатывает и первые документы
	if err := e.impErr(obj("О-3", "Т", "published", 0, ""), ImportOptions{AuthorLogin: "нет-такого"}); !strings.Contains(err.Error(), "не найден") {
		t.Errorf("сообщение: %v", err)
	}
	if n := e.count("SELECT count(*) FROM documents"); n != 0 {
		t.Errorf("после ошибки автора сохранено %d", n)
	}
}

func TestImportValidationErrors(t *testing.T) {
	e := newEnv(t)
	cases := map[string]struct{ raw, path string }{
		"неизвестный тип":            {`{"code":"О-1","type":"spaceship","title":"Т","composed":{"year":1979}}`, "type"},
		"неизвестный статус":         {`{"code":"О-1","type":"object","title":"Т","status":"hidden","composed":{"year":1979}}`, "status"},
		"шифр другого типа":          {`{"code":"ПРИКАЗ-1978-1","type":"object","title":"Т","composed":{"year":1979}}`, "code"},
		"не шифр":                    {`{"code":"чепуха","type":"object","title":"Т","composed":{"year":1979}}`, "code"},
		"приказ без шифра":           {`{"type":"order","title":"Т","status":"published","composed":{"year":1979}}`, "code"},
		"черновик объекта без шифра": {`{"type":"object","title":"Т","composed":{"year":1979}}`, "code"},
		"пустое название":            {`{"code":"О-1","type":"object","title":"","composed":{"year":1979}}`, "title"},
		"уровень 8":                  {`{"code":"О-1","type":"object","title":"Т","level":8,"composed":{"year":1979}}`, "level"},
		"режим ссылки":               {`{"code":"О-1","type":"object","title":"Т","direct_link":"maybe","composed":{"year":1979}}`, "direct_link"},
		"нет даты":                   {`{"code":"О-1","type":"object","title":"Т"}`, "composed"},
		"год 1800":                   {`{"code":"О-1","type":"object","title":"Т","composed":{"year":1800}}`, "composed.year"},
		"месяц 13":                   {`{"code":"О-1","type":"object","title":"Т","composed":{"year":1979,"month":13}}`, "composed.month"},
		"день без месяца":            {`{"code":"О-1","type":"object","title":"Т","composed":{"year":1979,"day":5}}`, "composed.day"},
		"31 февраля":                 {`{"code":"О-1","type":"object","title":"Т","composed":{"year":1979,"month":2,"day":31}}`, "composed.day"},
		"29 февраля невисокосного":   {`{"code":"О-1","type":"object","title":"Т","composed":{"year":1979,"month":2,"day":29}}`, "composed.day"},
		"класс 6":                    {`{"code":"О-1","type":"object","title":"Т","composed":{"year":1979},"props":{"danger_class":6}}`, "props.danger_class"},
		"отрицательные п.о.":         {`{"code":"О-1","type":"object","title":"Т","composed":{"year":1979},"props":{"deviation_points":-5}}`, "props.deviation_points"},
		"отдел не отдел":             {`{"code":"О-1","type":"object","title":"Т","composed":{"year":1979},"props":{"department":"О-41"}}`, "props.department"},
		"категория":                  {`{"code":"О-1","type":"object","title":"Т","composed":{"year":1979},"props":{"category":"ghost"}}`, "props.category"},
		"статус содержания":          {`{"code":"О-1","type":"object","title":"Т","composed":{"year":1979},"props":{"containment_status":"escaped"}}`, "props.containment_status"},
		"свойства у приказа":         {`{"code":"ПРИКАЗ-1978-1","type":"order","title":"Т","composed":{"year":1978},"props":{"danger_class":3}}`, "props"},
		"неизвестное поле":           {`{"code":"О-1","type":"object","title":"Т","composed":{"year":1979},"colour":"red"}`, "$"},
		"блок с ошибкой":             {`{"code":"О-1","type":"object","title":"Т","composed":{"year":1979},"blocks":[{"type":"heading","data":{"depth":9,"text":"З"}}]}`, "blocks[0].data.depth"},
	}
	for name, c := range cases {
		err := e.impErr(c.raw)
		if !hasPath(err, c.path) {
			t.Errorf("%s: нет замечания %q среди %v (%v)", name, c.path, problemPaths(err), err)
		}
	}
	if n := e.count("SELECT count(*) FROM documents"); n != 0 {
		t.Errorf("сохранено %d документов при ошибках", n)
	}

	// файл целиком: пустой, не JSON, слишком большой, массив вместо объекта
	for name, raw := range map[string]string{"пустой": "", "пробелы": "  \n", "не JSON": "{", "массив": "[]", "число": "5"} {
		if _, err := e.svc.Import(ctx, []byte(raw), ImportOptions{}); err == nil {
			t.Errorf("%s: ожидалась ошибка", name)
		}
	}
	if _, err := e.svc.Import(ctx, make([]byte, MaxInputBytes+1), ImportOptions{}); err == nil {
		t.Error("слишком большой файл принят")
	}
	if _, err := e.svc.Import(ctx, []byte(`{"documents": []}`), ImportOptions{}); err == nil {
		t.Error("пустой пакет принят")
	}
	if _, err := e.svc.Import(ctx, []byte(`{"documents": [], "extra": 1}`), ImportOptions{}); err == nil {
		t.Error("пакет с лишними полями принят")
	}
}

func TestImportRejectsDuplicateCodesInOneFile(t *testing.T) {
	e := newEnv(t)
	err := e.impErr(`{"documents": [` + obj("О-1", "А", "published", 0, "") + `,` + obj("o-01", "Б", "published", 0, "") + `]}`)
	if !hasPath(err, "documents[1].code") {
		t.Errorf("дубль шифра в пакете: %v", problemPaths(err))
	}
}

func TestImportDryRunSavesNothing(t *testing.T) {
	e := newEnv(t)
	rep := e.imp(obj("", "Новый объект", "published", 0, ""), ImportOptions{DryRun: true})
	if !rep.DryRun || len(rep.Items) != 1 || rep.Items[0].Code != "О-001" || !rep.Items[0].AssignedCode {
		t.Errorf("отчёт пробного прогона: %+v", rep)
	}
	if n := e.count("SELECT count(*) FROM documents"); n != 0 {
		t.Errorf("пробный прогон сохранил %d документов", n)
	}
	// настоящая загрузка после пробной получает тот же номер
	if got := e.imp(obj("", "Новый объект", "published", 0, "")).Items[0].Code; got != "О-001" {
		t.Errorf("номер после пробного прогона: %s", got)
	}
}

func TestImportUnknownAuthor(t *testing.T) {
	e := newEnv(t)
	err := e.impErr(obj("О-1", "Т", "published", 0, ""), ImportOptions{AuthorLogin: "призрак"})
	if !strings.Contains(err.Error(), `"призрак" не найден`) {
		t.Errorf("сообщение: %v", err)
	}
}

func TestImportWarnsAboutMissingTargets(t *testing.T) {
	e := newEnv(t)
	rep := e.imp(`{"code":"О-1","type":"object","title":"Т","status":"published","composed":{"year":1979},
		"props":{"department":"ОТД-9"},
		"blocks":[{"type":"doc_link","data":{"code":"О-99"}},{"type":"doc_link","data":{"code":"ПРИКАЗ-1978-1"}}]}`)
	w := strings.Join(rep.Items[0].Warnings, "\n")
	for _, must := range []string{"О-099", "ПРИКАЗ-1978-01", "ОТД-9"} {
		if !strings.Contains(w, must) {
			t.Errorf("нет предупреждения про %s:\n%s", must, w)
		}
	}
	// в пакете документы могут ссылаться друг на друга в любом порядке — без предупреждений
	rep = e.imp(`{"documents":[
		{"code":"О-2","type":"object","title":"А","status":"published","composed":{"year":1979},"blocks":[{"type":"doc_link","data":{"code":"О-3"}}]},
		` + obj("О-3", "Б", "published", 0, "") + `]}`)
	for _, it := range rep.Items {
		if len(it.Warnings) != 0 {
			t.Errorf("%s: лишние предупреждения %v", it.Code, it.Warnings)
		}
	}
}

// ---------------------------------------------------------------- номера О-№

func TestObjectNumbersAreAssignedInOrder(t *testing.T) {
	e := newEnv(t)
	var got []string
	for i := 0; i < 3; i++ {
		got = append(got, e.imp(obj("", fmt.Sprintf("Объект %d", i), "published", 0, "")).Items[0].Code)
	}
	if strings.Join(got, ",") != "О-001,О-002,О-003" {
		t.Errorf("автономера: %v", got)
	}
	// явный номер сдвигает счёт: дальше — следующий после наибольшего
	e.imp(obj("О-041", "Явный", "published", 0, ""))
	if c := e.imp(obj("", "После явного", "published", 0, "")).Items[0]; c.Code != "О-042" || !c.AssignedCode {
		t.Errorf("после О-041: %+v", c)
	}
	// О-0 (Праисточник) — вне общей нумерации: автоматически не занимается и счёт не сдвигает
	e2 := newEnv(t)
	e2.imp(obj("О-0", "Праисточник", "published", 7, ""))
	if c := e2.imp(obj("", "Первый обычный", "published", 0, "")).Items[0].Code; c != "О-001" {
		t.Errorf("после О-0 первый автономер: %s", c)
	}
	var d Document
	e2.db.Where("slug = 'O-0'").Take(&d)
	if d.Code == nil || *d.Code != "О-0" || *d.ObjectNumber != 0 {
		t.Errorf("Праисточник: %+v", d)
	}
}

func TestObjectNumbersAreUniqueUnderConcurrentImports(t *testing.T) {
	e := newEnv(t)
	const n = 12
	codes := make(chan string, n)
	errs := make(chan error, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			rep, err := e.svc.Import(ctx, []byte(obj("", fmt.Sprintf("Параллельный %d", i), "published", 0, "")), ImportOptions{})
			if err != nil {
				errs <- err
				return
			}
			codes <- rep.Items[0].Code
		}(i)
	}
	wg.Wait()
	close(codes)
	close(errs)
	for err := range errs {
		t.Errorf("ошибка параллельной загрузки: %v", err)
	}
	seen := map[string]bool{}
	for c := range codes {
		if seen[c] {
			t.Errorf("номер %s выдан дважды", c)
		}
		seen[c] = true
	}
	if len(seen) != n {
		t.Fatalf("выдано %d номеров из %d", len(seen), n)
	}
	for i := 1; i <= n; i++ {
		if !seen[ObjectCode(i).Canonical] {
			t.Errorf("в нумерации есть дыра: нет %s", ObjectCode(i).Canonical)
		}
	}
}

// ---------------------------------------------------------------- статус и удаление

func TestSetStatusAndPublicationDate(t *testing.T) {
	e := newEnv(t)
	e.imp(obj("О-1", "Т", "draft", 0, ""))
	if _, err := e.svc.Get(ctx, viewer(0), "О-1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("черновик виден гостю: %v", err)
	}

	e.advance(time.Hour)
	d, err := e.svc.SetStatus(ctx, "o-1", StatusPublished)
	if err != nil {
		t.Fatal(err)
	}
	firstPublished := *d.PublishedAt
	if !firstPublished.Equal(e.clock()) {
		t.Errorf("published_at: %v", firstPublished)
	}
	if _, err := e.svc.Get(ctx, viewer(0), "О-1"); err != nil {
		t.Errorf("опубликованный документ недоступен: %v", err)
	}

	// снять с публикации и опубликовать снова: дата поступления остаётся первой
	e.advance(time.Hour)
	if _, err := e.svc.SetStatus(ctx, "О-1", StatusArchived); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Get(ctx, viewer(6), "О-1"); !errors.Is(err, ErrNotFound) {
		t.Errorf("архивный документ виден читателю: %v", err)
	}
	e.advance(time.Hour)
	d, _ = e.svc.SetStatus(ctx, "О-1", StatusPublished)
	if !d.PublishedAt.Equal(firstPublished) {
		t.Errorf("повторная публикация изменила дату поступления: %v", d.PublishedAt)
	}

	if _, err := e.svc.SetStatus(ctx, "О-1", Status("hidden")); err == nil {
		t.Error("неизвестный статус принят")
	}
	if _, err := e.svc.SetStatus(ctx, "О-999", StatusPublished); !errors.Is(err, ErrNotFound) {
		t.Errorf("несуществующий документ: %v", err)
	}
	if _, err := e.svc.SetStatus(ctx, "чепуха", StatusPublished); !errors.Is(err, ErrNotFound) {
		t.Errorf("не шифр: %v", err)
	}
}

func TestDeleteRemovesDocumentAndReads(t *testing.T) {
	e := newEnv(t)
	uid := e.user("Читатель")
	e.imp(obj("О-1", "Т", "published", 0, ""))
	if _, err := e.svc.Get(ctx, Viewer{UserID: uid, UserLevel: 1}, "О-1"); err != nil {
		t.Fatal(err)
	}
	if n := e.count("SELECT count(*) FROM document_reads"); n != 1 {
		t.Fatalf("чтений %d", n)
	}
	if err := e.svc.Delete(ctx, "o-1"); err != nil {
		t.Fatal(err)
	}
	if n := e.count("SELECT count(*) FROM documents"); n != 0 {
		t.Error("документ не удалён")
	}
	if n := e.count("SELECT count(*) FROM document_reads"); n != 0 {
		t.Error("история чтения осталась")
	}
	if err := e.svc.Delete(ctx, "О-1"); !errors.Is(err, ErrNotFound) {
		t.Errorf("повторное удаление: %v", err)
	}
	if _, err := e.svc.Export(ctx, "О-1"); !errors.Is(err, ErrNotFound) {
		t.Errorf("выгрузка удалённого: %v", err)
	}
}

// ---------------------------------------------------------------- видимость

type visCase struct {
	code, status string
	level        int
	link         string // not_found | forbidden
}

// Матрица: статус × уровень × режим ссылки × читатель. Каждое решение сверено с правилами Get.
func TestGetVisibilityMatrix(t *testing.T) {
	e := newEnv(t)
	var cases []visCase
	n := 0
	for _, status := range []string{"published", "draft", "review", "archived"} {
		for _, level := range []int{0, 1, 3, 6, 7} {
			for _, link := range []string{"not_found", "forbidden"} {
				n++
				cases = append(cases, visCase{fmt.Sprintf("О-%d", n), status, level, link})
			}
		}
	}
	for _, c := range cases {
		e.imp(obj(c.code, "Документ "+c.code, c.status, c.level, fmt.Sprintf(`,"direct_link":%q`, c.link)))
	}

	viewers := []struct {
		name string
		v    Viewer
	}{
		{"Гражданин", Guest}, {"уровень 1", viewer(1)}, {"уровень 3", viewer(3)}, {"уровень 6", viewer(6)},
		{"Директорат", Viewer{UserID: 9000, UserLevel: 1, Directorate: true}},
	}

	for _, c := range cases {
		for _, vw := range viewers {
			doc, err := e.svc.Get(ctx, vw.v, c.code)
			label := fmt.Sprintf("%s / статус %s / уровень %d / %s / читатель %s", c.code, c.status, c.level, c.link, vw.name)

			var wantOK bool
			var wantDenied int // 0 — не «доступ запрещён»
			switch {
			case vw.v.Directorate:
				wantOK = true // Директорат видит всё, включая неопубликованное и уровень 7
			case c.status != "published":
				wantOK = false // неопубликованное — 404 всем, независимо от режима ссылки
			case c.level <= vw.v.Level():
				wantOK = true
			case c.link == "forbidden":
				wantDenied = c.level
			}

			switch {
			case wantOK:
				if err != nil || doc == nil {
					t.Errorf("%s: ожидался доступ, получено %v", label, err)
				}
			case wantDenied > 0:
				var ad *AccessDeniedError
				if !errors.As(err, &ad) || ad.RequiredLevel != wantDenied || doc != nil {
					t.Errorf("%s: ожидалось «Доступ запрещён» (уровень %d), получено %v", label, wantDenied, err)
				}
			default:
				if !errors.Is(err, ErrNotFound) || doc != nil {
					t.Errorf("%s: ожидалось «не найдено», получено %v", label, err)
				}
			}
		}
	}
}

func TestGetDoesNotRevealDraftsThroughForbiddenFlag(t *testing.T) {
	e := newEnv(t)
	e.imp(obj("О-1", "Черновик", "draft", 5, `,"direct_link":"forbidden"`))
	for _, v := range []Viewer{Guest, viewer(1), viewer(6)} {
		_, err := e.svc.Get(ctx, v, "О-1")
		var ad *AccessDeniedError
		if errors.As(err, &ad) || !errors.Is(err, ErrNotFound) {
			t.Errorf("черновик выдал %v: существование черновика раскрывается", err)
		}
	}
}

func TestGetAcceptsAnyCodeForm(t *testing.T) {
	e := newEnv(t)
	e.imp(obj("О-41", "Т", "published", 0, ""))
	for _, ref := range []string{"О-041", "O-041", "o-41", "  о-0041 ", "О–041"} {
		d, err := e.svc.Get(ctx, Guest, ref)
		if err != nil || d.Code != "О-041" || d.Slug != "O-041" {
			t.Errorf("%q: %v %+v", ref, err, d)
		}
	}
	for _, ref := range []string{"", "О-042", "чепуха", "О-041/../", "О-99999"} {
		if _, err := e.svc.Get(ctx, Guest, ref); !errors.Is(err, ErrNotFound) {
			t.Errorf("%q: %v", ref, err)
		}
	}
}

func TestGetFiltersBlocksByViewer(t *testing.T) {
	e := newEnv(t)
	e.imp(`{"code":"О-1","type":"object","title":"Т","status":"published","level":0,"composed":{"year":1979},"blocks":[
		{"id":"open","type":"paragraph","data":{"text":"ОТКРЫТО-ВСЕМ"}},
		{"id":"sec3","type":"paragraph","level":3,"data":{"text":"СЕКРЕТ-УРОВНЯ-3"}},
		{"id":"mix","type":"paragraph","data":{"text":[{"text":"открытая часть, "},{"text":"СЕКРЕТ-ФРАГМЕНТ-5","level":5}]}},
		{"id":"sec7","type":"stamp","level":7,"data":{"text":"ТОЛЬКО-ДИРЕКТОРАТ"}}
	]}`)
	body := func(v Viewer) string {
		d, err := e.svc.Get(ctx, v, "О-1")
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := json.Marshal(d)
		return string(raw)
	}
	guest := body(Guest)
	for _, must := range []string{"ОТКРЫТО-ВСЕМ", "открытая часть", `"redacted":true`, `"level":3`, `"level":5`, `"level":7`} {
		if !strings.Contains(guest, must) {
			t.Errorf("гость: нет %q в %s", must, guest)
		}
	}
	for _, mustNot := range []string{"СЕКРЕТ-УРОВНЯ-3", "СЕКРЕТ-ФРАГМЕНТ-5", "ТОЛЬКО-ДИРЕКТОРАТ", `"id":"sec3"`, `"id":"sec7"`} {
		if strings.Contains(guest, mustNot) {
			t.Errorf("гость видит %q: %s", mustNot, guest)
		}
	}
	l4 := body(viewer(4))
	if !strings.Contains(l4, "СЕКРЕТ-УРОВНЯ-3") || strings.Contains(l4, "СЕКРЕТ-ФРАГМЕНТ-5") || strings.Contains(l4, "ТОЛЬКО-ДИРЕКТОРАТ") {
		t.Errorf("уровень 4: %s", l4)
	}
	dir := body(Viewer{UserID: 1, UserLevel: 1, Directorate: true})
	for _, must := range []string{"СЕКРЕТ-УРОВНЯ-3", "СЕКРЕТ-ФРАГМЕНТ-5", "ТОЛЬКО-ДИРЕКТОРАТ"} {
		if !strings.Contains(dir, must) {
			t.Errorf("Директорат не видит %q", must)
		}
	}
	if strings.Contains(dir, "redacted") {
		t.Error("у Директората ничего не закрыто")
	}
}

func TestDocumentLevelHidesBlocksFromDirectorateOnlyDocs(t *testing.T) {
	e := newEnv(t)
	e.imp(`{"code":"О-0","type":"object","title":"Праисточник","status":"published","level":7,"direct_link":"forbidden","composed":{"year":1974},
		"blocks":[{"type":"paragraph","data":{"text":"ГЛУБОЧАЙШИЙ-СЕКРЕТ"}}]}`)
	for _, lvl := range []int{0, 1, 3, 6} {
		_, err := e.svc.Get(ctx, viewer(lvl), "О-0")
		var ad *AccessDeniedError
		if !errors.As(err, &ad) || ad.RequiredLevel != 7 {
			t.Errorf("уровень %d: %v", lvl, err)
		}
	}
	d, err := e.svc.Get(ctx, Viewer{UserID: 1, Directorate: true}, "О-0")
	if err != nil || !strings.Contains(fmt.Sprint(d.Blocks[0].Data), "ГЛУБОЧАЙШИЙ-СЕКРЕТ") {
		t.Errorf("Директорат: %v %+v", err, d)
	}
}

func TestGetResolvesLinksByViewer(t *testing.T) {
	e := newEnv(t)
	e.imp(`{"documents":[
		{"code":"О-1","type":"object","title":"Источник","status":"published","composed":{"year":1979},"blocks":[
			{"id":"l-open","type":"doc_link","data":{"code":"О-2","note":"открытая"}},
			{"id":"l-l5","type":"doc_link","data":{"code":"О-3","note":"закрытая уровнем 5"}},
			{"id":"l-draft","type":"doc_link","data":{"code":"О-4","note":"черновик"}},
			{"id":"l-missing","type":"doc_link","data":{"code":"О-9","note":"несуществующая"}}
		]},
		` + obj("О-2", "Открытый документ", "published", 0, "") + `,
		` + obj("О-3", "Документ уровня 5", "published", 5, "") + `,
		` + obj("О-4", "Черновик", "draft", 0, "") + `
	]}`)

	links := func(v Viewer) map[string]OutDocLink {
		d, err := e.svc.Get(ctx, v, "О-1")
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]OutDocLink{}
		for _, b := range d.Blocks {
			if b.Type == "doc_link" {
				out[b.ID] = b.Data.(OutDocLink)
			}
		}
		return out
	}

	g := links(Guest)
	if !g["l-open"].Available || g["l-open"].Title != "Открытый документ" || g["l-open"].Code != "О-002" || g["l-open"].Slug != "O-002" {
		t.Errorf("открытая цель: %+v", g["l-open"])
	}
	for _, id := range []string{"l-l5", "l-draft", "l-missing"} {
		if g[id] != (OutDocLink{}) {
			t.Errorf("гость: ссылка %s раскрывает %+v", id, g[id])
		}
	}
	// закрытая, черновик и несуществующая неотличимы
	if g["l-l5"] != g["l-missing"] || g["l-draft"] != g["l-missing"] {
		t.Error("недоступные цели различимы")
	}

	l5 := links(viewer(5))
	if !l5["l-l5"].Available || l5["l-l5"].Title != "Документ уровня 5" {
		t.Errorf("уровень 5 не видит цель уровня 5: %+v", l5["l-l5"])
	}
	if l5["l-draft"].Available {
		t.Error("черновик виден по ссылке читателю")
	}
	dir := links(Viewer{UserID: 1, Directorate: true})
	if !dir["l-draft"].Available || dir["l-missing"].Available {
		t.Errorf("Директорат: черновик %v, несуществующая %v", dir["l-draft"].Available, dir["l-missing"].Available)
	}
}

func TestGetAuthorAndMetadata(t *testing.T) {
	e := newEnv(t)
	e.user("Архивариус")
	e.imp(`{"code":"О-1","type":"object","title":"Т","status":"published","composed":{"year":1979,"month":3},
		"props":{"danger_class":4,"deviation_points":80,"department":"ОБ-14","category":"person","containment_status":"lost","discovery_place":"Шахта"}}`,
		ImportOptions{AuthorLogin: "Архивариус"})
	d, err := e.svc.Get(ctx, Guest, "О-1")
	if err != nil {
		t.Fatal(err)
	}
	if d.Author == nil || *d.Author != "Архивариус" || d.TypeName != "Объект" || d.Grif != "Форма КУПОЛ-1" ||
		d.Composed.Year != 1979 || *d.Composed.Month != 3 || d.Composed.Day != nil ||
		*d.DangerClass != 4 || *d.DeviationPoints != 80 || *d.Department != "ОБ-14" ||
		d.CategoryName != "Человек с аномальными свойствами" || d.ContainmentName != "Утрачен" || *d.DiscoveryPlace != "Шахта" {
		t.Errorf("метаданные: %+v", d)
	}
	if d.Status != "" {
		t.Errorf("читателю не показывается статус: %q", d.Status)
	}
	raw, _ := json.Marshal(d)
	if strings.Contains(string(raw), "published_at") || strings.Contains(string(raw), "created_at") {
		t.Errorf("реальные даты в ответе читателю: %s", raw)
	}

	// автор удалил аккаунт — документ остаётся, ник исчезает
	e.db.Exec("DELETE FROM users WHERE login = 'Архивариус'")
	d, _ = e.svc.Get(ctx, Guest, "О-1")
	if d.Author != nil {
		t.Errorf("автор после удаления аккаунта: %v", *d.Author)
	}
}

func TestReadsAreRecordedForUsersOnly(t *testing.T) {
	e := newEnv(t)
	uid := e.user("Читатель")
	e.imp(obj("О-1", "Т", "published", 0, ""))
	e.imp(obj("О-2", "Черновик", "draft", 0, ""))
	e.imp(obj("О-3", "Закрытый", "published", 5, ""))

	e.svc.Get(ctx, Guest, "О-1")
	if n := e.count("SELECT count(*) FROM document_reads"); n != 0 {
		t.Error("чтение гостя записано")
	}
	u := Viewer{UserID: uid, UserLevel: 1}
	e.svc.Get(ctx, u, "О-1")
	e.advance(time.Hour)
	e.svc.Get(ctx, u, "О-1")
	e.svc.Get(ctx, u, "О-3")                                      // нет допуска — не чтение
	e.svc.Get(ctx, Viewer{UserID: uid, Directorate: true}, "О-2") // черновик — не чтение

	var count int
	var first, last time.Time
	e.db.Raw("SELECT read_count, first_read_at, last_read_at FROM document_reads WHERE user_id = ?", uid).Row().Scan(&count, &first, &last)
	if count != 2 || !last.After(first) {
		t.Errorf("чтения: count=%d first=%v last=%v", count, first, last)
	}
	if n := e.count("SELECT count(*) FROM document_reads"); n != 1 {
		t.Errorf("записей о чтении %d, ожидалась 1 (закрытые и черновики не считаются)", n)
	}
}
