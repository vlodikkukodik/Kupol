-- +goose Up
-- Собственный DNS (ns.vladinc.ru, ns2.vladinc.ru): зоны своих доменов пользователей и их записи. Панель хранит желаемое состояние;
-- на сервер оно попадает файлом dns/state.json, а зоны для BIND собирает deploy/bin/dns-sync.py.
CREATE TABLE dns_zones (
    id         BIGSERIAL PRIMARY KEY,
    user_id    BIGINT      NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    domain     TEXT        NOT NULL,
    serial     BIGINT      NOT NULL, -- серийный номер SOA: растёт при каждом изменении зоны
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX dns_zones_domain_key ON dns_zones (domain);
CREATE INDEX dns_zones_user_idx ON dns_zones (user_id);

CREATE TABLE dns_records (
    id         BIGSERIAL PRIMARY KEY,
    zone_id    BIGINT      NOT NULL REFERENCES dns_zones (id) ON DELETE CASCADE,
    name       TEXT        NOT NULL, -- «@» или имя внутри зоны (www, _dmarc, *.dev)
    type       TEXT        NOT NULL, -- A, AAAA, CNAME, MX, TXT, SRV, CAA
    value      TEXT        NOT NULL,
    priority   INT         NOT NULL DEFAULT 0, -- MX и SRV
    ttl        INT         NOT NULL DEFAULT 300,
    managed    TEXT        NOT NULL DEFAULT '', -- «mail» — создана кнопкой автоматической настройки почты
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX dns_records_zone_idx ON dns_records (zone_id);

-- +goose Down
DROP TABLE dns_records;
DROP TABLE dns_zones;
