-- +goose Up
-- Свои домены сайтов: пользователь направляет A-запись домена на IP сервера, панель проверяет DNS и выпускает
-- сертификат. status: pending_dns — ждём A-запись; pending_cert — DNS верный, сертификат выпускается;
-- active — работает; failed — выпуск сертификата не удался.
CREATE TABLE domains (
    id                BIGSERIAL PRIMARY KEY,
    site_id           BIGINT      NOT NULL REFERENCES sites (id) ON DELETE CASCADE,
    host              TEXT        NOT NULL,
    status            TEXT        NOT NULL DEFAULT 'pending_dns' CHECK (status IN ('pending_dns', 'pending_cert', 'active', 'failed')),
    problem           TEXT        NOT NULL DEFAULT '',
    found             TEXT        NOT NULL DEFAULT '',
    error             TEXT        NOT NULL DEFAULT '',
    dns_checked_at    TIMESTAMPTZ,
    verified_at       TIMESTAMPTZ,
    cert_requested_at TIMESTAMPTZ,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX domains_host_key ON domains (host);
CREATE INDEX domains_site_idx ON domains (site_id);

-- +goose Down
DROP TABLE domains;
