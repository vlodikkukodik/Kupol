-- +goose Up

-- История версий документа (этап 3, шаг 3.2). Снимок — полное содержимое документа на момент записи
-- (название, допуск, дата, свойства, блоки); шифр, тип и статус хранятся в самом документе и в колонке status.
--
-- Что хранится всегда (permanent): создание, смена статуса, правка опубликованного, загрузка из файла, откат.
-- Автосохранения и обычные сохранения черновиков — «скользящие»: сервис оставляет последние 30 на документ.
CREATE TABLE document_versions (
    id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    document_id bigint      NOT NULL REFERENCES documents (id) ON DELETE CASCADE,
    -- номер редакции документа, на которую опирается снимок
    revision    integer     NOT NULL CHECK (revision >= 1),
    kind        text        NOT NULL CHECK (kind IN ('create', 'autosave', 'save', 'edit', 'status', 'import', 'rollback')),
    -- статус документа в момент снимка
    status      text        NOT NULL CHECK (status IN ('draft', 'review', 'published', 'archived')),
    permanent   boolean     NOT NULL,
    -- автор изменения; пусто, если это команда на сервере или автор удалил аккаунт
    author_id   bigint      REFERENCES users (id) ON DELETE SET NULL,
    note        text        NOT NULL DEFAULT '',
    content     jsonb       NOT NULL,
    created_at  timestamptz NOT NULL,
    -- «хранится всегда» определяется видом снимка, а не произволом вызывающего
    CONSTRAINT document_versions_permanent_by_kind CHECK (permanent = (kind NOT IN ('autosave', 'save')))
);
CREATE INDEX document_versions_document_idx ON document_versions (document_id, id DESC);
CREATE INDEX document_versions_rolling_idx ON document_versions (document_id, id DESC) WHERE NOT permanent;

-- «Взят в работу»: документ правит один человек, остальным — только чтение. Замок снимается по таймауту
-- (expires_at) или вручную; просроченный замок считается снятым.
CREATE TABLE document_locks (
    document_id bigint      PRIMARY KEY REFERENCES documents (id) ON DELETE CASCADE,
    user_id     bigint      NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    acquired_at timestamptz NOT NULL,
    expires_at  timestamptz NOT NULL,
    CONSTRAINT document_locks_expiry CHECK (expires_at > acquired_at)
);

-- +goose Down
DROP TABLE document_locks;
DROP TABLE document_versions;
