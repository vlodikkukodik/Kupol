package htaccess

import (
	"crypto/md5"  //nolint:gosec // формат apr1 (Apache MD5) обязан использовать MD5: это совместимость с htpasswd, а не выбор алгоритма
	"crypto/sha1" //nolint:gosec // формат {SHA} из htpasswd
	"crypto/subtle"
	"encoding/base64"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

// ParseHtpasswd читает файл .htpasswd: строки "пользователь:хеш".
func ParseHtpasswd(data []byte) map[string]string {
	out := map[string]string{}
	for _, line := range strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		user, hash, ok := strings.Cut(line, ":")
		if ok && user != "" {
			out[user] = hash
		}
	}
	return out
}

// VerifyPassword сверяет пароль с записью htpasswd: bcrypt ($2y$/$2a$/$2b$), Apache MD5 ($apr1$) и {SHA}.
// Старый crypt() и открытый текст не поддержаны: они небезопасны.
func VerifyPassword(hash, password string) bool {
	switch {
	case strings.HasPrefix(hash, "$2y$") || strings.HasPrefix(hash, "$2a$") || strings.HasPrefix(hash, "$2b$"):
		h := hash
		if strings.HasPrefix(h, "$2y$") {
			h = "$2a$" + h[4:] // $2y$ у PHP и Apache — тот же алгоритм, что $2a$
		}
		return bcrypt.CompareHashAndPassword([]byte(h), []byte(password)) == nil
	case strings.HasPrefix(hash, "$apr1$"):
		parts := strings.SplitN(hash, "$", 4) // "", "apr1", salt, hash
		if len(parts) != 4 {
			return false
		}
		salt := parts[2]
		if len(salt) > 8 {
			salt = salt[:8]
		}
		return subtle.ConstantTimeCompare([]byte(apr1(password, salt)), []byte(hash)) == 1
	case strings.HasPrefix(hash, "{SHA}"):
		sum := sha1.Sum([]byte(password)) //nolint:gosec
		want := "{SHA}" + base64.StdEncoding.EncodeToString(sum[:])
		return subtle.ConstantTimeCompare([]byte(want), []byte(hash)) == 1
	}
	return false
}

const apr1Alphabet = "./0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

func to64(v uint32, n int) string {
	var b []byte
	for ; n > 0; n-- {
		b = append(b, apr1Alphabet[v&0x3f])
		v >>= 6
	}
	return string(b)
}

// apr1 вычисляет "$apr1$соль$хеш" по алгоритму Apache (см. apr_md5_encode).
func apr1(pw, salt string) string {
	alt := md5.Sum([]byte(pw + salt + pw)) //nolint:gosec
	ctx := md5.New()                       //nolint:gosec
	ctx.Write([]byte(pw + "$apr1$" + salt))
	for n := len(pw); n > 0; n -= 16 {
		ctx.Write(alt[:min(n, 16)])
	}
	for n := len(pw); n != 0; n >>= 1 {
		if n&1 == 1 {
			ctx.Write([]byte{0})
		} else {
			ctx.Write([]byte(pw[:1]))
		}
	}
	final := ctx.Sum(nil)
	for i := range 1000 {
		c := md5.New() //nolint:gosec
		if i&1 == 1 {
			c.Write([]byte(pw))
		} else {
			c.Write(final)
		}
		if i%3 != 0 {
			c.Write([]byte(salt))
		}
		if i%7 != 0 {
			c.Write([]byte(pw))
		}
		if i&1 == 1 {
			c.Write(final)
		} else {
			c.Write([]byte(pw))
		}
		final = c.Sum(nil)
	}
	g := func(a, b, c int) uint32 { return uint32(final[a])<<16 | uint32(final[b])<<8 | uint32(final[c]) }
	out := "$apr1$" + salt + "$"
	out += to64(g(0, 6, 12), 4) + to64(g(1, 7, 13), 4) + to64(g(2, 8, 14), 4) + to64(g(3, 9, 15), 4) + to64(g(4, 10, 5), 4)
	out += to64(uint32(final[11]), 2)
	return out
}
