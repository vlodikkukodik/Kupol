package accounts

import (
	"crypto/rand"
	"math/big"
	"strings"
)

// question — вопрос-«анкета» при регистрации (спецификация §2: «простой вопрос по вселенной»).
//
// Ответ на каждый вопрос читается на главной странице сайта: печать (год, название), штамп
// (гриф), текст (архив). Если меняется текст главной — проверьте, что вопросы остаются отвечаемыми.
type question struct {
	ID      string
	Text    string
	Answers []string // допустимые ответы в нормализованном виде (см. normalizeAnswer)
}

var questions = []question{
	{"founded", "В каком году основан КУПОЛ? Год написан на печати.", []string{"1974"}},
	{"grif", "Какой гриф написан на штампе в правом верхнем углу главной страницы? Впишите его целиком.", []string{"форма купол-1", "форма купол 1"}},
	{"archive", "Как сокращённо называется Центральный архив Купола? Три буквы.", []string{"цак"}},
	{"fill", "Комитет Управления Паранормальными … и Локациями. Впишите пропущенное слово.", []string{"объектами"}},
	{"first-word", "С какого слова начинается полное название комитета?", []string{"комитет"}},
	{"letters", "Сколько букв в названии «КУПОЛ»? Ответьте цифрой.", []string{"5", "пять"}},
}

func questionByID(id string) (question, bool) {
	for _, q := range questions {
		if q.ID == id {
			return q, true
		}
	}
	return question{}, false
}

func randomQuestion() (question, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(int64(len(questions))))
	if err != nil {
		return question{}, err
	}
	return questions[n.Int64()], nil
}

// normalizeAnswer: нижний регистр, ё -> е, разные тире -> «-», лишние знаки и пробелы убраны.
func normalizeAnswer(s string) string {
	s = strings.ToLower(s)
	s = strings.NewReplacer("ё", "е", "–", "-", "—", "-", "−", "-", "‑", "-").Replace(s)
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '.' || r == ',' || r == '!' || r == '?' || r == '«' || r == '»' || r == '"' || r == '\'' || r == ';' || r == ':':
			continue
		default:
			b.WriteRune(r)
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

func (q question) accepts(answer string) bool {
	got := normalizeAnswer(answer)
	if got == "" {
		return false
	}
	for _, a := range q.Answers {
		if got == a {
			return true
		}
	}
	return false
}
