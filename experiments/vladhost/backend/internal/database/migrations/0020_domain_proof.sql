-- +goose Up
-- Домены в разделах «DNS» и «Почта» добавляются прямо там, без привязки к сайту, поэтому их владение подтверждается: TXT-запись
-- «_vladhost-verify.домен» с кодом (пакет domainproof) либо тем, что домен уже подключён к сайту этого пользователя. Неподтверждённый домен
-- на сервер не попадает. Занять домен может только подтвердивший, поэтому уникальность — среди подтверждённых; неподтверждённых заявок на один домен
-- может быть несколько (у каждого пользователя не больше одной).
ALTER TABLE dns_zones
    ADD COLUMN verified BOOLEAN NOT NULL DEFAULT true, -- прежние зоны создавались только для подключённых к сайту доменов
    ADD COLUMN token    TEXT    NOT NULL DEFAULT '';
DROP INDEX dns_zones_domain_key;
CREATE UNIQUE INDEX dns_zones_verified_domain_key ON dns_zones (domain) WHERE verified;
CREATE UNIQUE INDEX dns_zones_user_domain_key ON dns_zones (user_id, domain);

ALTER TABLE mail_domains
    ADD COLUMN verified BOOLEAN NOT NULL DEFAULT true, -- прежние домены почты создавались только для подключённых к сайту доменов
    ADD COLUMN token    TEXT    NOT NULL DEFAULT '';
DROP INDEX mail_domains_domain_key;
CREATE UNIQUE INDEX mail_domains_verified_domain_key ON mail_domains (domain) WHERE verified;
CREATE UNIQUE INDEX mail_domains_user_domain_key ON mail_domains (user_id, domain);

-- +goose Down
DROP INDEX mail_domains_user_domain_key;
DROP INDEX mail_domains_verified_domain_key;
CREATE UNIQUE INDEX mail_domains_domain_key ON mail_domains (domain);
ALTER TABLE mail_domains DROP COLUMN token, DROP COLUMN verified;

DROP INDEX dns_zones_user_domain_key;
DROP INDEX dns_zones_verified_domain_key;
CREATE UNIQUE INDEX dns_zones_domain_key ON dns_zones (domain);
ALTER TABLE dns_zones DROP COLUMN token, DROP COLUMN verified;
