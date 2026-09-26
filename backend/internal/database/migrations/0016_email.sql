-- +goose Up

-- Почта — по желанию (шаг 5.1.1): спецификация «без почты» смягчена по решению автора, письма нужны для уведомлений
-- (подтверждение адреса, повышение уровня — internal/mail). Вход по-прежнему только логином и паролем, почты без
-- подтверждения не бывает: pending_email ждёт перехода по ссылке из письма, email — уже подтверждённая.
ALTER TABLE users
    ADD COLUMN email         citext,
    ADD COLUMN pending_email citext,
    ADD CONSTRAINT users_email_length CHECK (email IS NULL OR char_length(email::text) BETWEEN 3 AND 254),
    ADD CONSTRAINT users_pending_email_length CHECK (pending_email IS NULL OR char_length(pending_email::text) BETWEEN 3 AND 254);

CREATE UNIQUE INDEX users_email_unique_idx ON users (email) WHERE email IS NOT NULL;

-- Ссылки подтверждения: token_hash — SHA-256 случайного токена из письма (как sessions.token_hash), утечка таблицы
-- не даёт подтвердить чужую почту. Старые записи пользователя гасятся при новом запросе (см. accounts.SetEmail).
CREATE TABLE email_confirmations (
    id         bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id    bigint      NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    email      citext      NOT NULL,
    token_hash bytea       NOT NULL UNIQUE,
    created_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL
);
CREATE INDEX email_confirmations_user_id_idx ON email_confirmations (user_id);

-- +goose Down
DROP TABLE email_confirmations;
ALTER TABLE users
    DROP COLUMN email,
    DROP COLUMN pending_email;
