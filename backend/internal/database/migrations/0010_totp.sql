-- +goose Up

-- Код из приложения (TOTP) по желанию. Секрет нужен серверу в открытом виде, чтобы считать коды, поэтому хранится
-- зашифрованным (AES-256-GCM, ключ выводится из общего секрета сервера; шифртекст привязан к номеру пользователя).
--   totp_pending    — секрет, выданный при подключении, но ещё не подтверждённый кодом;
--   totp_secret     — действующий секрет; заполнен ровно тогда, когда заполнено totp_enabled_at;
--   totp_last_step  — последний принятый 30-секундный шаг: тот же код второй раз не принимается.
ALTER TABLE users
    ADD COLUMN totp_pending    bytea,
    ADD COLUMN totp_secret     bytea,
    ADD COLUMN totp_enabled_at timestamptz,
    ADD COLUMN totp_last_step  bigint NOT NULL DEFAULT 0,
    ADD CONSTRAINT users_totp_consistent CHECK ((totp_secret IS NULL) = (totp_enabled_at IS NULL));

-- Одноразовые коды на случай потери телефона: в БД только SHA-256 (код случайный, 50 бит, перебор ограничен лимитами входа).
CREATE TABLE totp_recovery_codes (
    id         bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id    bigint      NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    code_hash  bytea       NOT NULL,
    created_at timestamptz NOT NULL,
    used_at    timestamptz,
    UNIQUE (user_id, code_hash)
);

-- +goose Down
DROP TABLE totp_recovery_codes;
ALTER TABLE users
    DROP CONSTRAINT users_totp_consistent,
    DROP COLUMN totp_last_step,
    DROP COLUMN totp_enabled_at,
    DROP COLUMN totp_secret,
    DROP COLUMN totp_pending;
