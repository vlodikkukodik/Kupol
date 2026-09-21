package documents

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"

	"kupol/internal/i18n"
)

// Status — состояние документа (спецификация §6: черновик → на проверке → опубликован → архив).
type Status string

const (
	StatusDraft     Status = "draft"
	StatusReview    Status = "review"
	StatusPublished Status = "published"
	StatusArchived  Status = "archived"
)

var statusNames = map[Status]string{
	StatusDraft: "Черновик", StatusReview: "На проверке", StatusPublished: "Опубликован", StatusArchived: "Архив",
}

func (s Status) Valid() bool  { _, ok := statusNames[s]; return ok }
func (s Status) Name() string { return statusNames[s] }

// NameIn — название на языке l.
func (s Status) NameIn(l i18n.Lang) string { return l.Translate(statusNames[s]) }

// Category — вид Объекта (канон, «Типы объектов»).
type Category string

const (
	CategoryPerson Category = "person" // человек с аномальными свойствами
	CategoryEntity Category = "entity" // существо или явление
	CategoryPlace  Category = "place"  // место или локация
)

var categoryNames = map[Category]string{
	CategoryPerson: "Человек с аномальными свойствами",
	CategoryEntity: "Существо или явление",
	CategoryPlace:  "Место или локация",
}

func (c Category) Valid() bool  { _, ok := categoryNames[c]; return ok }
func (c Category) Name() string { return categoryNames[c] }

// NameIn — название на языке l.
func (c Category) NameIn(l i18n.Lang) string { return l.Translate(categoryNames[c]) }

// Containment — статус содержания Объекта.
type Containment string

const (
	ContainmentContained Containment = "contained" // содержится
	ContainmentLost      Containment = "lost"      // утрачен
	ContainmentDestroyed Containment = "destroyed" // уничтожен
	ContainmentStudying  Containment = "studying"  // изучается
)

var containmentNames = map[Containment]string{
	ContainmentContained: "Содержится", ContainmentLost: "Утрачен",
	ContainmentDestroyed: "Уничтожен", ContainmentStudying: "Изучается",
}

func (c Containment) Valid() bool  { _, ok := containmentNames[c]; return ok }
func (c Containment) Name() string { return containmentNames[c] }

// NameIn — название на языке l.
func (c Containment) NameIn(l i18n.Lang) string { return l.Translate(containmentNames[c]) }

// DirectLink — что видит читатель без допуска, открывший документ по прямой ссылке.
type DirectLink string

const (
	DirectLinkNotFound  DirectLink = "not_found" // 404: существование документа не раскрывается
	DirectLinkForbidden DirectLink = "forbidden" // «Доступ запрещён» с указанием нужного допуска
)

func (d DirectLink) Valid() bool { return d == DirectLinkNotFound || d == DirectLinkForbidden }

// DefaultGrif — гриф по умолчанию (канон: «Форма КУПОЛ-1»).
const DefaultGrif = "Форма КУПОЛ-1"

// BlockList — блоки документа; хранится в JSONB.
type BlockList []Block

// Value записывает блоки в БД как JSON (строкой: драйвер сам приводит её к jsonb).
func (b BlockList) Value() (driver.Value, error) {
	if b == nil {
		return "[]", nil
	}
	raw, err := json.Marshal([]Block(b))
	if err != nil {
		return nil, err
	}
	return string(raw), nil
}

// Scan читает блоки из БД.
func (b *BlockList) Scan(src any) error {
	var raw []byte
	switch v := src.(type) {
	case []byte:
		raw = v
	case string:
		raw = []byte(v)
	case nil:
		*b = nil
		return nil
	default:
		return fmt.Errorf("documents: BlockList: неожиданный тип %T", src)
	}
	var out []Block
	if err := json.Unmarshal(raw, &out); err != nil {
		return err
	}
	*b = out
	return nil
}

// Document — запись документа (таблица documents).
type Document struct {
	ID         int64 `gorm:"primaryKey"`
	Code       *string
	Slug       *string
	Type       string
	Title      string
	Status     string
	Level      int
	DirectLink string
	Grif       string

	ComposedYear  int
	ComposedMonth *int
	ComposedDay   *int

	ObjectNumber      *int
	DangerClass       *int
	DeviationPoints   *int
	Department        *string
	Category          *string
	ContainmentStatus *string
	DiscoveryPlace    *string

	AuthorID *int64
	Blocks   BlockList `gorm:"type:jsonb"`
	Revision int
	// Время задаёт только сервис (он подменяем в тестах); автопроставление GORM отключено.
	CreatedAt   time.Time `gorm:"autoCreateTime:false"`
	UpdatedAt   time.Time `gorm:"autoUpdateTime:false"`
	PublishedAt *time.Time
}

func (Document) TableName() string { return "documents" }

// Composed — дата составления внутри вселенной; месяц и день можно не знать.
type Composed struct {
	Year  int  `json:"year"`
	Month *int `json:"month,omitempty"`
	Day   *int `json:"day,omitempty"`
}
