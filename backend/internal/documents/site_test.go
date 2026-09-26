package documents

import (
	"errors"
	"strings"
	"testing"

	"kupol/internal/audit"
)

func TestSiteSettingsOnlyDirectorateEdits(t *testing.T) {
	e := newEnv(t)
	a := e.actors()
	archivist := Actor{UserID: e.user("archivist"), Login: "archivist", CanManageGlossary: true, CanManageTemplates: true, CanManageTimeline: true}
	for name, who := range map[string]Actor{"Автор": a.owner, "Редактор": a.editor, "Архивариус": archivist, "модератор": a.moderator, "посторонний": a.plain} {
		if _, err := e.svc.SiteSettingsUpdate(ctx, who, SiteSettings{Contact: "взлом"}); !errors.Is(err, ErrForbidden) {
			t.Errorf("%s: %v", name, err)
		}
		if out, err := e.svc.SiteSettingsGet(ctx, who); err != nil || out.CanEdit {
			t.Errorf("%s: чтение %+v %v", name, out, err)
		}
	}
	if s, _ := e.svc.SiteInfo(ctx); s.Contact != "" {
		t.Errorf("контакт появился без права: %q", s.Contact)
	}
	out, err := e.svc.SiteSettingsUpdate(ctx, a.director, SiteSettings{Contact: "  автор@example.org  \r\n\r\n\r\nTelegram: @kupol  "})
	if err != nil || !out.CanEdit || out.Contact != "автор@example.org\n\nTelegram: @kupol" || out.UpdatedBy == nil || *out.UpdatedBy != "director" || out.UpdatedAt == nil {
		t.Fatalf("сохранение: %+v %v", out, err)
	}
	if s, _ := e.svc.SiteInfo(ctx); s.Contact != "автор@example.org\n\nTelegram: @kupol" {
		t.Errorf("публичный контакт: %q", s.Contact)
	}
}

func TestSiteContactValidationClearingAndAudit(t *testing.T) {
	e := newEnv(t)
	a := e.actors()
	var ve *ValidationError
	if _, err := e.svc.SiteSettingsUpdate(ctx, a.director, SiteSettings{Contact: strings.Repeat("я", maxSiteContactLen+1)}); !errors.As(err, &ve) {
		t.Errorf("слишком длинный контакт принят: %v", err)
	}
	if _, err := e.svc.SiteSettingsUpdate(ctx, a.director, SiteSettings{Contact: strings.Repeat("я", maxSiteContactLen)}); err != nil {
		t.Errorf("контакт максимальной длины: %v", err)
	}
	// управляющие знаки убираются, повторное сохранение обновляет ту же строку
	out, _ := e.svc.SiteSettingsUpdate(ctx, a.director, SiteSettings{Contact: "a\x00b\x07c"})
	if out.Contact != "a b c" || e.count("SELECT count(*) FROM site_settings") != 1 {
		t.Errorf("очистка: %q, строк %d", out.Contact, e.count("SELECT count(*) FROM site_settings"))
	}
	// пустое значение удаляет настройку
	out, err := e.svc.SiteSettingsUpdate(ctx, a.director, SiteSettings{Contact: " \n "})
	if err != nil || out.Contact != "" || e.count("SELECT count(*) FROM site_settings") != 0 {
		t.Errorf("очистка контакта: %+v %v", out, err)
	}
	if s, _ := e.svc.SiteInfo(ctx); s.Contact != "" {
		t.Errorf("контакт остался: %q", s.Contact)
	}
	// в журнале — факт и длина, текст контакта не попадает
	if _, err := e.svc.SiteSettingsUpdate(ctx, a.director, SiteSettings{Contact: "секретный@контакт.example"}); err != nil {
		t.Fatal(err)
	}
	rows := e.auditRows(audit.SiteUpdated)
	if len(rows) != 4 || rows[0].Title != "Изменены настройки сайта" {
		t.Fatalf("журнал: %d записей", len(rows))
	}
	for _, r := range rows {
		if strings.Contains(r.Details, "секретный") {
			t.Errorf("текст контакта в журнале: %s", r.Details)
		}
	}
}
