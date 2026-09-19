-- +goose Up

-- Роли команды (этап 3, спецификация §12). У человека может быть несколько ролей.
-- Директорат — не роль, а флаг users.directorate: он подразумевает все роли и выдаётся только автором.
CREATE TABLE user_roles (
    user_id    bigint      NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    role       text        NOT NULL CHECK (role IN ('author', 'editor', 'moderator', 'archivist')),
    -- кто выдал; пусто, если роль выдана командой на сервере или выдавший удалил аккаунт
    granted_by bigint      REFERENCES users (id) ON DELETE SET NULL,
    granted_at timestamptz NOT NULL,
    PRIMARY KEY (user_id, role)
);
CREATE INDEX user_roles_role_idx ON user_roles (role);

-- +goose Down
DROP TABLE user_roles;
