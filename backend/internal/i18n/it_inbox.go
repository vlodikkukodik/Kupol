package i18n

// Итальянские переводы: внутренняя почта (шаг 5.7).
func init() {
	register(IT, map[string]string{
		"Записка не найдена": "Messaggio non trovato",
		"Такого логина нет":  "Questo login non esiste",
		"Допуск повышен":     "Accesso aumentato",
		"Ваш допуск повышен до уровня %d. Поздравляем!": "Il tuo accesso è stato portato al livello %d. Congratulazioni!",
		"Новая грамота":                 "Nuovo attestato",
		"Вам выдана грамота: «%s»":      "Ti è stato assegnato l'attestato: «%s»",
		"Решение по вашему предложению": "Decisione sulla tua proposta",
		"Ваше предложение: %s.":         "La tua proposta: %s.",
		"рассмотрено":                   "esaminata",
		"принято":                       "accettata",
		"отклонено":                     "respinta",
		"Ответ на вашу пометку":         "Risposta alla tua nota",
		"На вашу пометку на полях ответили в деле %s.": "Hanno risposto alla tua nota a margine nel fascicolo %s.",
		"Заголовок: от 1 до 200 знаков":                "Titolo: da 1 a 200 caratteri",
		"Текст: от 1 до 4000 знаков":                   "Testo: da 1 a 4000 caratteri",
	})
}

// Ходатайства о допуске (шаг 5.8).
func init() {
	register(IT, map[string]string{
		"Решение по вашему ходатайству":                          "Decisione sulla tua istanza",
		"Ходатайство одобрено: ваш допуск повышен до уровня %d.": "Istanza approvata: il tuo accesso è stato portato al livello %d.",
		"Ходатайство на уровень %d отклонено.":                   "L'istanza per il livello %d è stata respinta.",
		"Ходатайство не найдено":                                 "Istanza non trovata",
		"Своё ходатайство решает другой член Совета":             "L'istanza propria la decide un altro membro del Consiglio",
		"У вас уже есть нерассмотренное ходатайство":             "Hai già un'istanza non ancora esaminata",
		"Ходатайство на следующий уровень сейчас недоступно":     "L'istanza per il livello successivo al momento non è disponibile",
		"Это ходатайство уже нельзя решить":                      "Questa istanza non può più essere decisa",
		"Решение: approved или rejected":                         "Decisione: approved o rejected",
		"Текст ходатайства: от 1 до 2000 знаков":                 "Testo dell'istanza: da 1 a 2000 caratteri",
		"Комментарий: не больше 2000 знаков":                     "Commento: non più di 2000 caratteri",
	})
}

// Наказания (шаг 5.9).
func init() {
	register(IT, map[string]string{
		"Предупреждение": "Ammonimento",
		"Модерация вынесла вам предупреждение. Причина: %s":        "La moderazione ti ha rivolto un ammonimento. Motivo: %s",
		"Блокировка комментариев":                                  "Blocco dei commenti",
		"Вам запрещено писать пометки на полях до %s. Причина: %s": "Non puoi scrivere note a margine fino al %s. Motivo: %s",
		"Вам временно запрещено писать пометки на полях до %s":     "Ti è temporaneamente vietato scrivere note a margine fino al %s",
		"Аккаунт заблокирован Модерацией":                          "Account bloccato dalla moderazione",
		"Наказание не найдено":                                     "Sanzione non trovata",
		"Этого пользователя наказать нельзя":                       "Questo utente non può essere sanzionato",
		"Наказание уже снято":                                      "La sanzione è già stata revocata",
		"Вид наказания: warning, comment_ban или ban":              "Tipo di sanzione: warning, comment_ban o ban",
		"Причина: от 1 до 1000 знаков":                             "Motivo: da 1 a 1000 caratteri",
		"Срок блокировки комментариев: от 1 до 365 дней":           "Durata del blocco dei commenti: da 1 a 365 giorni",
	})
}

// Приглашения Совета и карточка пользователя.
func init() {
	register(IT, map[string]string{
		"Приглашение Совета": "Invito del Consiglio",
		"Особый Совет приглашает вас на уровень %d. Примите приглашение в личном деле.": "Il Consiglio Speciale ti invita al livello %d. Accetta l'invito nel fascicolo personale.",
		"Ответ на приглашение": "Risposta all'invito",
		"Приглашённый вами читатель принял приглашение на уровень %d.":   "Il lettore da te invitato ha accettato l'invito al livello %d.",
		"Приглашённый вами читатель отклонил приглашение на уровень %d.": "Il lettore da te invitato ha rifiutato l'invito al livello %d.",
		"Приглашение не найдено":              "Invito non trovato",
		"Приглашение уже закрыто":             "L'invito è già chiuso",
		"Себя пригласить нельзя":              "Non puoi invitare te stesso",
		"Ответ: accept, decline или withdraw": "Risposta: accept, decline o withdraw",
	})
}

// Загрузки (этап 6.1).
func init() {
	register(IT, map[string]string{
		"Файл не найден": "File non trovato",
		"Билет на загрузку недействителен: получите новый": "Il biglietto di caricamento non è valido: ottienine uno nuovo",
		"Файл больше 20 МБ": "Il file supera i 20 MB",
		"Нужна картинка JPEG, PNG или WebP либо аудио mp3 или ogg":  "Serve un'immagine JPEG, PNG o WebP oppure un audio mp3 o ogg",
		"Файл используется в документе: сначала уберите его оттуда": "Il file è usato in un documento: rimuovilo prima da lì",
		"Выберите файл": "Scegli un file",
	})
}
