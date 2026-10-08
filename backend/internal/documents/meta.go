package documents

import "kupol/internal/i18n"

// Справочник для форм team panel: какие бывают типы, статусы и значения свойств Объекта.
// Единственный источник — этот пакет; интерфейс ничего из этого у себя не дублирует.

// typeCodeExamples — образец шифра для подсказки в форме.
var typeCodeExamples = map[Type]string{
	TypeObject:    "О-041",
	TypeOrder:     "ПРИКАЗ-1978-12",
	TypeIncident:  "ИНЦ-1982-07",
	TypePersonnel: "ЛД-0157",
	TypeUnit:      "ОТД-2 или ОБ-14",
	TypeProtocol:  "ПРОТ-1979-03",
	TypeTestimony: "ПОК-1980-22",
	TypeMemo:      "МЕМО-5",
}

// blockKindNames — названия типов блоков для интерфейса (в порядке KindNames).
var blockKindNames = map[string]string{
	"heading": "Заголовок", "paragraph": "Абзац", "list": "Список", "quote": "Цитата", "dossier_header": "Шапка досье",
	"experiment_log": "Журнал эксперимента", "stamp": "Штамп", "memo": "Бланк (записка, приказ, письмо)",
	"clipping": "Вырезка / расшифровка", "table": "Таблица", "doc_link": "Ссылка на документ", "divider": "Разделитель",
	"page": "Разрыв страницы", "footnote": "Сноска", "appendix": "Приложение",
	"containment_procedure": "Процедура содержания", "directive": "Пункт приказа", "incident_timeline": "Хронология инцидента",
	"personnel_record": "Личные данные", "roster": "Штат", "hypothesis": "Гипотеза и результат",
	"qa": "Вопрос — ответ", "routing": "Маршрут согласования",
	"image": "Изображение", "audio": "Аудиозапись",
}

// BlockKindName — название типа блока (для неизвестного — сам тип).
func BlockKindName(kind string) string { return BlockKindNameIn(i18n.RU, kind) }

// BlockKindNameIn — то же на языке l.
func BlockKindNameIn(l i18n.Lang, kind string) string {
	if n, ok := blockKindNames[kind]; ok {
		return l.Translate(n)
	}
	return kind
}

// CodeExample — образец шифра этого типа.
func (t Type) CodeExample() string { return typeCodeExamples[t] }

// CodeOptional — можно ли завести документ этого типа без шифра. Объекту номер О-№ присваивается при публикации;
// остальным шифр нужен сразу.
func (t Type) CodeOptional() bool { return t == TypeObject }

// Option — значение справочника: идентификатор и название.
type Option struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// TypeOption — тип документа для формы создания.
type TypeOption struct {
	Option       `tstype:",extends"`
	CodeExample  string `json:"code_example"`
	CodeOptional bool   `json:"code_optional"`
}

// Meta — справочник значений.
type Meta struct {
	Types       []TypeOption `json:"types"`
	BlockKinds  []Option     `json:"block_kinds"`
	Statuses    []Option     `json:"statuses"`
	Categories  []Option     `json:"categories"`
	Containment []Option     `json:"containment"`
	DirectLinks []Option     `json:"direct_links"`
	// MaxLevel — наибольший уровень допуска документа (7 — только Директорат).
	MaxLevel    int    `json:"max_level"`
	DefaultGrif string `json:"default_grif"`
}

// DocumentMeta возвращает справочник значений для форм по-русски.
func DocumentMeta() Meta { return DocumentMetaIn(i18n.RU) }

// DocumentMetaIn — справочник на языке l.
func DocumentMetaIn(l i18n.Lang) Meta {
	m := Meta{
		MaxLevel:    MaxLevel,
		DefaultGrif: DefaultGrif,
		DirectLinks: []Option{
			{string(DirectLinkNotFound), l.T("«Дело не найдено» (существование не раскрывается)")},
			{string(DirectLinkForbidden), l.T("«Доступ запрещён» с указанием нужного допуска")},
		},
	}
	for _, k := range KindNames() {
		m.BlockKinds = append(m.BlockKinds, Option{k, BlockKindNameIn(l, k)})
	}
	for _, s := range []Status{StatusDraft, StatusReview, StatusPublished, StatusArchived} {
		m.Statuses = append(m.Statuses, Option{string(s), s.NameIn(l)})
	}
	for _, t := range Types {
		m.Types = append(m.Types, TypeOption{Option: Option{string(t), t.NameIn(l)}, CodeExample: t.CodeExample(), CodeOptional: t.CodeOptional()})
	}
	for _, c := range []Category{CategoryPerson, CategoryEntity, CategoryPlace} {
		m.Categories = append(m.Categories, Option{string(c), c.NameIn(l)})
	}
	for _, c := range []Containment{ContainmentContained, ContainmentLost, ContainmentDestroyed, ContainmentStudying} {
		m.Containment = append(m.Containment, Option{string(c), c.NameIn(l)})
	}
	return m
}
