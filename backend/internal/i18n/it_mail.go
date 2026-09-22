package i18n

// Письма читателям (internal/mail, шаг 5.1.1): подтверждение почты, уведомление о повышении уровня.
func init() {
	register(IT, map[string]string{
		"КУПОЛ — вымышленный архив. Автоматическое письмо, отвечать на него не нужно.": "KUPOL — archivio immaginario. Messaggio automatico: non è necessario rispondere.",

		"Подтверждение почты": "Conferma della posta",
		"Вы указали этот адрес как почту аккаунта «%s» в архиве КУПОЛ. Чтобы подтвердить его, перейдите по ссылке ниже.":                "Hai indicato questo indirizzo come posta dell'account «%s» nell'archivio KUPOL. Per confermarlo, segui il link qui sotto.",
		"Ссылка действует 24 часа. Если это были не вы — просто не открывайте её: адрес подтверждён не будет, а аккаунт не пострадает.": "Il link è valido 24 ore. Se non sei stato tu, non aprirlo: l'indirizzo non verrà confermato e l'account non subirà alcun danno.",
		"Подтвердить почту":         "Conferma la posta",
		"Подтвердите почту — КУПОЛ": "Conferma la posta — KUPOL",

		"ДОПУСК ПОВЫШЕН": "ACCESSO ELEVATO",
		"%s, ваш допуск в архиве КУПОЛ повышен до уровня %d — «%s». Продолжайте нести службу.": "%s, il tuo livello di accesso nell'archivio KUPOL è stato elevato al livello %d — «%s». Continua il servizio.",
		"Допуск повышен — КУПОЛ": "Accesso elevato — KUPOL",
	})
}
