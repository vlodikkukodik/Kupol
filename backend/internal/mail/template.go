package mail

import (
	"bytes"
	"html/template"
	"strings"

	"kupol/internal/i18n"
)

// layout — фирменный вид письма (см. UiSeal/tokens.css фронтенда): тёмная «шапка» с печатью, светлый лист,
// красная кнопка-штамп. Только инлайн-стили и таблицы — многие почтовые клиенты режут <style> и внешние
// шрифты/картинки, поэтому вид держится на цвете и засечках Georgia, а не на брендовых Oswald/PT Sans.
var layout = template.Must(template.New("mail").Parse(`<!doctype html>
<html lang="{{.LangTag}}">
<head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>{{.Title}}</title></head>
<body style="margin:0;padding:0;background:#0e1412;">
<table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="background:#0e1412;padding:32px 16px;">
<tr><td align="center">
<table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="max-width:520px;background:#fdfbf5;border-radius:4px;overflow:hidden;font-family:Georgia,'Times New Roman',serif;">
<tr><td style="background:#101213;padding:28px 32px;text-align:center;">
<span style="display:inline-block;width:44px;height:44px;line-height:40px;border:2px solid #fdfbf5;border-radius:50%;color:#fdfbf5;font-size:20px;font-weight:bold;">{{.Seal}}</span>
<div style="color:#fdfbf5;letter-spacing:0.14em;font-size:20px;margin-top:10px;text-transform:uppercase;">{{.Brand}}</div>
</td></tr>
<tr><td style="padding:32px;color:#191c1e;">
<h1 style="font-size:21px;margin:0 0 18px;">{{.Title}}</h1>
{{range .Paragraphs}}<p style="margin:0 0 16px;line-height:1.55;font-size:15px;">{{.}}</p>
{{end}}{{if .ButtonURL}}<table role="presentation" cellpadding="0" cellspacing="0" style="margin:8px 0 20px;"><tr><td style="background:#8f1c17;border-radius:3px;">
<a href="{{.ButtonURL}}" style="display:inline-block;padding:13px 26px;color:#fdfbf5;text-decoration:none;letter-spacing:0.06em;text-transform:uppercase;font-size:14px;">{{.ButtonText}}</a>
</td></tr></table>
<p style="margin:0;font-size:12px;color:#6b655a;word-break:break-all;">{{.ButtonURL}}</p>
{{end}}</td></tr>
<tr><td style="padding:16px 32px 24px;border-top:1px solid #e5e1d6;color:#7a746a;font-size:11px;line-height:1.5;">{{.Footer}}</td></tr>
</table>
</td></tr>
</table>
</body>
</html>
`))

type layoutData struct {
	LangTag    string
	Brand      string
	Seal       string
	Title      string
	Paragraphs []string
	ButtonText string
	ButtonURL  string
	Footer     string
}

func render(l i18n.Lang, title string, paragraphs []string, buttonText, buttonURL string) (html, text string) {
	d := layoutData{
		LangTag:    string(l),
		Brand:      "КУПОЛ",
		Seal:       "К",
		Title:      title,
		Paragraphs: paragraphs,
		ButtonText: buttonText,
		ButtonURL:  buttonURL,
		Footer:     l.Translate("КУПОЛ — вымышленный архив. Автоматическое письмо, отвечать на него не нужно."),
	}
	var buf bytes.Buffer
	if err := layout.Execute(&buf, d); err != nil {
		panic("mail: шаблон: " + err.Error()) // шаблон статический — ошибка означает баг в коде, не во входных данных
	}

	var tb strings.Builder
	tb.WriteString(title)
	tb.WriteString("\n\n")
	for _, p := range paragraphs {
		tb.WriteString(p)
		tb.WriteString("\n\n")
	}
	if buttonURL != "" {
		tb.WriteString(buttonText)
		tb.WriteString(": ")
		tb.WriteString(buttonURL)
		tb.WriteString("\n\n")
	}
	tb.WriteString(d.Footer)
	return buf.String(), tb.String()
}

// EmailConfirmation — письмо со ссылкой подтверждения почты (шаг 5.1.1: email вместо «без почты» в спецификации).
// confirmURL — ссылка на /email-confirm?token=… (see httpapi), действует ограниченное время (см. accounts.EmailConfirmTTL).
func EmailConfirmation(l i18n.Lang, login, confirmURL string) Message {
	title := l.Translate("Подтверждение почты")
	p1 := l.T("Вы указали этот адрес как почту аккаунта «%s» в архиве КУПОЛ. Чтобы подтвердить его, перейдите по ссылке ниже.", login)
	p2 := l.Translate("Ссылка действует 24 часа. Если это были не вы — просто не открывайте её: адрес подтверждён не будет, а аккаунт не пострадает.")
	button := l.Translate("Подтвердить почту")
	subject := l.Translate("Подтвердите почту — КУПОЛ")
	html, text := render(l, title, []string{p1, p2}, button, confirmURL)
	return Message{Subject: subject, HTML: html, Text: text}
}

// LevelUp — письмо о повышении уровня по XP (см. internal/xp).
func LevelUp(l i18n.Lang, login, levelName string, level int) Message {
	title := l.Translate("ДОПУСК ПОВЫШЕН")
	p1 := l.T("%s, ваш допуск в архиве КУПОЛ повышен до уровня %d — «%s». Продолжайте нести службу.", login, level, levelName)
	subject := l.Translate("Допуск повышен — КУПОЛ")
	html, text := render(l, title, []string{p1}, "", "")
	return Message{Subject: subject, HTML: html, Text: text}
}
