-- +goose Up

-- Почтовый сервер, который включает Директорат в панели команды (шаг 5.1.1). Строки в таблице нет, пока
-- настройки не сохраняли: тогда действует окружение (KUPOL_SMTP_*). Пароль хранится зашифрованным
-- (AES-GCM, ключ выводится из общего секрета сервера — см. internal/mailsettings).
CREATE TABLE smtp_settings (
    singleton  boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    enabled    boolean      NOT NULL DEFAULT false,
    host       text         NOT NULL DEFAULT '' CHECK (char_length(host) <= 255),
    port       text         NOT NULL DEFAULT '587' CHECK (char_length(port) BETWEEN 1 AND 5),
    username   text         NOT NULL DEFAULT '' CHECK (char_length(username) <= 255),
    password   bytea        NOT NULL DEFAULT ''::bytea,
    from_addr  text         NOT NULL DEFAULT '' CHECK (char_length(from_addr) <= 320),
    from_name  text         NOT NULL DEFAULT '' CHECK (char_length(from_name) <= 255),
    updated_at timestamptz,
    updated_by bigint REFERENCES users (id) ON DELETE SET NULL
);

-- Язык читателя для писем, которые уходят без его запроса (уведомления); выбирается в интерфейсе.
ALTER TABLE users ADD COLUMN lang text NOT NULL DEFAULT 'ru' CHECK (lang IN ('ru', 'it'));

-- О чём читатель хочет получать письма-уведомления: копии записок внутренней почты (internal/inbox).
-- Значения по умолчанию — все виды записок; выключить может сам читатель.
ALTER TABLE users ADD COLUMN email_prefs jsonb NOT NULL DEFAULT
    '{"enabled":true,"note":true,"level_up":true,"achievement":true,"suggestion":true,"remark_reply":true,"petition":true,"invitation":true,"invitation_answer":true,"sanction":true}'
    CHECK (jsonb_typeof(email_prefs) = 'object');

-- Очередь писем: записка кладётся сюда в той же транзакции, что и в ящик, а письмо уходит после фиксации —
-- фоновая отправка (см. internal/inbox) не зависит от исхода транзакции и переживает перезапуск.
CREATE TABLE email_outbox (
    id         bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id    bigint NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    kind       text   NOT NULL,
    params     jsonb  NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL,
    attempts   integer NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    CONSTRAINT email_outbox_params_is_object CHECK (jsonb_typeof(params) = 'object')
);

-- +goose Down
DROP TABLE email_outbox;
ALTER TABLE users DROP COLUMN email_prefs;
ALTER TABLE users DROP COLUMN lang;
DROP TABLE smtp_settings;
