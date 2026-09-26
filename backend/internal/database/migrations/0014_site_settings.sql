-- +goose Up

-- Настройки сайта, которые правит Директорат в панели команды (сейчас — контакты автора на странице «О КУПОЛЕ»).
-- Ключ-значение: новые настройки не требуют миграций. Пустого значения в таблице нет: очищенная настройка удаляется.
CREATE TABLE site_settings (
    key        text PRIMARY KEY CHECK (key ~ '^[a-z][a-z_]{0,39}$'),
    value      text        NOT NULL CHECK (char_length(value) BETWEEN 1 AND 1000),
    updated_at timestamptz NOT NULL,
    updated_by bigint REFERENCES users (id) ON DELETE SET NULL
);

-- +goose Down
DROP TABLE site_settings;
