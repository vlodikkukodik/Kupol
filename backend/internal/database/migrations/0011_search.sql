-- +goose Up

-- Поиск по документам (этап 4). Текст каждого блока попадает в индекс вместе с уровнем допуска: читатель ищет только по
-- строкам уровня не выше своего, и сниппеты берутся только из них. Строки строятся кодом (documents/search_index.go) в той же
-- транзакции, что и сохранение документа. Статус и уровень самого документа здесь не хранятся — берутся из documents при
-- запросе, поэтому публикация, архив и смена допуска документа индекс не трогают.
--
-- Один блок может дать несколько строк: по одной на каждый уровень его фрагментов (открытый текст абзаца и закрытый
-- фрагмент внутри него не смешиваются — иначе слово из закрытого фрагмента находило бы абзац для читателя без допуска).
CREATE TABLE document_search (
    id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    document_id bigint   NOT NULL REFERENCES documents (id) ON DELETE CASCADE,
    -- title — название и шифр; meta — сведения досье (место обнаружения); block — текст блока
    kind        text     NOT NULL CHECK (kind IN ('title', 'meta', 'block')),
    -- положение блока в документе (для title и meta — 0)
    ord         integer  NOT NULL,
    block_id    text     NOT NULL DEFAULT '',
    -- эффективный уровень строки: наибольший из уровней блока и фрагмента
    level       smallint NOT NULL CHECK (level BETWEEN 0 AND 7),
    body        text     NOT NULL,
    -- тот же текст в нижнем регистре (строчные буквы считает сервер, а не база): база с локалью C не понижает кириллицу,
    -- и «Сотрудники» не нашлись бы по «сотрудник»
    norm        text     NOT NULL,
    -- совпадение в названии весомее, чем в сведениях досье, а те — весомее, чем в тексте
    tsv         tsvector GENERATED ALWAYS AS (
        CASE kind
            WHEN 'title' THEN setweight(to_tsvector('russian', norm), 'A')
            WHEN 'meta'  THEN setweight(to_tsvector('russian', norm), 'B')
            ELSE              setweight(to_tsvector('russian', norm), 'D')
        END) STORED
);
CREATE INDEX document_search_tsv_idx ON document_search USING gin (tsv);
CREATE INDEX document_search_document_idx ON document_search (document_id);

-- Версия правил построения индекса: при её смене сервер сам перестраивает индекс при старте
-- (так же строятся строки для документов, появившихся до поиска).
CREATE TABLE document_search_state (
    singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    version   integer NOT NULL
);

-- +goose Down
DROP TABLE document_search_state;
DROP TABLE document_search;
