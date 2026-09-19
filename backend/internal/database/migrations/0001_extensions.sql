-- +goose Up
-- citext: логины уникальны без учёта регистра (этап 1).
-- pg_trgm: нечёткий поиск и автодополнение по шифрам/названиям (этапы 2, 5).
CREATE EXTENSION IF NOT EXISTS citext;
CREATE EXTENSION IF NOT EXISTS pg_trgm;

-- +goose Down
DROP EXTENSION IF EXISTS pg_trgm;
DROP EXTENSION IF EXISTS citext;
