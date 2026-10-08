-- +goose Up
-- Планировщик задач: HTTP-запросы и команды в песочнице сайта по расписанию cron (в UTC), журнал запусков.
CREATE TABLE cron_jobs (
    id            BIGSERIAL PRIMARY KEY,
    user_id       BIGINT      NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    name          TEXT        NOT NULL,
    kind          TEXT        NOT NULL CHECK (kind IN ('http', 'command')),
    schedule      TEXT        NOT NULL,
    url           TEXT        NOT NULL DEFAULT '',
    command       TEXT        NOT NULL DEFAULT '',
    site_id       BIGINT      REFERENCES sites (id) ON DELETE CASCADE, -- для команды: в папке какого сайта она выполняется
    enabled       BOOLEAN     NOT NULL DEFAULT true,
    next_run_at   TIMESTAMPTZ,
    last_run_at   TIMESTAMPTZ,
    last_status   TEXT        NOT NULL DEFAULT '',
    fail_streak   INT         NOT NULL DEFAULT 0, -- подряд неудачных запусков
    failing_since TIMESTAMPTZ,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX cron_jobs_user_idx ON cron_jobs (user_id);
CREATE INDEX cron_jobs_due_idx ON cron_jobs (next_run_at) WHERE enabled;

CREATE TABLE cron_runs (
    id          BIGSERIAL PRIMARY KEY,
    job_id      BIGINT      NOT NULL REFERENCES cron_jobs (id) ON DELETE CASCADE,
    started_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    finished_at TIMESTAMPTZ,
    status      TEXT        NOT NULL CHECK (status IN ('running', 'ok', 'failed', 'timeout', 'skipped')),
    code        INT         NOT NULL DEFAULT 0, -- код выхода команды или HTTP-статус
    duration_ms INT         NOT NULL DEFAULT 0,
    reason      TEXT        NOT NULL DEFAULT '', -- код системной причины (интерфейс переводит его); сам вывод — в output
    output      TEXT        NOT NULL DEFAULT ''
);
CREATE INDEX cron_runs_job_idx ON cron_runs (job_id, id DESC);

-- +goose Down
DROP TABLE cron_runs;
DROP TABLE cron_jobs;
