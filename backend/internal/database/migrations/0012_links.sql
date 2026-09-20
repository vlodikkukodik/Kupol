-- +goose Up

-- Обратные ссылки: какие документы ссылаются на данный блоками doc_link («Упоминается в» на странице документа, дальше — граф связей).
-- Строки строит код (documents/search_index.go) в той же транзакции, что и запись документа. Цель хранится шифром, а не номером:
-- ссылка может вести на документ, которого ещё нет, и начинает работать, когда он появится. Уровень блока-ссылки здесь же:
-- читатель без допуска к блоку не должен узнать, что документ на что-то ссылается. Статус и уровень самого документа-источника
-- берутся из documents при запросе.
CREATE TABLE document_links (
    id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    source_id   bigint   NOT NULL REFERENCES documents (id) ON DELETE CASCADE,
    block_id    text     NOT NULL,
    level       smallint NOT NULL CHECK (level BETWEEN 0 AND 7),
    target_code text     NOT NULL,
    UNIQUE (source_id, block_id, target_code)
);
CREATE INDEX document_links_target_idx ON document_links (target_code);

-- +goose Down
DROP TABLE document_links;
