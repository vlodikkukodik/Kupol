-- +goose Up
-- Ежедневные снимки файлов сайта. Один снимок на site_id в сутки (UTC); хранятся 7 суток.
-- Содержимое лежит на диске в {BackupDir}/{site_id}/{day}/ (hardlink на public/).
CREATE TABLE site_backups (
    id        BIGSERIAL PRIMARY KEY,
    site_id   BIGINT      NOT NULL REFERENCES sites (id) ON DELETE CASCADE,
    day       DATE        NOT NULL,
    bytes     BIGINT      NOT NULL DEFAULT 0,
    files     INTEGER     NOT NULL DEFAULT 0,
    taken_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX site_backups_site_day ON site_backups (site_id, day);

-- +goose Down
DROP TABLE site_backups;
