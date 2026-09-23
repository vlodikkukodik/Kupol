-- +goose Up
-- none    — выпуск сертификатов выключен (локальная разработка);
-- pending — заявка отправлена, ждём результат; active — сертификат есть; failed — выпуск не удался.
ALTER TABLE sites
    ADD COLUMN cert_status       TEXT        NOT NULL DEFAULT 'none' CHECK (cert_status IN ('none', 'pending', 'active', 'failed')),
    ADD COLUMN cert_error        TEXT        NOT NULL DEFAULT '',
    ADD COLUMN cert_requested_at TIMESTAMPTZ;

-- +goose Down
ALTER TABLE sites DROP COLUMN cert_status, DROP COLUMN cert_error, DROP COLUMN cert_requested_at;
