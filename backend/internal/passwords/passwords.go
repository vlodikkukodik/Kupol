// Package passwords хеширует пароли и резервные коды алгоритмом argon2id.
//
// Хеш хранится в PHC-формате вместе с параметрами и солью:
//
//	$argon2id$v=19$m=19456,t=2,p=1$<соль base64>$<хеш base64>
//
// Поэтому параметры можно усиливать позже: старые хеши проверяются со своими параметрами,
// а Verify сообщает, что хеш стоит пересчитать (needsRehash).
package passwords

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/crypto/argon2"
)

const (
	saltLen = 16
	keyLen  = 32

	// maxMemoryKiB — потолок памяти при разборе чужого хеша (256 МиБ): защита от «бомбы» в данных.
	maxMemoryKiB = 256 * 1024
	maxIter      = 16
	maxParallel  = 16
)

// Params — параметры argon2id.
type Params struct {
	MemoryKiB   uint32
	Iterations  uint32
	Parallelism uint8
}

// DefaultParams — минимум из рекомендаций OWASP для argon2id (19 МиБ, 2 прохода, 1 поток).
// Память осознанно небольшая: боевой VPS общий и с ограниченной ОЗУ, а вход ограничен по частоте.
var DefaultParams = Params{MemoryKiB: 19 * 1024, Iterations: 2, Parallelism: 1}

var (
	// ErrInvalidHash — строка не является корректным argon2id-хешем.
	ErrInvalidHash = errors.New("passwords: некорректный формат хеша")
)

// Hasher считает и проверяет хеши, ограничивая число одновременных вычислений:
// каждое занимает десятки МиБ памяти, и без ограничения поток входов мог бы её исчерпать.
type Hasher struct {
	params Params
	sem    chan struct{}
	dummy  string
}

// NewHasher создаёт хешер. maxConcurrent — сколько хешей можно считать одновременно.
func NewHasher(p Params, maxConcurrent int) (*Hasher, error) {
	if maxConcurrent < 1 {
		return nil, errors.New("passwords: maxConcurrent должен быть >= 1")
	}
	if p.MemoryKiB < 8*uint32(max(p.Parallelism, 1)) || p.Iterations < 1 || p.Parallelism < 1 {
		return nil, fmt.Errorf("passwords: недопустимые параметры %+v", p)
	}
	h := &Hasher{params: p, sem: make(chan struct{}, maxConcurrent)}

	// Настоящий хеш случайного пароля с теми же параметрами: на него «тратится» время,
	// когда проверяемого пользователя не существует (см. VerifyDummy).
	var rnd [24]byte
	if _, err := rand.Read(rnd[:]); err != nil {
		return nil, err
	}
	d, err := h.Hash(context.Background(), base64.RawStdEncoding.EncodeToString(rnd[:]))
	if err != nil {
		return nil, err
	}
	h.dummy = d
	return h, nil
}

func (h *Hasher) acquire(ctx context.Context) error {
	select {
	case h.sem <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (h *Hasher) release() { <-h.sem }

// Hash возвращает PHC-строку с новой случайной солью.
func (h *Hasher) Hash(ctx context.Context, password string) (string, error) {
	if err := h.acquire(ctx); err != nil {
		return "", err
	}
	defer h.release()

	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(password), salt, h.params.Iterations, h.params.MemoryKiB, h.params.Parallelism, keyLen)
	return encode(h.params, salt, key), nil
}

// Verify проверяет пароль. needsRehash — параметры хеша слабее текущих, его стоит пересчитать
// (вызывающий делает это после успешной проверки, пока пароль у него в руках).
func (h *Hasher) Verify(ctx context.Context, password, encoded string) (ok, needsRehash bool, err error) {
	p, salt, want, err := decode(encoded)
	if err != nil {
		return false, false, err
	}
	if err := h.acquire(ctx); err != nil {
		return false, false, err
	}
	defer h.release()

	got := argon2.IDKey([]byte(password), salt, p.Iterations, p.MemoryKiB, p.Parallelism, uint32(len(want)))
	if subtle.ConstantTimeCompare(got, want) != 1 {
		return false, false, nil
	}
	return true, p != h.params || len(want) != keyLen, nil
}

// VerifyDummy тратит столько же времени, сколько настоящая проверка. Вызывается, когда пользователя
// нет: иначе по времени ответа можно отличить «нет такого логина» от «неверный пароль».
func (h *Hasher) VerifyDummy(ctx context.Context, password string) error {
	_, _, err := h.Verify(ctx, password, h.dummy)
	return err
}

func encode(p Params, salt, key []byte) string {
	b64 := base64.RawStdEncoding
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, p.MemoryKiB, p.Iterations, p.Parallelism, b64.EncodeToString(salt), b64.EncodeToString(key))
}

func decode(encoded string) (p Params, salt, key []byte, err error) {
	parts := strings.Split(encoded, "$")
	// ["", "argon2id", "v=19", "m=..,t=..,p=..", "<salt>", "<key>"]
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" {
		return p, nil, nil, ErrInvalidHash
	}
	if parts[2] != "v="+strconv.Itoa(argon2.Version) {
		return p, nil, nil, ErrInvalidHash
	}

	fields := strings.Split(parts[3], ",")
	if len(fields) != 3 {
		return p, nil, nil, ErrInvalidHash
	}
	var m, t, par uint64
	for i, f := range fields {
		name, val, found := strings.Cut(f, "=")
		want := [...]string{"m", "t", "p"}[i]
		if !found || name != want {
			return p, nil, nil, ErrInvalidHash
		}
		n, perr := strconv.ParseUint(val, 10, 32)
		if perr != nil {
			return p, nil, nil, ErrInvalidHash
		}
		switch want {
		case "m":
			m = n
		case "t":
			t = n
		case "p":
			par = n
		}
	}
	if m < 8 || m > maxMemoryKiB || t < 1 || t > maxIter || par < 1 || par > maxParallel || m < 8*par {
		return p, nil, nil, ErrInvalidHash
	}
	p = Params{MemoryKiB: uint32(m), Iterations: uint32(t), Parallelism: uint8(par)}

	b64 := base64.RawStdEncoding
	if salt, err = b64.DecodeString(parts[4]); err != nil || len(salt) < 8 {
		return p, nil, nil, ErrInvalidHash
	}
	if key, err = b64.DecodeString(parts[5]); err != nil || len(key) < 16 {
		return p, nil, nil, ErrInvalidHash
	}
	return p, salt, key, nil
}
