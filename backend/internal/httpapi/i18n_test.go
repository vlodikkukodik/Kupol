package httpapi_test

import (
	"strings"
	"testing"
)

// Язык ответа задаёт заголовок Accept-Language (его шлёт интерфейс): без него и с неизвестным языком — русский, с итальянским —
// итальянский. Меняются сообщения об ошибках, названия уровней, типов, статусов, ролей и замечания к документу; данные
// (названия документов, тексты блоков, логины) не трогаются никогда.

func hasCyrillic(s string) bool {
	for _, r := range s {
		if (r >= 'А' && r <= 'я') || r == 'Ё' || r == 'ё' {
			return true
		}
	}
	return false
}

func TestErrorMessagesFollowAcceptLanguage(t *testing.T) {
	st := newStack(t, nil)
	guest := st.newClient(t)

	ru := guest.do("GET", "/api/team/documents", nil)
	if ru.Code != 401 || ru.json()["error"].(map[string]any)["message"] != "Требуется вход" {
		t.Fatalf("по умолчанию русский: %d %s", ru.Code, ru.Body)
	}
	if ru.Header().Get("Content-Language") != "ru-RU" || !strings.Contains(ru.Header().Get("Vary"), "Accept-Language") {
		t.Errorf("заголовки языка: %q, %q", ru.Header().Get("Content-Language"), ru.Header().Get("Vary"))
	}

	guest.lang = "it-IT,it;q=0.9,en;q=0.8"
	it := guest.do("GET", "/api/team/documents", nil)
	if it.Code != 401 || it.json()["error"].(map[string]any)["message"] != "Accesso richiesto" {
		t.Fatalf("итальянский: %d %s", it.Code, it.Body)
	}
	if it.Header().Get("Content-Language") != "it-IT" {
		t.Errorf("Content-Language: %q", it.Header().Get("Content-Language"))
	}
	// код ошибки от языка не зависит: интерфейс ориентируется на него
	if it.errCode() != ru.errCode() {
		t.Errorf("код ошибки изменился: %s и %s", it.errCode(), ru.errCode())
	}

	guest.lang = "de-DE,fr;q=0.5" // неизвестные языки — русский
	if de := guest.do("GET", "/api/team/documents", nil); de.json()["error"].(map[string]any)["message"] != "Требуется вход" {
		t.Errorf("неизвестный язык: %s", de.Body)
	}

	// 404 роутера и 405 тоже
	guest.lang = "it"
	if r := guest.do("GET", "/api/net-takogo", nil); r.json()["error"].(map[string]any)["message"] != "Fascicolo non trovato" {
		t.Errorf("404: %s", r.Body)
	}
	if r := guest.do("POST", "/api/health", nil); r.json()["error"].(map[string]any)["message"] != "Metodo non supportato" {
		t.Errorf("405: %s", r.Body)
	}
}

func TestFormFieldErrorsAndCaptchaAreItalian(t *testing.T) {
	st := newStack(t, nil)
	c := st.newClient(t)
	c.lang = "it"

	// вопрос анкеты — по-итальянски, и на него отвечают по-итальянски
	cp := c.do("GET", "/api/auth/captcha", nil)
	q := cp.json()["question"].(string)
	if cp.Code != 200 || hasCyrillic(strings.ReplaceAll(q, "«KUPOL»", "")) || q == "" {
		t.Fatalf("вопрос анкеты: %d %q", cp.Code, q)
	}

	// ошибки полей формы входа
	r := c.do("POST", "/api/auth/login", map[string]any{"login": "", "password": ""})
	if r.Code != 422 {
		t.Fatalf("вход без данных: %d %s", r.Code, r.Body)
	}
	if f := r.fields(); f["login"] != "Inserisci il login" || f["password"] != "Inserisci la password" {
		t.Errorf("поля: %v", f)
	}
	if msg := r.json()["error"].(map[string]any)["message"]; msg != "Controlla i campi del modulo" {
		t.Errorf("сообщение: %v", msg)
	}
}

func TestNamesAndProblemsFollowLanguageInTeamPanel(t *testing.T) {
	_, actors := teamStackWithActors(t)
	author, director := actors["author"], actors["director"]
	author.client.lang, director.client.lang = "it", "it"

	// справочник форм, звание и роли — по-итальянски
	meta := author.client.do("GET", "/api/team/document-types", nil)
	if meta.Code != 200 {
		t.Fatalf("справочник: %d %s", meta.Code, meta.Body)
	}
	types := meta.json()["types"].([]any)
	if first := types[0].(map[string]any); first["id"] != "object" || first["name"] != "Oggetto" {
		t.Errorf("тип: %v", first)
	}
	statuses := meta.json()["statuses"].([]any)
	if first := statuses[0].(map[string]any); first["name"] != "Bozza" {
		t.Errorf("статус: %v", first)
	}

	sess := director.client.do("GET", "/api/auth/session", nil)
	if u := sess.json()["user"].(map[string]any); u["level_name"] != "Direttorato" {
		t.Errorf("звание Директората: %v", u)
	}
	roles := author.client.do("GET", "/api/team/roles", nil)
	first := roles.json()["roles"].([]any)[0].(map[string]any)
	if first["name"] != "Autore" {
		t.Errorf("роль: %v", first)
	}
	caps := first["capabilities"].([]any)
	if len(caps) == 0 || hasCyrillic(caps[0].(map[string]any)["name"].(string)) {
		t.Errorf("права роли: %v", first)
	}

	// замечания к содержимому — на языке читателя, путь и код остаются прежними
	bad := author.client.do("POST", docsPath, map[string]any{
		"type": "memo", "code": "МЕМО-7", "title": "", "composed": map[string]any{"year": 1979},
		"blocks": []any{map[string]any{"id": "b1", "type": "paragraph", "data": map[string]any{"text": ""}}},
	})
	if bad.Code != 422 || bad.errCode() != "validation" {
		t.Fatalf("пустое название: %d %s", bad.Code, bad.Body)
	}
	e := bad.json()["error"].(map[string]any)
	if e["message"] != "Controlla il contenuto del documento" {
		t.Errorf("сообщение: %v", e["message"])
	}
	problems := e["problems"].([]any)
	if len(problems) == 0 {
		t.Fatalf("нет замечаний: %s", bad.Body)
	}
	for _, p := range problems {
		m := p.(map[string]any)
		if m["message"] != "non può essere vuoto" {
			t.Errorf("замечание %v: %v", m["path"], m["message"])
		}
	}

	// то же по-русски
	author.client.lang = "ru"
	ru := author.client.do("POST", docsPath, map[string]any{
		"type": "memo", "code": "МЕМО-7", "title": "", "composed": map[string]any{"year": 1979}, "blocks": []any{},
	})
	if p := ru.json()["error"].(map[string]any)["problems"].([]any)[0].(map[string]any); p["message"] != "не может быть пустым" {
		t.Errorf("по-русски: %v", p)
	}

	// данные документа язык не меняет: название остаётся таким, как написал автор
	author.client.lang = "it"
	id := author.createMemo(t, "МЕМО-8", "Служебная записка")
	got := author.client.do("GET", teamDocPath(id, ""), nil)
	doc := got.json()["document"].(map[string]any)
	if doc["content"].(map[string]any)["title"] != "Служебная записка" || doc["type_name"] != "Memorandum" {
		t.Errorf("документ: %v", doc)
	}
}

// publishMemo заводит меморандум от автора и публикует его: отправка на проверку и вердикт Директората.
func publishMemo(t *testing.T, author, director *teamActor, code, title string, extra map[string]any) int64 {
	t.Helper()
	body := memoBody(code, title)
	for k, v := range extra {
		body[k] = v
	}
	created := author.client.do("POST", docsPath, body)
	if created.Code != 201 {
		t.Fatalf("создание %s: %d %s", code, created.Code, created.Body)
	}
	id := docID(created)
	rev := int(created.json()["document"].(map[string]any)["revision"].(float64))
	sent := author.client.do("POST", teamDocPath(id, "/submit"), map[string]any{"base_revision": rev})
	if sent.Code != 200 {
		t.Fatalf("отправка %s: %d %s", code, sent.Code, sent.Body)
	}
	rev = int(sent.json()["document"].(map[string]any)["revision"].(float64))
	if r := director.client.do("POST", teamDocPath(id, "/verdict"), map[string]any{"verdict": "approve", "base_revision": rev}); r.Code != 200 {
		t.Fatalf("публикация %s: %d %s", code, r.Code, r.Body)
	}
	return id
}

func TestReaderSeesItalianNamesButRussianData(t *testing.T) {
	_, actors := teamStackWithActors(t)
	author, director, guest := actors["author"], actors["director"], actors["guest"]
	publishMemo(t, author, director, "МЕМО-11", "Открытая записка", nil)

	guest.client.lang = "it"
	list := guest.client.do("GET", "/api/documents", nil)
	items := list.json()["items"].([]any)
	if list.Code != 200 || len(items) == 0 {
		t.Fatalf("каталог: %d %s", list.Code, list.Body)
	}
	item := items[0].(map[string]any)
	if item["type_name"] != "Memorandum" || item["title"] != "Открытая записка" {
		t.Errorf("карточка: %v", item)
	}
	doc := guest.client.do("GET", "/api/documents/MEMO-11", nil)
	d := doc.json()["document"].(map[string]any)
	if d["type_name"] != "Memorandum" || d["copy_number"] != "" {
		t.Errorf("документ гостю: %v", d)
	}

	// закрытый документ: сообщение и название нужного уровня — по-итальянски
	publishMemo(t, author, director, "МЕМО-12", "Закрытая записка", map[string]any{"level": 5, "direct_link": "forbidden"})
	denied := guest.client.do("GET", "/api/documents/MEMO-12", nil)
	de := denied.json()["error"].(map[string]any)
	if denied.Code != 403 || de["message"] != "Accesso negato" || de["required_level_name"] != "Curatore" {
		t.Errorf("отказ: %d %v", denied.Code, de)
	}
}
