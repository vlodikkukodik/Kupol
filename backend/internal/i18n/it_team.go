package i18n

// Итальянские переводы: ответы API панели команды (документы, шаблоны, глоссарий, хронология).
func init() {
	register(IT, map[string]string{
		"Документ не найден":                                        "Documento non trovato",
		"Шаблон не найден":                                          "Modello non trovato",
		"Шаблон с таким названием уже есть":                         "Esiste già un modello con questo nome",
		"Такое название уже занято: выберите другое":                "Questo nome è già occupato: scegline un altro",
		"Событие не найдено":                                        "Evento non trovato",
		"Термин не найден":                                          "Termine non trovato",
		"Такой термин уже есть":                                     "Questo termine esiste già",
		"Такой термин уже есть в глоссарии":                         "Questo termine è già nel glossario",
		"Свой документ проверяет другой Редактор":                   "Il proprio documento lo verifica un altro Redattore",
		"Комментарий не найден":                                     "Commento non trovato",
		"Это действие не подходит документу в его нынешнем статусе": "Questa azione non è adatta allo stato attuale del documento",
		"Документ не прошёл проверку канона: %s":                    "Il documento non ha superato il controllo del canone: %s",
		"Этот шифр уже занят":                                       "Questa sigla è già occupata",
		"Этот шифр уже занят другим документом":                     "Questa sigla è già occupata da un altro documento",
		"Документ правит %s":                                        "Il documento è in modifica da parte di %s",
		"Документ изменён после того, как вы его открыли":           "Il documento è stato modificato dopo che lo hai aperto",
		"Проверьте содержимое документа":                            "Controlla il contenuto del documento",
		"недопустимое значение %q; допустимы: not_found, forbidden": "valore non ammesso %q; ammessi: not_found, forbidden",
	})
}
