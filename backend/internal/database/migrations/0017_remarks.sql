-- +goose Up

-- «Пометки на полях» — комментарии читателей под документами (шаг 5.2, спецификация §8): ветки с ответами,
-- с уровня 1 (Посетитель), публикуются сразу. Видимость треда не зависит от уровня — если документ виден
-- читателю, видны и все его пометки; закрытых пометок не бывает.
CREATE TABLE remarks (
    id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    document_id bigint      NOT NULL REFERENCES documents (id) ON DELETE CASCADE,
    parent_id   bigint      REFERENCES remarks (id) ON DELETE CASCADE,
    author_id   bigint      NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    text        text        NOT NULL CHECK (char_length(text) BETWEEN 1 AND 2000),
    created_at  timestamptz NOT NULL,
    report_count integer    NOT NULL DEFAULT 0
);
CREATE INDEX remarks_document_id_idx ON remarks (document_id);
CREATE INDEX remarks_parent_id_idx ON remarks (parent_id);
-- жалобы модераторам разбирают по свежести
CREATE INDEX remarks_reported_idx ON remarks (report_count DESC, created_at) WHERE report_count > 0;

-- одна жалоба на пометку от одного читателя
CREATE TABLE remark_reports (
    remark_id  bigint      NOT NULL REFERENCES remarks (id) ON DELETE CASCADE,
    user_id    bigint      NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL,
    PRIMARY KEY (remark_id, user_id)
);

-- +goose Down
DROP TABLE remark_reports;
DROP TABLE remarks;
