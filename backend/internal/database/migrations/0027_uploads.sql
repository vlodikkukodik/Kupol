-- +goose Up

-- Загрузки (этап 6.1): картинки (перекодируются в WebP, EXIF удаляется, есть превью) и аудио mp3/ogg.
-- Файл отдаётся по случайному ключу и только читателю с допуском не ниже level; блоки документов ссылаются на ключ.
CREATE TABLE uploads (
    id         bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    key        text        NOT NULL UNIQUE,
    owner_id   bigint      REFERENCES users (id) ON DELETE SET NULL,
    kind       text        NOT NULL CHECK (kind IN ('image', 'audio')),
    name       text        NOT NULL,
    mime       text        NOT NULL,
    size       bigint      NOT NULL CHECK (size > 0),
    width      integer,
    height     integer,
    level      smallint    NOT NULL DEFAULT 0 CHECK (level BETWEEN 0 AND 7),
    created_at timestamptz NOT NULL
);
CREATE INDEX uploads_owner_idx ON uploads (owner_id, created_at DESC);

-- +goose Down
DROP TABLE uploads;
