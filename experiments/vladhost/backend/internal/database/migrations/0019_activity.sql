-- +goose Up
-- Журнал действий аккаунта: входы, смена пароля, доступы, изменения сайтов и служб — с адресом и временем. Пользователь видит свои события;
-- частые однотипные (правки файлов, неудачные входы с одного адреса) сливаются в одну строку со счётчиком.
CREATE TABLE account_events (
    id         BIGSERIAL PRIMARY KEY,
    user_id    BIGINT      NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    kind       TEXT        NOT NULL,             -- auth.login, site.create, ftp.enable, files.change …
    category   TEXT        NOT NULL,             -- security, sites, access, services, other (для фильтра)
    target     TEXT        NOT NULL DEFAULT '',  -- сайт, домен, имя базы и т. п.
    count      INT         NOT NULL DEFAULT 1,   -- сколько однотипных событий слито в эту строку
    ip         TEXT        NOT NULL DEFAULT '',
    user_agent TEXT        NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(), -- первое событие строки
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()  -- последнее
);
CREATE INDEX account_events_user_idx ON account_events (user_id, id DESC);
CREATE INDEX account_events_category_idx ON account_events (user_id, category, id DESC);
CREATE INDEX account_events_created_idx ON account_events (created_at);

-- +goose Down
DROP TABLE account_events;
