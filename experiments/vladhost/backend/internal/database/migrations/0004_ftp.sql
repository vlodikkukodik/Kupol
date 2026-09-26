-- +goose Up
-- У каждого сайта свой FTP-доступ с отдельным паролем (не паролем аккаунта). Пустой хеш — доступ не выдавался.
ALTER TABLE sites
    ADD COLUMN ftp_enabled       BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN ftp_password_hash TEXT     NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE sites DROP COLUMN ftp_enabled, DROP COLUMN ftp_password_hash;
