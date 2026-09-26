-- +goose Up

-- Пользователи. Гражданин (уровень 0) — это не запись, а посетитель без входа.
CREATE TABLE users (
    id                  bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    -- Логин уникален без учёта регистра (citext), отображается так, как его ввели.
    login               citext      NOT NULL UNIQUE,
    password_hash       text        NOT NULL,
    -- Резервный код для восстановления доступа; хранится только хеш.
    backup_code_hash    text        NOT NULL,
    -- 1 Посетитель … 6 Особый Совет (см. спецификацию §3). Уровни 2–3 растут по XP (этап 4),
    -- 4–6 выдаёт Особый Совет; Директорат — отдельный флаг, выдаётся только автором.
    level               smallint    NOT NULL DEFAULT 1 CHECK (level BETWEEN 1 AND 6),
    directorate         boolean     NOT NULL DEFAULT false,
    created_at          timestamptz NOT NULL,
    last_login_at       timestamptz,
    password_changed_at timestamptz NOT NULL,
    CONSTRAINT users_login_length CHECK (char_length(login::text) BETWEEN 3 AND 24)
);

-- Серверные сессии. В куке лежит случайный токен, в БД — только его SHA-256:
-- утечка таблицы не даёт войти. Удаление строки мгновенно завершает сессию.
CREATE TABLE sessions (
    id                bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    token_hash        bytea       NOT NULL UNIQUE,
    user_id           bigint      NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    created_at        timestamptz NOT NULL,
    last_seen_at      timestamptz NOT NULL,
    -- скользящий срок: продлевается при активности, но не дальше absolute_expires_at
    expires_at        timestamptz NOT NULL,
    absolute_expires_at timestamptz NOT NULL,
    ip                inet,
    user_agent        text        NOT NULL DEFAULT ''
);
CREATE INDEX sessions_user_id_idx ON sessions (user_id);
CREATE INDEX sessions_expires_at_idx ON sessions (expires_at);

-- Одноразовые вопросы-«анкеты» для регистрации: ответ проверяется ровно один раз.
CREATE TABLE captcha_challenges (
    id          uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    question_id text        NOT NULL,
    expires_at  timestamptz NOT NULL
);
CREATE INDEX captcha_challenges_expires_at_idx ON captcha_challenges (expires_at);

-- +goose Down
DROP TABLE captcha_challenges;
DROP TABLE sessions;
DROP TABLE users;
