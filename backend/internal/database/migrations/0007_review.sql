-- +goose Up

-- Рецензия документов (этап 3, шаг 3.5).
--
-- Комментарии рецензента привязаны к блоку по его идентификатору (blocks[].id) либо к документу целиком (block_id пуст).
-- Блок могут потом удалить или переставить: комментарий остаётся и показывается как «блок удалён». Комментарий пишется
-- на конкретную редакцию (revision): видно, к какому тексту он относился.
CREATE TABLE review_comments (
    id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    document_id bigint      NOT NULL REFERENCES documents (id) ON DELETE CASCADE,
    block_id    text        CHECK (block_id IS NULL OR (block_id <> '' AND char_length(block_id) <= 32)),
    revision    integer     NOT NULL CHECK (revision >= 1),
    -- автор комментария; пусто, если он удалил аккаунт (текст остаётся: он часть истории документа)
    author_id   bigint      REFERENCES users (id) ON DELETE SET NULL,
    body        text        NOT NULL CHECK (char_length(btrim(body)) BETWEEN 1 AND 2000),
    -- «исправлено»: отмечает автор документа или рецензент
    resolved_at timestamptz,
    resolved_by bigint      REFERENCES users (id) ON DELETE SET NULL,
    created_at  timestamptz NOT NULL,
    -- отметивший без времени невозможен (наоборот бывает: отметивший мог удалить аккаунт)
    CONSTRAINT review_comments_resolved_by_needs_time CHECK (resolved_by IS NULL OR resolved_at IS NOT NULL)
);
CREATE INDEX review_comments_document_idx ON review_comments (document_id, id);

-- Ход рецензии: отправка на проверку, отзыв автором, вердикты. Причина возврата или отклонения хранится здесь и видна автору.
-- Это история документа, а не журнал аудита (тот — audit_events): она удаляется вместе с документом.
CREATE TABLE review_events (
    id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    document_id bigint      NOT NULL REFERENCES documents (id) ON DELETE CASCADE,
    kind        text        NOT NULL CHECK (kind IN ('submit', 'withdraw', 'approve', 'return', 'reject', 'archive', 'unarchive')),
    revision    integer     NOT NULL CHECK (revision >= 1),
    actor_id    bigint      REFERENCES users (id) ON DELETE SET NULL,
    comment     text        NOT NULL DEFAULT '' CHECK (char_length(comment) <= 2000),
    created_at  timestamptz NOT NULL,
    -- возврат и отклонение без причины бессмысленны: автору нечего исправлять
    CONSTRAINT review_events_reason_required CHECK (kind NOT IN ('return', 'reject') OR btrim(comment) <> '')
);
CREATE INDEX review_events_document_idx ON review_events (document_id, id);

-- +goose Down
DROP TABLE review_events;
DROP TABLE review_comments;
