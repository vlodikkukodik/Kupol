-- +goose Up
CREATE TABLE sites (
    id          BIGSERIAL PRIMARY KEY,
    user_id     BIGINT      NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    slug        TEXT        NOT NULL,
    host        TEXT        NOT NULL,
    disk_bytes  BIGINT      NOT NULL DEFAULT 0,
    status      TEXT        NOT NULL DEFAULT 'empty' CHECK (status IN ('empty', 'live')),
    deployed_at TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX sites_host_key ON sites (host);
CREATE UNIQUE INDEX sites_user_slug_key ON sites (user_id, slug);

-- +goose Down
DROP TABLE sites;
