package config

import "testing"

func TestLoadMail(t *testing.T) {
	set := func(kv map[string]string) {
		for _, k := range []string{"VLADHOST_SMTP_HOST", "VLADHOST_SMTP_PORT", "VLADHOST_SMTP_USER", "VLADHOST_SMTP_PASSWORD", "VLADHOST_SMTP_FROM", "VLADHOST_SMTP_FROM_NAME", "VLADHOST_SMTP_TLS", "VLADHOST_MAIL_SPOOL"} {
			t.Setenv(k, kv[k])
		}
	}

	set(nil)
	m, err := loadMail()
	if err != nil || m.Enabled() || m.TLS != "starttls" || m.FromName != "Vladhost" {
		t.Fatalf("без настроек почта выключена: %+v %v", m, err)
	}

	set(map[string]string{"VLADHOST_SMTP_HOST": "smtp.majordomo.ru", "VLADHOST_SMTP_FROM": "noreply@vladinc.ru", "VLADHOST_SMTP_USER": "noreply@vladinc.ru", "VLADHOST_SMTP_PASSWORD": "x", "VLADHOST_SMTP_PORT": "465", "VLADHOST_SMTP_TLS": "TLS"})
	m, err = loadMail()
	if err != nil || !m.Enabled() || m.Port != 465 || m.TLS != "tls" || m.Host != "smtp.majordomo.ru" {
		t.Fatalf("%+v %v", m, err)
	}

	set(map[string]string{"VLADHOST_MAIL_SPOOL": "/tmp/x"})
	if m, err = loadMail(); err != nil || !m.Enabled() {
		t.Fatalf("папка вместо SMTP: %+v %v", m, err)
	}

	for name, kv := range map[string]map[string]string{
		"сервер без отправителя":  {"VLADHOST_SMTP_HOST": "h"},
		"отправитель без сервера": {"VLADHOST_SMTP_FROM": "a@b.ru"},
		"логин без пароля":        {"VLADHOST_SMTP_HOST": "h", "VLADHOST_SMTP_FROM": "a@b.ru", "VLADHOST_SMTP_USER": "u"},
		"порт не число":           {"VLADHOST_SMTP_HOST": "h", "VLADHOST_SMTP_FROM": "a@b.ru", "VLADHOST_SMTP_PORT": "abc"},
		"порт вне диапазона":      {"VLADHOST_SMTP_HOST": "h", "VLADHOST_SMTP_FROM": "a@b.ru", "VLADHOST_SMTP_PORT": "70000"},
		"нулевой порт":            {"VLADHOST_SMTP_HOST": "h", "VLADHOST_SMTP_FROM": "a@b.ru", "VLADHOST_SMTP_PORT": "0"},
	} {
		set(kv)
		if _, err := loadMail(); err == nil {
			t.Errorf("%s: настройки приняты", name)
		}
	}
}
