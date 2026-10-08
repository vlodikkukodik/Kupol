// Package shellaccess — доступ к оболочке сайта: SSH-ключи аккаунта, включение доступа для сайта и проверка входа. Сами сеансы
// обслуживают SSH-сервер (sshd) и веб-терминал через посредника оболочек.
package shellaccess

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"encoding/pem"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgconn"
	"golang.org/x/crypto/ssh"
	"gorm.io/gorm"

	"vladhost/internal/apperr"
)

var (
	ErrKeyInvalid  = apperr.Validation("public_key", "ssh_key_invalid", "invalid public key")
	ErrKeyName     = apperr.Validation("name", "ssh_key_name", "invalid key name")
	ErrKeyTaken    = apperr.New(http.StatusConflict, "ssh_key_taken", "key already added").OnField("public_key")
	ErrKeyLimit    = apperr.New(http.StatusForbidden, "ssh_key_limit", "key limit reached")
	ErrKeyNotFound = apperr.New(http.StatusNotFound, "ssh_key_not_found", "key not found")
)

// MaxKeys — сколько ключей у аккаунта.
const MaxKeys = 10

// Key — открытый ключ аккаунта.
type Key struct {
	ID          int64      `gorm:"primaryKey" json:"id"`
	UserID      int64      `json:"-"`
	Name        string     `json:"name"`
	Algorithm   string     `json:"algorithm"`
	PublicKey   string     `json:"public_key"`
	Fingerprint string     `json:"fingerprint"`
	CreatedAt   time.Time  `json:"created_at"`
	LastUsedAt  *time.Time `json:"last_used_at"`
}

func (Key) TableName() string { return "ssh_keys" }

// ParseKey разбирает одну строку authorized_keys: без параметров (command=, from= …), без второй строки; допустимы ed25519, ECDSA и RSA от
// 2048 бит. Возвращает ключ, его каноническую запись и отпечаток.
func ParseKey(line string) (ssh.PublicKey, string, string, error) {
	line = strings.TrimSpace(line)
	if line == "" || len(line) > 8192 || strings.ContainsAny(line, "\r\n\x00") {
		return nil, "", "", ErrKeyInvalid
	}
	pub, _, options, rest, err := ssh.ParseAuthorizedKey([]byte(line))
	if err != nil || len(options) > 0 || len(strings.TrimSpace(string(rest))) > 0 {
		return nil, "", "", ErrKeyInvalid
	}
	switch pub.Type() {
	case ssh.KeyAlgoED25519, ssh.KeyAlgoECDSA256, ssh.KeyAlgoECDSA384, ssh.KeyAlgoECDSA521:
	case ssh.KeyAlgoRSA:
		cp, ok := pub.(ssh.CryptoPublicKey)
		if !ok {
			return nil, "", "", ErrKeyInvalid
		}
		rk, ok := cp.CryptoPublicKey().(*rsa.PublicKey)
		if !ok || rk.N.BitLen() < 2048 {
			return nil, "", "", ErrKeyInvalid
		}
	default:
		return nil, "", "", ErrKeyInvalid
	}
	return pub, strings.TrimSpace(string(ssh.MarshalAuthorizedKey(pub))), ssh.FingerprintSHA256(pub), nil
}

func validName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if n := utf8.RuneCountInString(name); n < 1 || n > 60 || strings.ContainsAny(name, "\n\r\x00") {
		return "", ErrKeyName
	}
	return name, nil
}

// ListKeys возвращает ключи аккаунта.
func (s *Service) ListKeys(ctx context.Context, userID int64) ([]Key, error) {
	var out []Key
	err := s.db.WithContext(ctx).Where("user_id = ?", userID).Order("id").Find(&out).Error
	return out, err
}

// AddKey добавляет ключ. Лимит проверяется под блокировкой строки пользователя: параллельные запросы его не обойдут.
func (s *Service) AddKey(ctx context.Context, userID int64, name, line string) (*Key, error) {
	name, err := validName(name)
	if err != nil {
		return nil, err
	}
	pub, canonical, fp, err := ParseKey(line)
	if err != nil {
		return nil, err
	}
	k := &Key{UserID: userID, Name: name, Algorithm: pub.Type(), PublicKey: canonical, Fingerprint: fp, CreatedAt: s.now()}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Raw("SELECT id FROM users WHERE id = ? FOR UPDATE", userID).Scan(new(int64)).Error; err != nil {
			return err
		}
		var n int64
		if err := tx.Model(&Key{}).Where("user_id = ?", userID).Count(&n).Error; err != nil {
			return err
		}
		if n >= MaxKeys {
			return ErrKeyLimit.With(MaxKeys)
		}
		if err := tx.Create(k).Error; err != nil {
			var pg *pgconn.PgError
			if errors.As(err, &pg) && pg.Code == "23505" {
				return ErrKeyTaken
			}
			return err
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return k, nil
}

// Generated — пара ключей, созданная панелью. Закрытая часть существует только в этом ответе: в панели она не хранится.
type Generated struct {
	Key        *Key
	PrivateKey string // OpenSSH PEM, как у ssh-keygen -t ed25519
}

// GenerateKey создаёт пару ed25519, сохраняет открытый ключ и отдаёт закрытый один раз.
func (s *Service) GenerateKey(ctx context.Context, userID int64, name string) (*Generated, error) {
	name, err := validName(name)
	if err != nil {
		return nil, err
	}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		return nil, err
	}
	block, err := ssh.MarshalPrivateKey(priv, "vladhost-"+name)
	if err != nil {
		return nil, err
	}
	k, err := s.AddKey(ctx, userID, name, string(ssh.MarshalAuthorizedKey(sshPub)))
	if err != nil {
		return nil, err
	}
	return &Generated{Key: k, PrivateKey: string(pem.EncodeToMemory(block))}, nil
}

// DeleteKey удаляет ключ аккаунта.
func (s *Service) DeleteKey(ctx context.Context, userID, id int64) error {
	res := s.db.WithContext(ctx).Where("id = ? AND user_id = ?", id, userID).Delete(&Key{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrKeyNotFound
	}
	return nil
}
