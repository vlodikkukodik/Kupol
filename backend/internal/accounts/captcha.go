package accounts

import (
	"crypto/rand"
	"math/big"
	"strings"

	"kupol/internal/i18n"
)

// question — вопрос-«анкета» при регистрации (спецификация §2: «простой вопрос по вселенной»).
//
// Ответ на каждый вопрос читается на главной странице сайта: печать (год, название), штамп
// (гриф), текст (архив). Если меняется текст главной — проверьте, что вопросы остаются отвечаемыми.
// Вопрос задаётся на языке читателя (Text), а принимается ответ на любом из языков (Answers): читатель мог сменить язык
// интерфейса, пока вопрос был на экране, а в итальянской версии сайта то же слово написано по-итальянски.
type question struct {
	ID      string
	Text    map[i18n.Lang]string
	Answers []string // допустимые ответы в нормализованном виде (см. normalizeAnswer)
}

// text — вопрос на языке l (нет перевода — по-русски).
func (q question) text(l i18n.Lang) string {
	if t, ok := q.Text[l]; ok {
		return t
	}
	return q.Text[i18n.RU]
}

var questions = []question{
	{"founded", map[i18n.Lang]string{
		i18n.RU: "В каком году основан КУПОЛ? Год написан на печати.",
		i18n.IT: "In che anno è stato fondato KUPOL? L'anno è scritto sul sigillo.",
	}, []string{"1974"}},
	{"grif", map[i18n.Lang]string{
		i18n.RU: "Какой гриф написан на штампе в правом верхнем углу главной страницы? Впишите его целиком.",
		i18n.IT: "Quale timbro è scritto in alto a destra nella pagina iniziale? Scrivilo per intero.",
	}, []string{"форма купол-1", "форма купол 1", "modulo kupol-1", "modulo kupol 1"}},
	{"archive", map[i18n.Lang]string{
		i18n.RU: "Как сокращённо называется Центральный архив Купола? Три буквы.",
		i18n.IT: "Come si abbrevia l'Archivio centrale di Kupol? Tre lettere.",
	}, []string{"цак", "ack"}},
	{"fill", map[i18n.Lang]string{
		i18n.RU: "Комитет Управления Паранормальными … и Локациями. Впишите пропущенное слово.",
		i18n.IT: "Comitato per la Gestione degli Oggetti e dei Luoghi …. Scrivi la parola mancante.",
	}, []string{"объектами", "paranormali"}},
	{"first-word", map[i18n.Lang]string{
		i18n.RU: "С какого слова начинается полное название комитета?",
		i18n.IT: "Con quale parola inizia il nome completo del comitato?",
	}, []string{"комитет", "comitato"}},
	{"letters", map[i18n.Lang]string{
		i18n.RU: "Сколько букв в названии «КУПОЛ»? Ответьте цифрой.",
		i18n.IT: "Quante lettere ha il nome «KUPOL»? Rispondi con una cifra.",
	}, []string{"5", "пять", "cinque"}},
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
