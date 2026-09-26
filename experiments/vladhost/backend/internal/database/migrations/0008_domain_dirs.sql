-- +goose Up
-- Поддомены сайта на нашем домене ({метка}.{сайт}.{пользователь}.vladinc.ru, kind = 'sub') и папка сайта, которую
-- отдаёт конкретное имя (dir; пусто — весь сайт). Свои домены (kind = 'custom') работают как раньше.
ALTER TABLE domains
    ADD COLUMN kind TEXT NOT NULL DEFAULT 'custom' CHECK (kind IN ('custom', 'sub')),
    ADD COLUMN dir  TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE domains DROP COLUMN kind, DROP COLUMN dir;
