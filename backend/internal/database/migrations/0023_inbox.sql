-- +goose Up

-- Внутренняя почта (шаг 5.7): записки читателю в интерфейсе (не письма на почту). Хранятся вид и параметры,
-- текст собирается при чтении на языке читателя; записка Директората — свободный текст в params.
CREATE TABLE inbox_messages (
    id         bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id    bigint      NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    kind       text        NOT NULL,
    params     jsonb       NOT NULL DEFAULT '{}',
    created_at timestamptz NOT NULL,
    read_at    timestamptz,
    CONSTRAINT inbox_params_is_object CHECK (jsonb_typeof(params) = 'object')
);
CREATE INDEX inbox_user_idx ON inbox_messages (user_id, created_at DESC, id DESC);
CREATE INDEX inbox_unread_idx ON inbox_messages (user_id) WHERE read_at IS NULL;

-- +goose Down
DROP TABLE inbox_messages;
