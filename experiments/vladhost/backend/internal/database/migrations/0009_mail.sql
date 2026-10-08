-- +goose Up
-- Почта: подтверждение адреса, язык писем и согласие на уведомления, одноразовые ссылки из писем, очередь отправки
-- и журнал уже отправленных уведомлений (чтобы не слать одно и то же повторно).
ALTER TABLE users
    ADD COLUMN email_verified_at TIMESTAMPTZ,
    ADD COLUMN lang              TEXT    NOT NULL DEFAULT 'ru' CHECK (lang IN ('ru', 'it')),
    ADD COLUMN notify_email      BOOLEAN NOT NULL DEFAULT true;
-- Администраторов заводит оператор сервера, их адрес считается подтверждённым.
UPDATE users SET email_verified_at = created_at WHERE role = 'admin';

-- Ссылки из писем: в БД только хеш токена; verify — подтверждение адреса (email — какой именно), reset — сброс пароля.
CREATE TABLE mail_tokens (
    id         BIGSERIAL PRIMARY KEY,
    user_id    BIGINT      NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    kind       TEXT        NOT NULL CHECK (kind IN ('verify', 'reset')),
    token_hash TEXT        NOT NULL UNIQUE,
    email      TEXT        NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    used_at    TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX mail_tokens_user_idx ON mail_tokens (user_id, kind, created_at);

-- Очередь писем. Тела хранятся только до отправки (в них бывают ссылки для входа): после успеха или окончательной
-- неудачи они стираются.
CREATE TABLE mail_outbox (
    id              BIGSERIAL PRIMARY KEY,
    user_id         BIGINT      REFERENCES users (id) ON DELETE SET NULL,
    kind            TEXT        NOT NULL,
    to_email        TEXT        NOT NULL,
    subject         TEXT        NOT NULL,
    text_body       TEXT        NOT NULL,
    html_body       TEXT        NOT NULL DEFAULT '',
    status          TEXT        NOT NULL DEFAULT 'queued' CHECK (status IN ('queued', 'sent', 'failed')),
    attempts        INT         NOT NULL DEFAULT 0,
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_error      TEXT        NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    sent_at         TIMESTAMPTZ
);
CREATE INDEX mail_outbox_due_idx ON mail_outbox (next_attempt_at) WHERE status = 'queued';

-- Отправленные уведомления: одно и то же событие (kind + dedupe_key) пользователю не повторяется.
CREATE TABLE notifications (
    id         BIGSERIAL PRIMARY KEY,
    user_id    BIGINT      NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    kind       TEXT        NOT NULL,
    dedupe_key TEXT        NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (user_id, kind, dedupe_key)
);

-- +goose Down
DROP TABLE notifications;
DROP TABLE mail_outbox;
DROP TABLE mail_tokens;
ALTER TABLE users DROP COLUMN email_verified_at, DROP COLUMN lang, DROP COLUMN notify_email;
