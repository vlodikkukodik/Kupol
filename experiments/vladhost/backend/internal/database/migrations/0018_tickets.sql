-- +goose Up
-- Обращения в поддержку: тикет пользователя и переписка по нему. Отвечает администратор панели.
CREATE TABLE tickets (
    id         BIGSERIAL PRIMARY KEY,
    user_id    BIGINT      NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    subject    TEXT        NOT NULL,
    category   TEXT        NOT NULL,                 -- general, sites, domains, mail, dns, databases, other
    status     TEXT        NOT NULL DEFAULT 'open',  -- open (ждёт ответа поддержки), answered (ответила поддержка), closed
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),   -- время последнего сообщения или смены статуса
    closed_at  TIMESTAMPTZ
);
CREATE INDEX tickets_user_idx ON tickets (user_id, updated_at DESC);
CREATE INDEX tickets_status_idx ON tickets (status, updated_at DESC);

CREATE TABLE ticket_messages (
    id         BIGSERIAL PRIMARY KEY,
    ticket_id  BIGINT      NOT NULL REFERENCES tickets (id) ON DELETE CASCADE,
    author_id  BIGINT      NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    staff      BOOLEAN     NOT NULL DEFAULT false,   -- сообщение поддержки
    body       TEXT        NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX ticket_messages_ticket_idx ON ticket_messages (ticket_id, id);

-- +goose Down
DROP TABLE ticket_messages;
DROP TABLE tickets;
