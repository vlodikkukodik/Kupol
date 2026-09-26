package accounts

import (
	"crypto/rand"
	"strings"
)

// Алфавит резервного кода: 32 символа (ровно 5 бит) без похожих I, O, 0, 1.
const backupAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"

const (
	backupGroups    = 4
	backupGroupSize = 4
	backupPrefix    = "KUPOL"
)

// GenerateBackupCode возвращает случайный код вида KUPOL-XXXX-XXXX-XXXX-XXXX (80 бит энтропии).
// Показывается пользователю один раз при регистрации.
func GenerateBackupCode() (string, error) {
	raw := make([]byte, backupGroups*backupGroupSize)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	var b strings.Builder
	b.WriteString(backupPrefix)
	for i, v := range raw {
		if i%backupGroupSize == 0 {
			b.WriteByte('-')
		}
		b.WriteByte(backupAlphabet[v&31]) // 256 кратно 32: смещения нет
	}
	return b.String(), nil
}

// CanonicalBackupCode приводит введённый код к каноническому виду (16 символов алфавита)
// или сообщает, что это не код. Допускаются любой регистр, пробелы, дефисы и отсутствие префикса.
func CanonicalBackupCode(input string) (string, bool) {
	var b strings.Builder
	for _, r := range strings.ToUpper(input) {
		switch {
		case r == ' ' || r == '-' || r == '_' || r == '\t':
			continue
		case r < 0x80:
			b.WriteRune(r)
		default:
			return "", false
		}
	}
	s := strings.TrimPrefix(b.String(), backupPrefix)
	if len(s) != backupGroups*backupGroupSize {
		return "", false
	}
	for i := 0; i < len(s); i++ {
		if strings.IndexByte(backupAlphabet, s[i]) < 0 {
			return "", false
		}
	}
	return s, true
}
