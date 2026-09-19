-- +goose Up

-- Журнал событий (аудит): кто, когда и что сделал с ролями, замками документов, откатами и паролями.
-- История самих документов хранится в document_versions; здесь — то, что версиями не описывается.
--
-- Персональных данных, кроме ссылок на пользователя, в журнале нет. Ссылки — ON DELETE SET NULL: когда человек
-- «сдаёт дело в архив» и аккаунт удаляется, его следы в журнале обезличиваются (запись остаётся, имя — нет).
CREATE TABLE audit_events (
    id             bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    at             timestamptz NOT NULL,
    -- машинное имя события: role.granted, lock.broken, document.rolled_back, …
    action         text        NOT NULL CHECK (action <> '' AND length(action) <= 64),
    -- кто сделал; пусто — команда на сервере (автор) или аккаунт удалён
    actor_id       bigint      REFERENCES users (id) ON DELETE SET NULL,
    -- над кем сделано (например, кому выдана роль)
    target_user_id bigint      REFERENCES users (id) ON DELETE SET NULL,
    document_id    bigint      REFERENCES documents (id) ON DELETE SET NULL,
    -- подробности события: роль, номер версии, прежний владелец замка — без секретов и без паролей
    details        jsonb       NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(details) = 'object')
);
CREATE INDEX audit_events_at_idx ON audit_events (at DESC, id DESC);
CREATE INDEX audit_events_action_idx ON audit_events (action, at DESC);
CREATE INDEX audit_events_actor_idx ON audit_events (actor_id, at DESC) WHERE actor_id IS NOT NULL;
CREATE INDEX audit_events_target_idx ON audit_events (target_user_id, at DESC) WHERE target_user_id IS NOT NULL;
CREATE INDEX audit_events_document_idx ON audit_events (document_id, at DESC) WHERE document_id IS NOT NULL;

-- +goose Down
DROP TABLE audit_events;
