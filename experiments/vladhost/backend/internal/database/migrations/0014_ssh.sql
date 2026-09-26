-- +goose Up
-- Доступ по SSH и веб-терминал. Ключи принадлежат аккаунту; каждый сайт получает доступ отдельным включением.
CREATE TABLE ssh_keys (
    id           BIGSERIAL PRIMARY KEY,
    user_id      BIGINT      NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    name         TEXT        NOT NULL,
    algorithm    TEXT        NOT NULL,
    public_key   TEXT        NOT NULL, -- строка authorized_keys без параметров и комментария
    fingerprint  TEXT        NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_used_at TIMESTAMPTZ
);
-- Один ключ — один аккаунт: по ключу определяется, кто вошёл.
CREATE UNIQUE INDEX ssh_keys_fingerprint_key ON ssh_keys (fingerprint);
CREATE INDEX ssh_keys_user_idx ON ssh_keys (user_id);

CREATE TABLE site_shell (
    site_id    BIGINT PRIMARY KEY REFERENCES sites (id) ON DELETE CASCADE,
    enabled_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE site_shell;
DROP TABLE ssh_keys;
