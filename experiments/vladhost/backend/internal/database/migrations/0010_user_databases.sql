-- +goose Up
-- Пользовательские базы данных (PostgreSQL и MariaDB). Сами базы живут в отдельных серверах СУБД; здесь только учёт:
-- кому принадлежит база, её состояние (frozen — превышен лимит размера, разрешены только чтение и удаление данных),
-- последний замер размера и список IP, которым разрешён внешний доступ.
CREATE TABLE user_databases (
    id              BIGSERIAL PRIMARY KEY,
    user_id         BIGINT      NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    engine          TEXT        NOT NULL CHECK (engine IN ('postgres', 'mariadb')),
    name            TEXT        NOT NULL, -- полное имя базы и её пользователя: {пользователь панели}_{имя}
    status          TEXT        NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'frozen')),
    size_bytes      BIGINT      NOT NULL DEFAULT 0,
    size_checked_at TIMESTAMPTZ,
    frozen_at       TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX user_databases_name_key ON user_databases (engine, name);
CREATE INDEX user_databases_user_idx ON user_databases (user_id);

-- Внешний доступ: адрес или сеть, с которых можно подключаться к базе по TLS. Пусто — только веб-клиент панели.
CREATE TABLE database_allowed_ips (
    id          BIGSERIAL PRIMARY KEY,
    database_id BIGINT      NOT NULL REFERENCES user_databases (id) ON DELETE CASCADE,
    addr        TEXT        NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (database_id, addr)
);

-- Временные учётные записи веб-клиента (входят из панели одним щелчком): удаляются по истечении срока.
CREATE TABLE database_temp_accounts (
    id          BIGSERIAL PRIMARY KEY,
    database_id BIGINT      NOT NULL REFERENCES user_databases (id) ON DELETE CASCADE,
    account     TEXT        NOT NULL,
    expires_at  TIMESTAMPTZ NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX database_temp_accounts_exp_idx ON database_temp_accounts (expires_at);

-- +goose Down
DROP TABLE database_temp_accounts;
DROP TABLE database_allowed_ips;
DROP TABLE user_databases;
