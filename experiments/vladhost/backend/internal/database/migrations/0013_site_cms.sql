-- +goose Up
-- Приложение, установленное на сайт «в один клик» (WordPress): что и когда поставлено и какая база создана.
CREATE TABLE site_cms (
    site_id      BIGINT PRIMARY KEY REFERENCES sites (id) ON DELETE CASCADE,
    cms          TEXT        NOT NULL,
    version      TEXT        NOT NULL,
    db_name      TEXT        NOT NULL,
    installed_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE site_cms;
