-- +goose Up
-- Двухфакторный вход (TOTP): секрет хранится зашифрованным (ключ из секрета панели); pending — секрет, который ещё не подтвердили кодом.
-- totp_last_step — последний принятый шаг времени: один и тот же код нельзя предъявить дважды.
ALTER TABLE users
    ADD COLUMN totp_secret     TEXT,
    ADD COLUMN totp_pending    TEXT,
    ADD COLUMN totp_enabled_at TIMESTAMPTZ,
    ADD COLUMN totp_last_step  BIGINT NOT NULL DEFAULT 0;

-- Одноразовые коды восстановления (на случай потери телефона): только хеши.
CREATE TABLE recovery_codes (
    id         BIGSERIAL PRIMARY KEY,
    user_id    BIGINT      NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    code_hash  TEXT        NOT NULL,
    used_at    TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX recovery_codes_user_idx ON recovery_codes (user_id);

-- Сессия — вход с одного устройства. Refresh-токены внутри неё меняются при каждом обновлении, сама сессия остаётся:
-- её показывают в списке и закрывают целиком.
CREATE TABLE sessions (
    id           BIGSERIAL PRIMARY KEY,
    user_id      BIGINT      NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    ip           TEXT        NOT NULL DEFAULT '',
    user_agent   TEXT        NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_seen_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at   TIMESTAMPTZ NOT NULL,
    revoked_at   TIMESTAMPTZ
);
CREATE INDEX sessions_user_idx ON sessions (user_id);

ALTER TABLE refresh_tokens ADD COLUMN session_id BIGINT REFERENCES sessions (id) ON DELETE CASCADE;

-- Действующие входы до обновления становятся сессиями (номер сессии = номер токена), чтобы их было видно в списке и можно было закрыть.
INSERT INTO sessions (id, user_id, created_at, last_seen_at, expires_at)
SELECT id, user_id, created_at, created_at, expires_at FROM refresh_tokens WHERE revoked_at IS NULL AND expires_at > now();
UPDATE refresh_tokens SET session_id = id WHERE revoked_at IS NULL AND expires_at > now();
SELECT setval(pg_get_serial_sequence('sessions', 'id'), COALESCE((SELECT max(id) FROM sessions), 0) + 1, false);

-- +goose Down
ALTER TABLE refresh_tokens DROP COLUMN session_id;
DROP TABLE sessions;
DROP TABLE recovery_codes;
ALTER TABLE users
    DROP COLUMN totp_secret,
    DROP COLUMN totp_pending,
    DROP COLUMN totp_enabled_at,
    DROP COLUMN totp_last_step;
