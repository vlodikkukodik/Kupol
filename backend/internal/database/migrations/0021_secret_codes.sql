-- +goose Up

-- Скрытые коды (пасхалки, спецификация §4/§6, шаг 5.5): Директорат заводит код на документ (целиком
-- или с якорем на блок внутри него), верный код открывает документ аккаунту и даёт XP.
CREATE TABLE secret_codes (
    id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    code        citext      NOT NULL UNIQUE,
    document_id bigint      NOT NULL REFERENCES documents (id) ON DELETE CASCADE,
    -- block_id — якорь: докрутить к этому блоку после открытия; на саму видимость блока не влияет.
    block_id    text,
    reward_xp   integer     NOT NULL DEFAULT 0 CHECK (reward_xp >= 0),
    note        text        NOT NULL DEFAULT '',
    created_at  timestamptz NOT NULL
);

CREATE TABLE secret_code_redemptions (
    user_id     bigint      NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    code_id     bigint      NOT NULL REFERENCES secret_codes (id) ON DELETE CASCADE,
    redeemed_at timestamptz NOT NULL,
    PRIMARY KEY (user_id, code_id)
);
CREATE INDEX secret_code_redemptions_user_idx ON secret_code_redemptions (user_id);

-- +goose Down
DROP TABLE secret_code_redemptions;
DROP TABLE secret_codes;
