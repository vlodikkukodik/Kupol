-- +goose Up
-- Дополнительные FTP-аккаунты сайта: свой логин ({имя}.{сайт}.{пользователь}), свой пароль, ограничение подпапкой
-- и режим «только чтение». Основной доступ сайта (sites.ftp_enabled) остаётся как был.
CREATE TABLE ftp_accounts (
    id            BIGSERIAL PRIMARY KEY,
    site_id       BIGINT      NOT NULL REFERENCES sites (id) ON DELETE CASCADE,
    name          TEXT        NOT NULL,
    password_hash TEXT        NOT NULL,
    dir           TEXT        NOT NULL DEFAULT '',
    read_only     BOOLEAN     NOT NULL DEFAULT false,
    enabled       BOOLEAN     NOT NULL DEFAULT true,
    last_login_at TIMESTAMPTZ,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX ftp_accounts_name_key ON ftp_accounts (site_id, name);

-- +goose Down
DROP TABLE ftp_accounts;
