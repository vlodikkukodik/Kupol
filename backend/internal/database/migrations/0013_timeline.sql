-- +goose Up

-- Хронология «О КУПОЛЕ»: события вселенной 1974 — наши дни. Ведут Редактор и Архивариус (право manage_timeline).
-- У события свой уровень допуска: шкала показывает читателю только доступные ему события. Ссылка на документ — шифром: её читатель видит,
-- только если может открыть сам документ.
CREATE TABLE timeline_events (
    id            bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    year          smallint NOT NULL CHECK (year BETWEEN 1900 AND 2099),
    month         smallint CHECK (month BETWEEN 1 AND 12),
    day           smallint CHECK (day BETWEEN 1 AND 31),
    title         text     NOT NULL CHECK (char_length(title) BETWEEN 1 AND 200),
    body          text     NOT NULL DEFAULT '' CHECK (char_length(body) <= 2000),
    level         smallint NOT NULL DEFAULT 0 CHECK (level BETWEEN 0 AND 7),
    document_code text,
    author_id     bigint REFERENCES users (id) ON DELETE SET NULL,
    created_at    timestamptz NOT NULL,
    updated_at    timestamptz NOT NULL,
    CONSTRAINT timeline_day_needs_month CHECK (day IS NULL OR month IS NOT NULL)
);
CREATE INDEX timeline_events_date_idx ON timeline_events (year, month NULLS FIRST, day NULLS FIRST, id);

-- +goose Down
DROP TABLE timeline_events;
