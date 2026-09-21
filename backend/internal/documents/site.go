package documents

import (
	"context"
	"strings"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"kupol/internal/audit"
)

// Настройки сайта. Сейчас одна: контакты автора на странице «О КУПОЛЕ». Читают все (страница открыта гостям), правит только Директорат:
// контакты — решение автора архива, а не Редактора или Архивариуса.

const (
	siteContactKey    = "contact"
	maxSiteContactLen = 1000
)

// SiteSettings — настройки сайта.
type SiteSettings struct {
	// Contact — как связаться с автором (почта, ссылка, ник); пусто — раздел контактов на странице не показывается.
	Contact string `json:"contact"`
}

// SiteSettingsOut — настройки в team panel: с правом их менять.
type SiteSettingsOut struct {
	SiteSettings
	UpdatedAt *time.Time `json:"updated_at,omitempty"`
	// UpdatedBy — кто менял последним.
	UpdatedBy *string `json:"updated_by,omitempty"`
	// CanEdit — вправе ли человек менять настройки (только Директорат).
	CanEdit bool `json:"can_edit"`
}

type siteSettingRow struct {
	Key       string `gorm:"primaryKey"`
	Value     string
	UpdatedAt time.Time `gorm:"autoUpdateTime:false"`
	UpdatedBy *int64
}

func (siteSettingRow) TableName() string { return "site_settings" }

// SiteInfo — публичные настройки сайта.
func (s *Service) SiteInfo(ctx context.Context) (SiteSettings, error) {
	var rows []siteSettingRow
	if err := s.db.WithContext(ctx).Where("key = ?", siteContactKey).Find(&rows).Error; err != nil {
		return SiteSettings{}, err
	}
	if len(rows) == 0 {
		return SiteSettings{}, nil
	}
	return SiteSettings{Contact: rows[0].Value}, nil
}

// SiteSettingsGet — настройки для панели команды.
func (s *Service) SiteSettingsGet(ctx context.Context, a Actor) (*SiteSettingsOut, error) {
	var row struct {
		Value     string
		UpdatedAt time.Time
		Login     *string
	}
	res := s.db.WithContext(ctx).Table("site_settings s").Select("s.value, s.updated_at, u.login::text AS login").
		Joins("LEFT JOIN users u ON u.id = s.updated_by").Where("s.key = ?", siteContactKey).Limit(1).Scan(&row)
	if res.Error != nil {
		return nil, res.Error
	}
	out := &SiteSettingsOut{CanEdit: a.Directorate}
	if res.RowsAffected > 0 {
		out.Contact = row.Value
		t := row.UpdatedAt.UTC()
		out.UpdatedAt, out.UpdatedBy = &t, row.Login
	}
	return out, nil
}

// cleanContact срезает пробелы, убирает управляющие знаки (кроме перевода строки) и лишние пустые строки.
func cleanContact(s string) string {
	s = strings.Map(func(r rune) rune {
		if r == '\r' {
			return -1
		}
		if r < 0x20 && r != '\n' {
			return ' '
		}
		return r
	}, s)
	lines := strings.Split(s, "\n")
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l == "" && (len(out) == 0 || out[len(out)-1] == "") {
			continue
		}
		out = append(out, l)
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

// SiteSettingsUpdate сохраняет настройки. Только Директорат; пустой контакт удаляет настройку.
func (s *Service) SiteSettingsUpdate(ctx context.Context, a Actor, in SiteSettings) (*SiteSettingsOut, error) {
	if !a.Directorate {
		return nil, ErrForbidden
	}
	contact := cleanContact(in.Contact)
	if n := utf8.RuneCountInString(contact); n > maxSiteContactLen {
		return nil, oneProblem("contact", "слишком длинное значение (%d знаков, не больше %d)", n, maxSiteContactLen)
	}
	now := s.now()
	uid := a.UserID
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if contact == "" {
			if err := tx.Where("key = ?", siteContactKey).Delete(&siteSettingRow{}).Error; err != nil {
				return err
			}
		} else {
			row := siteSettingRow{Key: siteContactKey, Value: contact, UpdatedAt: now, UpdatedBy: &uid}
			err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "key"}}, DoUpdates: clause.AssignmentColumns([]string{"value", "updated_at", "updated_by"})}).Create(&row).Error
			if err != nil {
				return err
			}
		}
		// в журнал — сам факт и длина, а не текст: контакт может быть личным
		return audit.Record(tx, now, audit.SiteUpdated, audit.Event{ActorID: &uid, Details: audit.Details("setting", siteContactKey, "length", utf8.RuneCountInString(contact))})
	})
	if err != nil {
		return nil, err
	}
	return s.SiteSettingsGet(ctx, a)
}
