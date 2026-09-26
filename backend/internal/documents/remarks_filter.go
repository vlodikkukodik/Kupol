package documents

import "strings"

// bannedWords — базовый автофильтр «пометок на полях» (спецификация §8). Список намеренно короткий и без грубой
// лексики: подбор полного словаря — задача автора после запуска, здесь важен сам механизм (и то, что он ловит слово
// по вхождению основы, без учёта регистра — «Идиотский» тоже поймает «идиот»).
var bannedWords = []string{"идиот", "дебил", "кретин", "тупица"}

// firstBannedWord ищет первое запрещённое слово без учёта регистра; "", false — текст чист.
func firstBannedWord(text string) (string, bool) {
	lower := strings.ToLower(text)
	for _, w := range bannedWords {
		if strings.Contains(lower, w) {
			return w, true
		}
	}
	return "", false
}
