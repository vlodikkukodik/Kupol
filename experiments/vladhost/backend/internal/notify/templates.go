package notify

import (
	"bytes"
	"fmt"
	"html"
	"regexp"
	"strings"
	"text/template"
)

// Виды писем.
const (
	KindVerifyEmail     = "verify_email"
	KindResetPassword   = "reset_password"
	KindPasswordChanged = "password_changed"
	KindTwoFactorOn     = "two_factor_on"  // включён вход с кодом из приложения
	KindTwoFactorOff    = "two_factor_off" // выключен (пользователем или администратором)
	KindCertFailed      = "cert_failed"
	KindCertExpiring    = "cert_expiring"
	KindDiskFull        = "disk_full"
	KindDBFrozen        = "db_frozen"
	KindCronFailed      = "cron_failed"
	KindMailboxFull     = "mailbox_full"
	KindTicketNew       = "ticket_new"        // администраторам: новое обращение
	KindTicketUserReply = "ticket_user_reply" // администраторам: пользователь дописал в тикет
	KindTicketReply     = "ticket_reply"      // пользователю: поддержка ответила
)

// Data — подстановки в письма. Значения приходят из внешних источников (текст ошибки certbot, имя пользователя),
// поэтому попадают в шаблон только как данные, а в HTML экранируются.
type Data struct {
	Name    string
	Link    string
	Host    string
	Reason  string
	Days    int
	Percent int
	Used    string
	Total   string
	Author  string // автор обращения (письма администраторам)
	Ticket  int64  // номер обращения
}

// Rendered — готовое письмо.
type Rendered struct {
	Subject string
	Text    string
	HTML    string
}

type source struct{ subject, body string }

// catalog[язык][вид]. Текст написан один раз: HTML-версия строится из него автоматически (абзацы и ссылки).
var catalog = map[string]map[string]source{
	"ru": {
		KindVerifyEmail: {
			"Подтвердите адрес почты — Vladhost",
			`Здравствуйте, {{.Name}}!

Подтвердите адрес почты, перейдя по ссылке (она действует 48 часов):
{{.Link}}

Если вы не регистрировались в Vladhost, просто проигнорируйте это письмо.`,
		},
		KindResetPassword: {
			"Сброс пароля — Vladhost",
			`Здравствуйте, {{.Name}}!

Для аккаунта запросили сброс пароля. Чтобы задать новый пароль, перейдите по ссылке (она действует 1 час):
{{.Link}}

Если это были не вы, ничего делать не нужно: пароль останется прежним.`,
		},
		KindPasswordChanged: {
			"Пароль от аккаунта изменён — Vladhost",
			`Здравствуйте, {{.Name}}!

Пароль от вашего аккаунта Vladhost только что изменён, все остальные сессии закрыты.

Если это были не вы, немедленно восстановите доступ через ссылку «Забыли пароль?» на странице входа и сообщите администратору.`,
		},
		KindTwoFactorOn: {
			"Включена двухфакторная защита — Vladhost",
			`Здравствуйте, {{.Name}}!

Для входа в ваш аккаунт Vladhost теперь нужен ещё и код из приложения-аутентификатора. Сохраните коды восстановления: они понадобятся, если телефон потеряется.

Если это были не вы, смените пароль и напишите администратору.`,
		},
		KindTwoFactorOff: {
			"Двухфакторная защита выключена — Vladhost",
			`Здравствуйте, {{.Name}}!

Вход в ваш аккаунт Vladhost больше не требует кода из приложения — только пароль.

Если это были не вы, смените пароль, снова включите защиту в настройках и напишите администратору.`,
		},
		KindCertFailed: {
			"Не удалось выпустить HTTPS для {{.Host}} — Vladhost",
			`Здравствуйте, {{.Name}}!

Сертификат HTTPS для {{.Host}} не выпущен. Причина: {{.Reason}}

Проверьте настройки и повторите выпуск в разделе SSL:
{{.Link}}`,
		},
		KindCertExpiring: {
			`{{if gt .Days 0}}Сертификат {{.Host}} истекает через {{.Days}} дн.{{else}}Сертификат {{.Host}} истёк{{end}} — Vladhost`,
			`Здравствуйте, {{.Name}}!

{{if gt .Days 0}}Сертификат HTTPS для {{.Host}} истекает через {{.Days}} дн., а автоматическое продление не сработало.{{else}}Сертификат HTTPS для {{.Host}} истёк, а автоматическое продление не сработало.{{end}} Посетители могут видеть предупреждение браузера.

Попробуйте перевыпустить сертификат в разделе SSL и проверьте, что DNS домена указывает на наш сервер:
{{.Link}}`,
		},
		KindDBFrozen: {
			"База {{.Host}} переведена в режим чтения — Vladhost",
			`Здравствуйте, {{.Name}}!

База данных {{.Host}} занимает {{.Used}} при лимите {{.Total}}, поэтому запись в неё и создание таблиц отключены. Читать данные и удалять их можно.

Удалите лишнее и нажмите «Проверить» на странице баз данных: когда база станет заметно меньше лимита, запись включится снова.
{{.Link}}`,
		},
		KindCronFailed: {
			"Задача «{{.Host}}» не выполняется — Vladhost",
			`Здравствуйте, {{.Name}}!

Задача планировщика «{{.Host}}» завершилась неудачей {{.Days}} раза подряд. Последний результат:
{{.Reason}}

Проверьте журнал запусков и исправьте задачу. Пока она не сработает успешно, повторных писем не будет.
{{.Link}}`,
		},
		KindDiskFull: {
			"Место на диске почти закончилось ({{.Percent}}%) — Vladhost",
			`Здравствуйте, {{.Name}}!

Ваши сайты занимают {{.Used}} из {{.Total}} ({{.Percent}}%). Когда место закончится, загрузка новых файлов станет невозможной.

Удалите ненужные файлы или архивы в файловом менеджере:
{{.Link}}`,
		},
		KindMailboxFull: {
			"Почтовый ящик {{.Host}} почти заполнен ({{.Percent}}%) — Vladhost",
			`Здравствуйте, {{.Name}}!

Почтовый ящик {{.Host}} занят на {{.Percent}}%: {{.Used}} из {{.Total}}. Когда место закончится, новые письма перестанут приниматься, а отправителям придёт отказ.

Удалите ненужные письма или увеличьте размер ящика в разделе «Почта»:
{{.Link}}`,
		},
		KindTicketNew: {
			"Новое обращение №{{.Ticket}}: {{.Host}} — Vladhost",
			`Здравствуйте, {{.Name}}!

Пользователь {{.Author}} создал обращение №{{.Ticket}} «{{.Host}}»:

{{.Reason}}

Открыть и ответить:
{{.Link}}`,
		},
		KindTicketUserReply: {
			"Ответ в обращении №{{.Ticket}}: {{.Host}} — Vladhost",
			`Здравствуйте, {{.Name}}!

Пользователь {{.Author}} дописал в обращение №{{.Ticket}} «{{.Host}}»:

{{.Reason}}

Открыть и ответить:
{{.Link}}`,
		},
		KindTicketReply: {
			"Поддержка ответила на обращение №{{.Ticket}} — Vladhost",
			`Здравствуйте, {{.Name}}!

Поддержка ответила на ваше обращение №{{.Ticket}} «{{.Host}}»:

{{.Reason}}

Прочитать ответ и продолжить переписку:
{{.Link}}`,
		},
	},
	"it": {
		KindVerifyEmail: {
			"Conferma l'indirizzo email — Vladhost",
			`Ciao {{.Name}}!

Conferma l'indirizzo email aprendo il link (valido 48 ore):
{{.Link}}

Se non ti sei registrato su Vladhost, ignora semplicemente questo messaggio.`,
		},
		KindResetPassword: {
			"Reimpostazione della password — Vladhost",
			`Ciao {{.Name}}!

È stata richiesta la reimpostazione della password del tuo account. Per scegliere una nuova password apri il link (valido 1 ora):
{{.Link}}

Se non sei stato tu, non devi fare nulla: la password resterà quella di prima.`,
		},
		KindPasswordChanged: {
			"La password dell'account è stata cambiata — Vladhost",
			`Ciao {{.Name}}!

La password del tuo account Vladhost è appena stata cambiata e tutte le altre sessioni sono state chiuse.

Se non sei stato tu, recupera subito l'accesso con il link «Password dimenticata?» nella pagina di accesso e avvisa l'amministratore.`,
		},
		KindTwoFactorOn: {
			"Verifica in due passaggi attivata — Vladhost",
			`Ciao {{.Name}}!

Per accedere al tuo account Vladhost ora serve anche il codice dell'app di autenticazione. Conserva i codici di recupero: ti serviranno se perdi il telefono.

Se non sei stato tu, cambia la password e avvisa l'amministratore.`,
		},
		KindTwoFactorOff: {
			"Verifica in due passaggi disattivata — Vladhost",
			`Ciao {{.Name}}!

L'accesso al tuo account Vladhost non richiede più il codice dell'app: basta la password.

Se non sei stato tu, cambia la password, riattiva la protezione nelle impostazioni e avvisa l'amministratore.`,
		},
		KindCertFailed: {
			"Impossibile emettere l'HTTPS per {{.Host}} — Vladhost",
			`Ciao {{.Name}}!

Il certificato HTTPS per {{.Host}} non è stato emesso. Motivo: {{.Reason}}

Controlla le impostazioni e ripeti l'emissione nella sezione SSL:
{{.Link}}`,
		},
		KindCertExpiring: {
			`{{if gt .Days 0}}Il certificato di {{.Host}} scade tra {{.Days}} gg{{else}}Il certificato di {{.Host}} è scaduto{{end}} — Vladhost`,
			`Ciao {{.Name}}!

{{if gt .Days 0}}Il certificato HTTPS di {{.Host}} scade tra {{.Days}} gg e il rinnovo automatico non ha funzionato.{{else}}Il certificato HTTPS di {{.Host}} è scaduto e il rinnovo automatico non ha funzionato.{{end}} I visitatori potrebbero vedere un avviso del browser.

Prova a riemettere il certificato nella sezione SSL e verifica che il DNS del dominio punti al nostro server:
{{.Link}}`,
		},
		KindDBFrozen: {
			"Il database {{.Host}} è in sola lettura — Vladhost",
			`Ciao {{.Name}}!

Il database {{.Host}} occupa {{.Used}} con un limite di {{.Total}}, quindi la scrittura e la creazione di tabelle sono disattivate. Puoi leggere ed eliminare i dati.

Elimina il superfluo e premi «Verifica» nella pagina dei database: quando il database sarà nettamente sotto il limite, la scrittura verrà riattivata.
{{.Link}}`,
		},
		KindCronFailed: {
			"L’attività «{{.Host}}» non funziona — Vladhost",
			`Ciao {{.Name}}!

L’attività «{{.Host}}» dell’utilità di pianificazione è fallita {{.Days}} volte di seguito. Ultimo risultato:
{{.Reason}}

Controlla il registro delle esecuzioni e correggi l’attività. Finché non riesce, non riceverai altre email.
{{.Link}}`,
		},
		KindDiskFull: {
			"Lo spazio su disco sta per finire ({{.Percent}}%) — Vladhost",
			`Ciao {{.Name}}!

I tuoi siti occupano {{.Used}} su {{.Total}} ({{.Percent}}%). Quando lo spazio finirà, non sarà più possibile caricare nuovi file.

Elimina i file o gli archivi inutili nel gestore file:
{{.Link}}`,
		},
		KindMailboxFull: {
			"La casella {{.Host}} è quasi piena ({{.Percent}}%) — Vladhost",
			`Ciao {{.Name}}!

La casella di posta {{.Host}} è piena al {{.Percent}}%: {{.Used}} su {{.Total}}. Quando lo spazio finirà, i nuovi messaggi non verranno più accettati e i mittenti riceveranno un rifiuto.

Elimina i messaggi inutili o aumenta la dimensione della casella nella sezione «Posta»:
{{.Link}}`,
		},
		KindTicketNew: {
			"Nuova richiesta n. {{.Ticket}}: {{.Host}} — Vladhost",
			`Ciao {{.Name}}!

L'utente {{.Author}} ha aperto la richiesta n. {{.Ticket}} «{{.Host}}»:

{{.Reason}}

Apri e rispondi:
{{.Link}}`,
		},
		KindTicketUserReply: {
			"Nuovo messaggio nella richiesta n. {{.Ticket}}: {{.Host}} — Vladhost",
			`Ciao {{.Name}}!

L'utente {{.Author}} ha scritto nella richiesta n. {{.Ticket}} «{{.Host}}»:

{{.Reason}}

Apri e rispondi:
{{.Link}}`,
		},
		KindTicketReply: {
			"Il supporto ha risposto alla richiesta n. {{.Ticket}} — Vladhost",
			`Ciao {{.Name}}!

Il supporto ha risposto alla tua richiesta n. {{.Ticket}} «{{.Host}}»:

{{.Reason}}

Leggi la risposta e continua la conversazione:
{{.Link}}`,
		},
	},
}

// Kinds — все виды писем (для проверки, что у каждого есть тексты на обоих языках).
func Kinds() []string {
	return []string{KindVerifyEmail, KindResetPassword, KindPasswordChanged, KindTwoFactorOn, KindTwoFactorOff, KindCertFailed, KindCertExpiring, KindDiskFull, KindDBFrozen, KindCronFailed, KindMailboxFull, KindTicketNew, KindTicketUserReply, KindTicketReply}
}

func execute(name, src string, d Data) (string, error) {
	t, err := template.New(name).Option("missingkey=error").Parse(src)
	if err != nil {
		return "", err
	}
	var b bytes.Buffer
	if err := t.Execute(&b, d); err != nil {
		return "", err
	}
	return b.String(), nil
}

// oneLine делает текст безопасным для заголовка: без переводов строки и управляющих символов.
func oneLine(s string) string {
	return strings.Join(strings.FieldsFunc(s, func(r rune) bool { return r < 0x20 || r == 0x7f }), " ")
}

// Render собирает письмо нужного вида на языке lang (неизвестный язык — русский).
func Render(kind, lang string, d Data) (Rendered, error) {
	cat, ok := catalog[lang]
	if !ok {
		cat = catalog["ru"]
	}
	src, ok := cat[kind]
	if !ok {
		return Rendered{}, fmt.Errorf("notify: неизвестный вид письма %q", kind)
	}
	d.Reason = oneLine(d.Reason)
	if len(d.Reason) > 300 {
		d.Reason = strings.ToValidUTF8(d.Reason[:300], "") + "…"
	}
	subject, err := execute(kind+".subject", src.subject, d)
	if err != nil {
		return Rendered{}, err
	}
	text, err := execute(kind+".body", src.body, d)
	if err != nil {
		return Rendered{}, err
	}
	return Rendered{Subject: oneLine(subject), Text: text, HTML: htmlFromText(text)}, nil
}

var urlRe = regexp.MustCompile(`https?://[^\s<>"']+`)

// htmlFromText строит HTML из текста письма: абзацы и кликабельные ссылки; всё остальное экранируется.
func htmlFromText(text string) string {
	var b strings.Builder
	b.WriteString(`<div style="font-family:-apple-system,Segoe UI,Roboto,Helvetica,Arial,sans-serif;font-size:15px;line-height:1.55;color:#1f2340;max-width:560px">`)
	for _, para := range strings.Split(strings.TrimSpace(text), "\n\n") {
		b.WriteString(`<p style="margin:0 0 14px">`)
		lines := strings.Split(para, "\n")
		for i, line := range lines {
			if i > 0 {
				b.WriteString("<br>")
			}
			b.WriteString(linkify(line))
		}
		b.WriteString("</p>")
	}
	b.WriteString(`<p style="margin:22px 0 0;color:#8a8fb0;font-size:13px">Vladhost</p></div>`)
	return b.String()
}

func linkify(line string) string {
	var b strings.Builder
	last := 0
	for _, loc := range urlRe.FindAllStringIndex(line, -1) {
		b.WriteString(html.EscapeString(line[last:loc[0]]))
		u := line[loc[0]:loc[1]]
		fmt.Fprintf(&b, `<a href="%s" style="color:#6d4aff">%s</a>`, html.EscapeString(u), html.EscapeString(u))
		last = loc[1]
	}
	b.WriteString(html.EscapeString(line[last:]))
	return b.String()
}
