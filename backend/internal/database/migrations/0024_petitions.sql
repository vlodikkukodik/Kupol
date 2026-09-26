-- +goose Up

-- Ходатайства о повышении допуска до 4–6 (шаг 5.8): подаёт читатель из личного дела, решает Особый Совет или Директорат.
CREATE TABLE petitions (
    id           bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id      bigint      NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    target_level smallint    NOT NULL CHECK (target_level BETWEEN 4 AND 6),
    text         text        NOT NULL,
    status       text        NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'approved', 'rejected')),
    comment      text        NOT NULL DEFAULT '',
    decided_by   bigint      REFERENCES users (id) ON DELETE SET NULL,
    decided_at   timestamptz,
    created_at   timestamptz NOT NULL
);
-- не больше одного нерассмотренного ходатайства на человека
CREATE UNIQUE INDEX petitions_one_pending_key ON petitions (user_id) WHERE status = 'pending';
CREATE INDEX petitions_status_idx ON petitions (status, created_at);

-- +goose Down
DROP TABLE petitions;
