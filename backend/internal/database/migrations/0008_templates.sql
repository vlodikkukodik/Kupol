-- +goose Up

-- Шаблоны документов и наборы блоков (этап 3, шаг 3.6).
--
-- Шаблон документа — заготовка нового документа: тип, заготовка названия, допуск, гриф и блоки. Набор блоков — несколько
-- блоков, которые вставляют в готовый документ (например, шапка досье и заголовок «Общие сведения»). Содержимое — JSON,
-- уже проверенный сервером так же, как блоки документа (blocks.go), поэтому вставить его в документ всегда можно.
-- Читают все члены команды; ведут — те, у кого право manage_templates (Редактор, Архивариус, Директорат).
CREATE TABLE templates (
    id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    kind        text        NOT NULL CHECK (kind IN ('document', 'blockset')),
    name        text        NOT NULL CHECK (char_length(btrim(name)) BETWEEN 1 AND 100),
    description text        NOT NULL DEFAULT '' CHECK (char_length(description) <= 500),
    -- тип документа (object, memo…): только у шаблона документа
    doc_type    text,
    content     jsonb       NOT NULL CHECK (jsonb_typeof(content) = 'object'),
    -- автор шаблона; пусто, если он удалил аккаунт
    author_id   bigint      REFERENCES users (id) ON DELETE SET NULL,
    created_at  timestamptz NOT NULL,
    updated_at  timestamptz NOT NULL,
    CONSTRAINT templates_doc_type_by_kind CHECK ((kind = 'document') = (doc_type IS NOT NULL))
);
-- Название уникально среди шаблонов одного вида без учёта регистра и пробелов по краям
CREATE UNIQUE INDEX templates_kind_name_idx ON templates (kind, lower(btrim(name)));
CREATE INDEX templates_kind_updated_idx ON templates (kind, updated_at DESC);

-- +goose Down
DROP TABLE templates;
