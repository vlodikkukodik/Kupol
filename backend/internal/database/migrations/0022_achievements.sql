-- +goose Up

-- Грамоты (достижения, шаг 5.6): фиксированный набор, выдаётся автоматически и не снимается.
CREATE TABLE achievements (
    user_id    bigint      NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    kind       text        NOT NULL,
    awarded_at timestamptz NOT NULL,
    PRIMARY KEY (user_id, kind)
);
CREATE INDEX achievements_user_idx ON achievements (user_id, awarded_at DESC);

-- +goose Down
DROP TABLE achievements;
