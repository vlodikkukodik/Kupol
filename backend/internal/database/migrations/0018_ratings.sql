-- +goose Up

-- «Оценки» (шаг 5.3, спецификация §8): «ознакомлен / одобряю / сомнительно» — канцелярские
-- отметки читателей под документами. Публичные счётчики штампами с числами (видит каждый
-- залогиненный); +5 XP за оценку (до 10/день). У одного пользователя на документ — одна
-- оценка (можно изменить или удалить).
CREATE TABLE document_ratings (
    id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    document_id bigint      NOT NULL REFERENCES documents (id) ON DELETE CASCADE,
    user_id     bigint      NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    rating      text        NOT NULL CHECK (rating IN ('acknowledged', 'approved', 'doubtful')),
    created_at  timestamptz NOT NULL,
    UNIQUE (document_id, user_id)
);
CREATE INDEX document_ratings_document_id_idx ON document_ratings (document_id);
CREATE INDEX document_ratings_user_id_idx ON document_ratings (user_id);

-- +goose Down
DROP TABLE document_ratings;
