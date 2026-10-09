package i18n

// Итальянские переводы: лист печати (этап 6.2) — подписи колонтитулов, шапки досье, блоков и ответы API печати.
// Формулировки совпадают с итальянским интерфейсом сайта (frontend/src/i18n/messages/it/doc.ts),
// чтобы читатель видел одни и те же слова на сайте и на бумаге.
func init() {
	register(IT, map[string]string{
		// ——— ответы API печати ———
		"Не удалось напечатать дело": "Impossibile stampare il fascicolo",
		"Печать не найдена":          "Stampa non trovata",
		"Печать ещё не готова":       "La stampa non è ancora pronta",

		// ——— колонтитулы и шапка листа ———
		"Несекретно":                               "Non riservato",
		"Для служебного пользования":               "Ad uso di servizio",
		"Конфиденциально":                          "Confidenziale",
		"Секретно":                                 "Segreto",
		"Совершенно секретно":                      "Segretissimo",
		"Особой важности":                          "Di particolare importanza",
		"Особой важности · Особый Совет":           "Di particolare importanza · Consiglio Speciale",
		"Особой важности · только Директорат":      "Di particolare importanza · solo Direttorato",
		"Фонд %d · Опись %d · Дело %d · Листов %d": "Fondo %d · Inventario %d · Pratica %d · Fogli %d",
		"экз. б/н":  "copia s/n",
		"экз. № %s": "copia n. %s",
		"Шифр":      "Sigla",
		"Тип":       "Tipo",
		"допуск не ниже уровня %d (%s)": "accesso non inferiore al livello %d (%s)",
		"только Директорат":             "solo Direttorato",
		"Данные удалены":                "Dati eliminati",
		"Упоминается в":                 "Menzionato in",
		"Составил(а): %s":               "Redatto da: %s",
		"открытый":                      "aperto",
		"уровень %d — %s":               "livello %d — %s",
		"%d п.о.":                       "%d p.o.",

		// ——— блоки листа ———
		"Приложение № %s":         "Allegato n. %s",
		"Служебная записка":       "Nota di servizio",
		"Рукописная заметка":      "Appunto manoscritto",
		"Материал":                "Materiale",
		"Связанный документ":      "Documento collegato",
		"Засекречен":              "Secretato",
		"— стр. —":                "— pag. —",
		"— стр. %s —":             "— pag. %s —",
		"Срок: %s":                "Scadenza: %s",
		"Участники: %s":           "Partecipanti: %s",
		"Расшифровка":             "Trascrizione",
		"Изображение недоступно.": "Immagine non disponibile.",
		"На службе":               "In servizio",
		"Переведён(а)":            "Trasferito/a",
		"Погиб(ла)":               "Deceduto/a",
		"Неизвестно":              "Sconosciuto",
		"Пропал(а) без вести":     "Disperso/a",
		"Не подтверждено":         "Non confermata",
		"Подтверждено":            "Confermata",
		"Одобрено":                "Approvato",
		"Отклонено":               "Respinto",
		"На рассмотрении":         "In attesa",
		"Принято к сведению":      "Preso atto",

		// ——— дата составления: месяцы в единственном числе (падежи русского) ———
		"января": "gennaio", "февраля": "febbraio", "марта": "marzo", "апреля": "aprile",
		"мая": "maggio", "июня": "giugno", "июля": "luglio", "августа": "agosto",
		"сентября": "settembre", "октября": "ottobre", "ноября": "novembre", "декабря": "dicembre",
		"январь": "gennaio", "февраль": "febbraio", "март": "marzo", "апрель": "aprile",
		"май": "maggio", "июнь": "giugno", "июль": "luglio", "август": "agosto",
		"сентябрь": "settembre", "октябрь": "ottobre", "ноябрь": "novembre", "декабрь": "dicembre",
		"%d г.":       "%d",
		"%d %s %d г.": "%d %s %d",
		"%s %d г.":    "%s %d",

		// ——— сборка ———
		"сборка не удалась": "compilazione fallita",
	})
}
