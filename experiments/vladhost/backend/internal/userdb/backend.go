package userdb

import (
	"context"
	"errors"
	"time"
)

// Backend — операции над одним сервером СУБД от имени служебной учётной записи панели. Реализации:
// postgres.go и mariadb.go. Имена баз и пользователей сюда приходят уже проверенными (см. ValidName/FullName).
type Backend interface {
	// Create создаёт базу и одноимённую учётную запись с паролем; учётная запись видит только эту базу.
	Create(ctx context.Context, name, password string) error
	// Drop удаляет базу и все её учётные записи (включая временные и открытые для внешних адресов). Нет базы — не ошибка.
	Drop(ctx context.Context, name string, id int64) error
	// SetPassword меняет пароль учётной записи базы (у всех её адресов).
	SetPassword(ctx context.Context, name, password string) error
	// Size возвращает занятое базой место в байтах.
	Size(ctx context.Context, name string) (int64, error)
	// Freeze переводит базу в режим «только чтение и удаление данных»: писать новое и создавать таблицы нельзя, а освободить место можно.
	Freeze(ctx context.Context, name string, id int64) error
	// Unfreeze возвращает обычные права.
	Unfreeze(ctx context.Context, name string, id int64) error
	// Compact возвращает серверу место, освобождённое удалением данных (VACUUM FULL / OPTIMIZE TABLE): без этого размер не уменьшится.
	Compact(ctx context.Context, name string) error
	// SetAccess задаёт список адресов, с которых база доступна снаружи (по TLS). Пустой список закрывает внешний доступ.
	SetAccess(ctx context.Context, name string, id int64, addrs []string, frozen bool) error
	// TempAccount создаёт временную учётную запись для веб-клиента, действующую до expires.
	TempAccount(ctx context.Context, name string, expires time.Time, frozen bool) (account, password string, err error)
	// DropTemp удаляет временную учётную запись (и закрывает её сессии). Нет записи — не ошибка.
	DropTemp(ctx context.Context, name, account string) error
	// Ping проверяет связь с сервером.
	Ping(ctx context.Context) error
}

// ErrNoExternalAccess — сервер СУБД не настроен для внешнего доступа.
var ErrNoExternalAccess = errors.New("userdb: external access is not configured")
