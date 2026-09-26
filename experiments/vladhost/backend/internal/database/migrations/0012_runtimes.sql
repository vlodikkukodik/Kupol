-- +goose Up
-- Среда выполнения сайта: PHP (версия), Node.js или Python (команда запуска и порт приложения). Сайт без строки — статика.
-- Файл runtime.json рядом с public (его читает веб-шлюз) пишется из этой таблицы.
CREATE TABLE site_runtimes (
    site_id    BIGINT PRIMARY KEY REFERENCES sites (id) ON DELETE CASCADE,
    runtime    TEXT        NOT NULL CHECK (runtime IN ('php', 'node', 'python')),
    version    TEXT        NOT NULL DEFAULT '',
    command    TEXT        NOT NULL DEFAULT '',
    port       INT,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
-- Порт приложения уникален на весь сервер: приложения слушают 127.0.0.1.
CREATE UNIQUE INDEX site_runtimes_port_key ON site_runtimes (port) WHERE port IS NOT NULL;

-- +goose Down
DROP TABLE site_runtimes;
