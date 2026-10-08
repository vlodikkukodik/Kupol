-- +goose Up
-- Правила ящика, которые задаются в панели: автоответчик и пересылка на другие адреса (с копией или без). На сервере они превращаются
-- в скрипт sieve ящика (deploy/bin/mail-sync.py).
ALTER TABLE mailboxes
    ADD COLUMN autoreply_enabled BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN autoreply_subject TEXT    NOT NULL DEFAULT '',
    ADD COLUMN autoreply_body    TEXT    NOT NULL DEFAULT '',
    ADD COLUMN autoreply_from    TEXT    NOT NULL DEFAULT '', -- «ГГГГ-ММ-ДД» или пусто: отвечать с этого дня
    ADD COLUMN autoreply_to      TEXT    NOT NULL DEFAULT '', -- «ГГГГ-ММ-ДД» или пусто: отвечать по этот день включительно
    ADD COLUMN autoreply_days    INT     NOT NULL DEFAULT 1,  -- не чаще одного ответа одному адресу за столько дней
    ADD COLUMN forward_to        TEXT    NOT NULL DEFAULT '', -- адреса через перевод строки; пусто — пересылки нет
    ADD COLUMN forward_keep      BOOLEAN NOT NULL DEFAULT true; -- оставлять ли копию письма в ящике

-- +goose Down
ALTER TABLE mailboxes
    DROP COLUMN forward_keep,
    DROP COLUMN forward_to,
    DROP COLUMN autoreply_days,
    DROP COLUMN autoreply_to,
    DROP COLUMN autoreply_from,
    DROP COLUMN autoreply_body,
    DROP COLUMN autoreply_subject,
    DROP COLUMN autoreply_enabled;
