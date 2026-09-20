-- +goose Up

-- Глоссарий канона (этап 3, шаг 3.6): справочник терминов вселенной — как пишется, что значит, какие есть варианты.
-- Читают все члены команды; ведут те, у кого право manage_glossary (Редактор, Архивариус, Директорат).
-- Читателям сайта глоссарий пока не показывается: он появится вместе с разделом «О КУПОЛЕ» (этап 5).
CREATE TABLE glossary_terms (
    id         bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    term       text        NOT NULL CHECK (char_length(btrim(term)) BETWEEN 1 AND 100),
    definition text        NOT NULL CHECK (char_length(btrim(definition)) BETWEEN 1 AND 2000),
    -- другие написания и сокращения: находятся поиском, но сами не считаются терминами
    aliases    jsonb       NOT NULL DEFAULT '[]' CHECK (jsonb_typeof(aliases) = 'array' AND jsonb_array_length(aliases) <= 10),
    -- автор записи; пусто, если он удалил аккаунт
    author_id  bigint      REFERENCES users (id) ON DELETE SET NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL
);
-- Термин уникален без учёта регистра и пробелов по краям
CREATE UNIQUE INDEX glossary_terms_term_idx ON glossary_terms (lower(btrim(term)));

-- +goose Down
DROP TABLE glossary_terms;
