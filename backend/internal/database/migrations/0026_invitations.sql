-- +goose Up

-- Приглашения Совета на уровни 4–6 (шаг 5.8+): Особый Совет или Директорат приглашает читателя без ходатайства;
-- читатель принимает (уровень поднимается) или отклоняет.
CREATE TABLE invitations (
    id           bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id      bigint      NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    target_level smallint    NOT NULL CHECK (target_level BETWEEN 4 AND 6),
    message      text        NOT NULL DEFAULT '',
    status       text        NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'accepted', 'declined', 'withdrawn')),
    invited_by   bigint      REFERENCES users (id) ON DELETE SET NULL,
    created_at   timestamptz NOT NULL,
    answered_at  timestamptz
);
CREATE UNIQUE INDEX invitations_one_pending_key ON invitations (user_id) WHERE status = 'pending';
CREATE INDEX invitations_status_idx ON invitations (status, created_at);

-- +goose Down
DROP TABLE invitations;
