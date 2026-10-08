-- +goose Up

-- Перевод дела на второй язык интерфейса (сейчас — только it). Ключ ru не используется:
-- существующие колонки title/blocks остаются каноническим (русским) содержимым.
ALTER TABLE documents ADD COLUMN translations jsonb NOT NULL DEFAULT '{}'::jsonb;
ALTER TABLE documents ADD CONSTRAINT documents_translations_is_object
    CHECK (jsonb_typeof(translations) = 'object');

-- +goose Down
ALTER TABLE documents DROP COLUMN translations;
