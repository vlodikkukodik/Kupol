-- +goose Up

-- Наказания (шаг 5.9): предупреждение → временная блокировка комментариев → бан аккаунта; причина в журнале.
CREATE TABLE sanctions (
    id         bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id    bigint      NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    kind       text        NOT NULL CHECK (kind IN ('warning', 'comment_ban', 'ban')),
    reason     text        NOT NULL,
    -- конец блокировки комментариев; у предупреждения и бана пусто
    expires_at timestamptz,
    issued_by  bigint      REFERENCES users (id) ON DELETE SET NULL,
    created_at timestamptz NOT NULL,
    revoked_at timestamptz,
    revoked_by bigint      REFERENCES users (id) ON DELETE SET NULL,
    CONSTRAINT sanctions_expiry_only_comment_ban CHECK ((kind = 'comment_ban') = (expires_at IS NOT NULL))
);
CREATE INDEX sanctions_user_idx ON sanctions (user_id, created_at DESC);

-- бан: вход закрыт, сессии сняты; снимается отзывом наказания
ALTER TABLE users ADD COLUMN banned_at timestamptz;

-- +goose Down
ALTER TABLE users DROP COLUMN banned_at;
DROP TABLE sanctions;
