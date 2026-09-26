-- +goose Up

-- Естественная сортировка шифров: «ПРИКАЗ-1978-2» перед «ПРИКАЗ-1978-12», а не наоборот.
-- (ICU: kn-true — числа в строках сравниваются как числа.)
CREATE COLLATION kupol_natural (provider = icu, locale = 'ru-u-kn-true', deterministic = true);

-- Документы архива (спецификация §6). Блоки — JSONB-массив; у каждого блока свой уровень допуска.
CREATE TABLE documents (
    id                 bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    -- Шифр в каноническом виде (кириллица): «О-041», «ПРИКАЗ-1978-12». NULL допустим только у неопубликованных
    -- объектов: номер О-№ присваивается автоматически при публикации.
    code               text,
    -- Тот же шифр латиницей для адресов: «O-041», «PRIKAZ-1978-12».
    slug               text,
    type               text        NOT NULL,
    title              text        NOT NULL,
    status             text        NOT NULL DEFAULT 'draft',
    -- Минимальный уровень читателя: 0 Гражданин … 6 Особый Совет, 7 — только Директорат.
    level              smallint    NOT NULL DEFAULT 0,
    -- Что видит читатель без допуска, открывший документ по прямой ссылке: 404 или «Доступ запрещён».
    direct_link        text        NOT NULL DEFAULT 'not_found',
    grif               text        NOT NULL DEFAULT 'Форма КУПОЛ-1',
    -- Дата составления — внутри вселенной; месяц и день можно не знать.
    composed_year      smallint    NOT NULL,
    composed_month     smallint,
    composed_day       smallint,
    -- Поля Объекта (для остальных типов запрещены ограничением ниже).
    object_number      integer,
    danger_class       smallint,
    deviation_points   integer,
    department         text,
    category           text,
    containment_status text,
    discovery_place    text,
    author_id          bigint REFERENCES users (id) ON DELETE SET NULL,
    blocks             jsonb       NOT NULL DEFAULT '[]',
    revision           integer     NOT NULL DEFAULT 1,
    created_at         timestamptz NOT NULL,
    updated_at         timestamptz NOT NULL,
    published_at       timestamptz,

    CONSTRAINT documents_type_check CHECK (type IN
        ('object', 'order', 'incident', 'personnel', 'unit', 'protocol', 'testimony', 'memo')),
    CONSTRAINT documents_status_check CHECK (status IN ('draft', 'review', 'published', 'archived')),
    CONSTRAINT documents_level_check CHECK (level BETWEEN 0 AND 7),
    CONSTRAINT documents_direct_link_check CHECK (direct_link IN ('not_found', 'forbidden')),
    CONSTRAINT documents_year_check CHECK (composed_year BETWEEN 1900 AND 2099),
    CONSTRAINT documents_month_check CHECK (composed_month BETWEEN 1 AND 12),
    CONSTRAINT documents_day_check CHECK (composed_day BETWEEN 1 AND 31),
    CONSTRAINT documents_day_needs_month CHECK (composed_day IS NULL OR composed_month IS NOT NULL),
    CONSTRAINT documents_danger_class_check CHECK (danger_class BETWEEN 1 AND 5),
    CONSTRAINT documents_deviation_check CHECK (deviation_points >= 0),
    CONSTRAINT documents_category_check CHECK (category IN ('person', 'entity', 'place')),
    CONSTRAINT documents_containment_check CHECK (containment_status IN ('contained', 'lost', 'destroyed', 'studying')),
    -- Поля Объекта бывают только у Объекта.
    CONSTRAINT documents_object_fields CHECK (type = 'object' OR (
        object_number IS NULL AND danger_class IS NULL AND deviation_points IS NULL AND category IS NULL
        AND containment_status IS NULL AND discovery_place IS NULL)),
    -- Опубликованный документ обязан иметь шифр.
    CONSTRAINT documents_published_has_code CHECK (status <> 'published' OR (code IS NOT NULL AND slug IS NOT NULL)),
    CONSTRAINT documents_code_slug_together CHECK ((code IS NULL) = (slug IS NULL)),
    CONSTRAINT documents_blocks_is_array CHECK (jsonb_typeof(blocks) = 'array')
);

CREATE UNIQUE INDEX documents_code_key ON documents (code) WHERE code IS NOT NULL;
CREATE UNIQUE INDEX documents_slug_key ON documents (slug) WHERE slug IS NOT NULL;
-- Номер О-№ уникален (О-0, Праисточник, — тоже номер 0).
CREATE UNIQUE INDEX documents_object_number_key ON documents (object_number) WHERE object_number IS NOT NULL;
CREATE INDEX documents_visibility_idx ON documents (status, level);
CREATE INDEX documents_type_idx ON documents (type);
CREATE INDEX documents_published_at_idx ON documents (published_at DESC) WHERE published_at IS NOT NULL;

-- Что читал пользователь (история в личном деле, достижения «прочитал 50 документов», статистика авторам).
-- Данные не восстановить задним числом, поэтому пишутся с самого начала.
CREATE TABLE document_reads (
    user_id       bigint      NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    document_id   bigint      NOT NULL REFERENCES documents (id) ON DELETE CASCADE,
    first_read_at timestamptz NOT NULL,
    last_read_at  timestamptz NOT NULL,
    read_count    integer     NOT NULL DEFAULT 1,
    PRIMARY KEY (user_id, document_id)
);
CREATE INDEX document_reads_document_idx ON document_reads (document_id);

-- +goose Down
DROP TABLE document_reads;
DROP TABLE documents;
DROP COLLATION kupol_natural;
