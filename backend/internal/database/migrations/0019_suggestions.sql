-- +goose Up

-- «Предложения» (шаг 5.4, спецификация §8): одна форма (идея или замечание), очередь Редакторов,
-- статусы получено → рассмотрено → принято/отклонено. Принятое приносит автору +100 XP.
-- comment — пояснение Редактора, оно же «записка» автору (отдельная доставка внутренней почтой — шаг 5.7);
-- handled_by обезличивается при сдаче дела в архив (как в журнале аудита), текст предложения остаётся.
CREATE TABLE suggestions (
    id         bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    author_id  bigint      NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    text       text        NOT NULL CHECK (char_length(text) BETWEEN 1 AND 2000),
    status     text        NOT NULL DEFAULT 'received' CHECK (status IN ('received', 'reviewed', 'accepted', 'rejected')),
    comment    text        NOT NULL DEFAULT '' CHECK (char_length(comment) <= 2000),
    handled_by bigint      REFERENCES users (id) ON DELETE SET NULL,
    created_at timestamptz NOT NULL,
    handled_at timestamptz
);
-- очередь: разбор по свежести/давности внутри статуса
CREATE INDEX suggestions_status_created_idx ON suggestions (status, created_at);
CREATE INDEX suggestions_author_id_idx ON suggestions (author_id);

-- +goose Down
DROP TABLE suggestions;
