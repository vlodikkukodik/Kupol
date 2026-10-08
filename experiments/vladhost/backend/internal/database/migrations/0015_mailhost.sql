-- +goose Up
-- Почта на своих доменах: домены, ящики и алиасы. Панель хранит желаемое состояние; на сервер оно попадает файлами для exim и dovecot
-- (deploy/bin/mail-sync.py).
CREATE TABLE mail_domains (
    id            BIGSERIAL PRIMARY KEY,
    user_id       BIGINT      NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    domain        TEXT        NOT NULL,
    enabled       BOOLEAN     NOT NULL DEFAULT true, -- выключенный домен не принимает почту
    dkim_selector TEXT        NOT NULL,
    dkim_private  TEXT        NOT NULL,              -- закрытый ключ подписи (PEM); в ответах API не показывается
    dkim_public   TEXT        NOT NULL,              -- открытая часть для записи DNS (base64 без переносов)
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX mail_domains_domain_key ON mail_domains (domain);
CREATE INDEX mail_domains_user_idx ON mail_domains (user_id);

CREATE TABLE mailboxes (
    id            BIGSERIAL PRIMARY KEY,
    domain_id     BIGINT      NOT NULL REFERENCES mail_domains (id) ON DELETE CASCADE,
    local_part    TEXT        NOT NULL,
    password_hash TEXT        NOT NULL, -- {SHA512-CRYPT}$6$…; пароль нигде не хранится
    quota_mb      INT         NOT NULL DEFAULT 500,
    enabled       BOOLEAN     NOT NULL DEFAULT true, -- выключенный ящик не пускает по паролю, но письма принимает
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX mailboxes_local_key ON mailboxes (domain_id, local_part);

CREATE TABLE mail_aliases (
    id           BIGSERIAL PRIMARY KEY,
    domain_id    BIGINT      NOT NULL REFERENCES mail_domains (id) ON DELETE CASCADE,
    local_part   TEXT        NOT NULL, -- «*» — общий ящик домена (всё, чему нет ящика и алиаса)
    destinations TEXT        NOT NULL, -- адреса через перевод строки
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX mail_aliases_local_key ON mail_aliases (domain_id, local_part);

-- Папки почты, которые нужно удалить с диска при следующей синхронизации (ящик или домен удалены).
CREATE TABLE mail_purge (
    path TEXT PRIMARY KEY -- «домен» или «домен/ящик»
);

-- +goose Down
DROP TABLE mail_purge;
DROP TABLE mail_aliases;
DROP TABLE mailboxes;
DROP TABLE mail_domains;
