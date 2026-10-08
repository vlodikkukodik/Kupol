package mailsettings

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"kupol/internal/config"
	"kupol/internal/testutil"
)

func newService(t *testing.T, env config.SMTP) (*Service, Actor) {
	t.Helper()
	db := testutil.NewMigratedDB(t)
	s, err := New(db, env, bytes.Repeat([]byte("k"), 32), testutil.Logger(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var uid int64
	if err := db.Raw(`INSERT INTO users (login, password_hash, backup_code_hash, created_at, password_changed_at)
		VALUES ('dir','x','y',now(),now()) RETURNING id`).Scan(&uid).Error; err != nil {
		t.Fatal(err)
	}
	return s, Actor{UserID: uid, Directorate: true}
}

// Ключ для шифрования пароля выводится из общего секрета сервера; короткий секрет — отказ, а не тихий обход.
func TestNewRejectsShortSecret(t *testing.T) {
	db := testutil.NewMigratedDB(t)
	if _, err := New(db, config.SMTP{}, []byte("короткий"), testutil.Logger(), nil); err == nil {
		t.Fatal("ожидался отказ для короткого общего секрета")
	}
}

// Пароль живёт только в базе в зашифрованном виде и обратно: сохранённый читается, повреждённый — нет.
func TestSecretBoxRoundTrip(t *testing.T) {
	box, err := newSecretBox(bytes.Repeat([]byte("k"), 32))
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := box.seal([]byte("секрет"))
	if err != nil || len(sealed) == 0 {
		t.Fatalf("seal: %x, %v", sealed, err)
	}
	got, err := box.open(sealed)
	if err != nil || string(got) != "секрет" {
		t.Errorf("open: %q, %v", got, err)
	}
	// пустой пароль — пустая колонка, а не NULL (в базе колонка NOT NULL)
	empty, err := box.seal(nil)
	if err != nil || empty == nil || len(empty) != 0 {
		t.Errorf("seal пустого: %#v, %v", empty, err)
	}
	if _, err := box.open([]byte("коротко")); err == nil {
		t.Error("повреждённый шифртекст должен не читаться")
	}
}

// Пока настройки не сохраняли, действует окружение сервера — и читать их может вся команда.
func TestGetFallsBackToEnvironment(t *testing.T) {
	ctx := context.Background()
	env := config.SMTP{Host: "smtp.env.example.org", Port: "25", Username: "envuser", Password: "envpass", From: "env@example.org", FromName: "Окружение"}
	s, a := newService(t, env)

	out, err := s.Get(ctx, a)
	if err != nil {
		t.Fatal(err)
	}
	if out.Source != "env" || !out.Enabled || out.Host != env.Host || out.Port != env.Port || out.From != env.From {
		t.Errorf("окружение: %+v", out)
	}
	if !out.PasswordSet {
		t.Error("пароль задан в окружении — password_set должен быть true")
	}
	if !out.CanEdit {
		t.Error("Директорат должно править")
	}
	if out.UpdatedAt != nil || out.UpdatedBy != nil {
		t.Errorf("до сохранения обновления не было: %+v", out)
	}
	if guest, err := s.Get(ctx, Actor{UserID: a.UserID}); err != nil || guest.CanEdit {
		t.Errorf("без Директората: can_edit=%v, err=%v", guest.CanEdit, err)
	}
}

// Сохранение Директоратом: пароль уходит в базу зашифрованным, наружу — только признак «задан».
func TestUpdateKeepsAndClearsPassword(t *testing.T) {
	ctx := context.Background()
	s, a := newService(t, config.SMTP{})

	pw := "секрет"
	in := Input{Enabled: true, Host: "smtp.example.org", Port: "587", Username: "postmaster@example.org", Password: &pw, From: "no-reply@example.org", FromName: "КУПОЛ"}
	if _, err := s.Update(ctx, a, in); err != nil {
		t.Fatal(err)
	}
	out, err := s.Get(ctx, a)
	if err != nil {
		t.Fatal(err)
	}
	if out.Source != "db" || !out.PasswordSet || out.UpdatedBy == nil || *out.UpdatedBy != "dir" {
		t.Errorf("после сохранения: %+v", out)
	}

	// ключ password не прислали — сохранённый остаётся (так экономит интерфейс при пустом поле)
	in.Password = nil
	if _, err := s.Update(ctx, a, in); err != nil {
		t.Fatal(err)
	}
	if out, _ = s.Get(ctx, a); !out.PasswordSet {
		t.Error("отсутствующий ключ затёр пароль")
	}

	// "" снимает пароль, но не оставляет почту с пользователем без него
	empty := ""
	in.Password = &empty
	var ve *ValidationError
	if _, err := s.Update(ctx, a, in); !errors.As(err, &ve) || ve.Fields["password"] == "" {
		t.Fatalf("пользователь без пароля ожидал ошибку поля password, получено %v", err)
	}
	in.Username = ""
	if _, err := s.Update(ctx, a, in); err != nil {
		t.Fatal(err)
	}
	if out, _ = s.Get(ctx, a); out.PasswordSet {
		t.Error("пустой пароль при пустом пользователе должен очистить сохранённый")
	}
}

// Правит только Директорат; форма проверяет поля до записи в базу.
func TestUpdateValidationAndRights(t *testing.T) {
	ctx := context.Background()
	s, a := newService(t, config.SMTP{})
	plain := Actor{UserID: a.UserID}
	in := Input{Enabled: true, Host: "smtp.example.org", Port: "587", From: "no-reply@example.org"}
	if _, err := s.Update(ctx, plain, in); !errors.Is(err, ErrForbidden) {
		t.Errorf("без Директората: %v", err)
	}

	pw := "секрет"
	in.Username, in.Password = "postmaster@example.org", &pw
	if _, err := s.Update(ctx, a, in); err != nil {
		t.Fatalf("валидный ввод: %v", err)
	}

	for name, tc := range map[string]struct {
		in    Input
		field string
	}{
		"порт не число":        {Input{Enabled: false, Host: "smtp.example.org", Port: "восемьдесят", From: "no-reply@example.org"}, "port"},
		"порт вне диапазона":   {Input{Enabled: false, Host: "smtp.example.org", Port: "70000", From: "no-reply@example.org"}, "port"},
		"адрес без собаки":     {Input{Enabled: false, Host: "smtp.example.org", Port: "587", From: "без-собаки"}, "from"},
		"включено без сервера": {Input{Enabled: true, Host: "", Port: "587", From: "no-reply@example.org"}, "host"},
	} {
		_, err := s.Update(ctx, a, tc.in)
		var ve *ValidationError
		if !errors.As(err, &ve) || ve.Fields[tc.field] == "" {
			t.Errorf("%s: ожидалось поле %q, получено %v", name, tc.field, err)
		}
	}
}

// Письмо-проверка: пишет только Директорат, а выключенная почта — 409, а не попытка соединиться.
func TestTestChecksRightsAndState(t *testing.T) {
	ctx := context.Background()
	s, a := newService(t, config.SMTP{})
	if err := s.Test(ctx, Actor{UserID: a.UserID}, "reader@example.org"); !errors.Is(err, ErrForbidden) {
		t.Errorf("без Директората: %v", err)
	}
	var ve *ValidationError
	if err := s.Test(ctx, a, "не адрес"); !errors.As(err, &ve) || ve.Fields["to"] == "" {
		t.Errorf("адрес: %v", err)
	}
	// почта выключена: соединение не открываем
	if err := s.Test(ctx, a, "reader@example.org"); !errors.Is(err, ErrNotEnabled) {
		t.Errorf("выключенная почта: %v", err)
	}
	// sender из базы читается заново при каждом вызове — сохранение действует без перезапуска
	if _, ok := s.Sender(ctx); ok {
		t.Error("до сохранения отправителя быть не должно")
	}
	pw := "секрет"
	if _, err := s.Update(ctx, a, Input{Enabled: true, Host: "smtp.example.org", Port: "587", Username: "u", Password: &pw, From: "no-reply@example.org"}); err != nil {
		t.Fatal(err)
	}
	if sender, ok := s.Sender(ctx); !ok || sender == nil {
		t.Error("после сохранения отправитель должен появиться")
	}
}
