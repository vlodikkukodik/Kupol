package httpapi_test

import "testing"

// Почтовый сервер в панели команды (шаг 5.1.1): читают члены команды, правит и проверяет письмом
// только Директорат. Пароль наружу не выходит — остаётся признак «пароль задан».
func TestSmtpSettingsOverHTTP(t *testing.T) {
	_, actors := teamStackWithActors(t)
	const path = "/api/team/smtp"

	for name, wantCode := range map[string]int{"guest": 401, "plain": 403, "author": 200, "editor": 200, "moderator": 200, "archivist": 200, "director": 200} {
		if got := actors[name].client.do("GET", path, nil).Code; got != wantCode {
			t.Errorf("%s: чтение = %d, ожидалось %d", name, got, wantCode)
		}
	}
	// до первого сохранения действует окружение (в тестах его нет — почта выключена)
	if r := actors["author"].client.do("GET", path, nil); r.json()["smtp"].(map[string]any)["source"] != "env" || r.json()["smtp"].(map[string]any)["enabled"] != false {
		t.Errorf("до сохранения: %s", r.Body)
	}

	// письмо-проверка: пишет только Директорат, адрес проверяется раньше, чем почта
	want(t, actors["author"].client.do("POST", path+"/test", map[string]any{"to": "reader@example.org"}), 403, "forbidden")
	want(t, actors["director"].client.do("POST", path+"/test", map[string]any{"to": "не адрес"}), 422, "validation")
	if f := actors["director"].client.do("POST", path+"/test", map[string]any{"to": "не адрес"}).fields(); f["to"] == nil {
		t.Error("поле to в ошибке")
	}
	// почта выключена: 409, а не попытка соединиться с сервером
	want(t, actors["director"].client.do("POST", path+"/test", map[string]any{"to": "reader@example.org"}), 409, "invalid_state")

	for name, wantCode := range map[string]int{"guest": 401, "plain": 403, "author": 403, "editor": 403, "moderator": 403, "archivist": 403, "director": 200} {
		body := map[string]any{"enabled": false, "host": "smtp.example.org", "port": "587", "username": "", "password": "", "from": "no-reply@example.org", "from_name": "КУПОЛ"}
		if got := actors[name].client.do("PUT", path, body).Code; got != wantCode {
			t.Errorf("%s: правка = %d, ожидалось %d", name, got, wantCode)
		}
	}

	// сохранение Директоратом: пароль уходит на сервер, но не возвращается
	body := map[string]any{"enabled": true, "host": "smtp.example.org", "port": "587", "username": "postmaster@example.org", "password": "секрет", "from": "no-reply@example.org", "from_name": "КУПОЛ"}
	r := actors["director"].client.do("PUT", path, body)
	want(t, r, 200, "")
	saved := r.json()["smtp"].(map[string]any)
	if saved["password_set"] != true || saved["source"] != "db" || saved["can_edit"] != true {
		t.Errorf("после сохранения: %s", r.Body)
	}
	if _, ok := saved["password"]; ok {
		t.Errorf("пароль не должен возвращаться: %s", r.Body)
	}
	// член команды видит настройки, но править не может
	got := actors["editor"].client.do("GET", path, nil).json()["smtp"].(map[string]any)
	if got["can_edit"] != false || got["password_set"] != true || got["host"] != "smtp.example.org" {
		t.Errorf("Редактор: %v", got)
	}
	// поля не прислали (ключ password отсутствует — так и делает интерфейс при пустом поле) — сохранённый остаётся
	r = actors["director"].client.do("PUT", path, map[string]any{"enabled": true, "host": "smtp.example.org", "port": "587", "username": "postmaster@example.org", "from": "no-reply@example.org", "from_name": "КУПОЛ"})
	want(t, r, 200, "")
	if r.json()["smtp"].(map[string]any)["password_set"] != true {
		t.Error("отсутствующий пароль не должен затирать сохранённый")
	}
	// очистить вместе с пользователем — можно
	r = actors["director"].client.do("PUT", path, map[string]any{"enabled": false, "host": "smtp.example.org", "port": "587", "username": "", "password": "", "from": "no-reply@example.org", "from_name": "КУПОЛ"})
	want(t, r, 200, "")
	if r.json()["smtp"].(map[string]any)["password_set"] != false {
		t.Error("пустой пароль при пустом пользователе должен очистить сохранённый")
	}

	// проверка полей
	want(t, actors["director"].client.do("PUT", path, map[string]any{"enabled": true, "host": "", "port": "587", "username": "", "password": "", "from": "no-reply@example.org", "from_name": ""}), 422, "validation")
	want(t, actors["director"].client.do("PUT", path, map[string]any{"enabled": false, "host": "smtp.example.org", "port": "восемьдесят", "username": "", "password": "", "from": "no-reply@example.org", "from_name": ""}), 422, "validation")
	want(t, actors["director"].client.do("PUT", path, map[string]any{"enabled": false, "host": "smtp.example.org", "port": "587", "username": "", "password": "", "from": "без-собаки", "from_name": ""}), 422, "validation")
	want(t, actors["director"].client.do("PUT", path, map[string]any{"enabled": false, "host": "smtp.example.org", "port": "587", "username": "postmaster@example.org", "password": "", "from": "no-reply@example.org", "from_name": "", "role": "admin"}), 400, "bad_request")
	// порт вне диапазона — отдельное поле
	want(t, actors["director"].client.do("PUT", path, map[string]any{"enabled": false, "host": "smtp.example.org", "port": "70000", "username": "", "password": "", "from": "no-reply@example.org", "from_name": ""}), 422, "validation")
	if f := actors["director"].client.do("PUT", path, map[string]any{"enabled": false, "host": "smtp.example.org", "port": "0", "username": "", "password": "", "from": "no-reply@example.org", "from_name": ""}).fields(); f["port"] == nil {
		t.Error("поле port в ошибке")
	}
}
