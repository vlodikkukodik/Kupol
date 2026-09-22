package documents

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"

	"kupol/internal/xp"
)

// «Оценки» (шаг 5.3, спецификация §8): «ознакомлен / одобряю / сомнительно» — канцелярские
// отметки читателей под документами. Публичные счётчики штампами с числами (видит каждый
// залогиненный); +5 XP за оценку (до 10/день). У одного пользователя на документ — одна
// оценка (можно изменить или удалить).

// Rating — тип оценки.
type Rating string

const (
	RatingAcknowledged Rating = "acknowledged" // ознакомлен
	RatingApproved     Rating = "approved"     // одобряю
	RatingDoubtful     Rating = "doubtful"     // сомнительно
)

var validRatings = map[Rating]bool{
	RatingAcknowledged: true,
	RatingApproved:     true,
	RatingDoubtful:     true,
}

// RatingValid проверяет, что значение rating допустимо.
func RatingValid(r Rating) bool { return validRatings[r] }

// ErrRatingNotFound — нет такой оценки (или она под другим документом).
var ErrRatingNotFound = errors.New("documents: оценка не найдена")

type ratingRow struct {
	ID         int64  `gorm:"primaryKey"`
	DocumentID int64
	UserID     int64
	Rating     string
	CreatedAt  time.Time
}

func (ratingRow) TableName() string { return "document_ratings" }

// RatingOut — оценка в ответе читателю.
type RatingOut struct {
	Rating   Rating `json:"rating"`
	Username string `json:"username"`
}

// RatingCounts — публичные счётчики оценок под документом (штампы с числами).
type RatingCounts struct {
	Acknowledged int `json:"acknowledged"`
	Approved     int `json:"approved"`
	Doubtful     int `json:"doubtful"`
}

// MyRating — оценка текущего пользователя (для кнопок).
type MyRating struct {
	Rating *Rating `json:"rating" tstype:"Rating | null"`
}

// DocumentRatings — полный ответ по оценкам документа.
type DocumentRatings struct {
	Counts RatingCounts `json:"counts"`
	My     MyRating     `json:"my"`
}

// SetRating устанавливает оценку читателя на документ (заменяет предыдущую). Если оценка
// совпадает с текущей — удаляет её (toggle). Начисляет XP (до дневного лимита).
func (s *Service) SetRating(ctx context.Context, v Viewer, ref string, rating Rating) (*DocumentRatings, error) {
	if v.UserID == 0 {
		return nil, ErrForbidden
	}
	if !RatingValid(rating) {
		return nil, oneProblem("rating", "Неизвестная оценка")
	}
	d, err := s.resolveVisibleDocument(ctx, v, ref)
	if err != nil {
		return nil, err
	}
	if d.Status != string(StatusPublished) {
		return nil, ErrForbidden
	}

	now := s.now()
	var res *DocumentRatings
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var existing ratingRow
		err := tx.Where("document_id = ? AND user_id = ?", d.ID, v.UserID).Take(&existing).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}

		if errors.Is(err, gorm.ErrRecordNotFound) {
			// новая оценка
			if err := tx.Create(&ratingRow{DocumentID: d.ID, UserID: v.UserID, Rating: string(rating), CreatedAt: now}).Error; err != nil {
				return err
			}
			if _, err := xp.Award(ctx, tx, v.UserID, xp.SourceRating, xp.RatingXP, xp.RatingDailyCap, now); err != nil {
				return err
			}
		} else if existing.Rating == string(rating) {
			// toggle: убираем оценку
			if err := tx.Where("document_id = ? AND user_id = ?", d.ID, v.UserID).Delete(&ratingRow{}).Error; err != nil {
				return err
			}
		} else {
			// замена оценки (без повторного XP: это не новое действие, а корректировка)
			if err := tx.Model(&ratingRow{}).Where("document_id = ? AND user_id = ?", d.ID, v.UserID).Update("rating", string(rating)).Error; err != nil {
				return err
			}
		}

		counts, err := ratingCounts(tx, d.ID)
		if err != nil {
			return err
		}
		my, err := myRating(tx, d.ID, v.UserID)
		if err != nil {
			return err
		}
		res = &DocumentRatings{Counts: *counts, My: my}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return res, nil
}

// DeleteRating удаляет оценку читателя с документа (без XP — это отмена).
func (s *Service) DeleteRating(ctx context.Context, v Viewer, ref string) (*DocumentRatings, error) {
	if v.UserID == 0 {
		return nil, ErrForbidden
	}
	d, err := s.resolveVisibleDocument(ctx, v, ref)
	if err != nil {
		return nil, err
	}

	var res *DocumentRatings
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("document_id = ? AND user_id = ?", d.ID, v.UserID).Delete(&ratingRow{}).Error; err != nil {
			return err
		}
		counts, err := ratingCounts(tx, d.ID)
		if err != nil {
			return err
		}
		my, err := myRating(tx, d.ID, v.UserID)
		if err != nil {
			return err
		}
		res = &DocumentRatings{Counts: *counts, My: my}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return res, nil
}

// GetRatings возвращает счётчики и оценку текущего пользователя.
func (s *Service) GetRatings(ctx context.Context, v Viewer, ref string) (*DocumentRatings, error) {
	d, err := s.resolveVisibleDocument(ctx, v, ref)
	if err != nil {
		return nil, err
	}
	counts, err := ratingCounts(s.db.WithContext(ctx), d.ID)
	if err != nil {
		return nil, err
	}
	my, err := myRating(s.db.WithContext(ctx), d.ID, v.UserID)
	if err != nil {
		return nil, err
	}
	return &DocumentRatings{Counts: *counts, My: my}, nil
}

func ratingCounts(tx *gorm.DB, docID int64) (*RatingCounts, error) {
	var rows []struct {
		Rating string
		Count  int64
	}
	if err := tx.Model(&ratingRow{}).
		Select("rating, count(*) AS count").
		Where("document_id = ?", docID).
		Group("rating").Scan(&rows).Error; err != nil {
		return nil, err
	}
	c := &RatingCounts{}
	for _, r := range rows {
		switch Rating(r.Rating) {
		case RatingAcknowledged:
			c.Acknowledged = int(r.Count)
		case RatingApproved:
			c.Approved = int(r.Count)
		case RatingDoubtful:
			c.Doubtful = int(r.Count)
		}
	}
	return c, nil
}

func myRating(tx *gorm.DB, docID, userID int64) (MyRating, error) {
	if userID == 0 {
		return MyRating{}, nil
	}
	var row ratingRow
	err := tx.Where("document_id = ? AND user_id = ?", docID, userID).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return MyRating{}, nil
	}
	if err != nil {
		return MyRating{}, err
	}
	r := Rating(row.Rating)
	return MyRating{Rating: &r}, nil
}
