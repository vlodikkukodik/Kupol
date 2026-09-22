-- +goose Up

-- Очки опыта (спецификация §5): счётчик на пользователе, серия ежедневных входов, автоматическое повышение
-- 1→2→3. Журнал начислений — отдельной таблицей: нужен для дневных лимитов по источнику (оценки, комментарии —
-- шаги 5.2–5.4) и чтобы объяснить в личном деле, откуда взялось число.
ALTER TABLE users
    ADD COLUMN xp integer NOT NULL DEFAULT 0,
    ADD COLUMN login_streak integer NOT NULL DEFAULT 0,
    ADD COLUMN last_xp_day date;

CREATE TABLE xp_events (
    id         bigserial PRIMARY KEY,
    user_id    bigint NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    source     text NOT NULL,
    amount     integer NOT NULL,
    created_at timestamptz NOT NULL
);

-- дневные лимиты по источнику (5.2–5.4): сумма начислений пользователю за источник за сегодня
CREATE INDEX xp_events_user_source_day_idx ON xp_events (user_id, source, created_at);

-- +goose Down
DROP TABLE xp_events;
ALTER TABLE users
    DROP COLUMN xp,
    DROP COLUMN login_streak,
    DROP COLUMN last_xp_day;
