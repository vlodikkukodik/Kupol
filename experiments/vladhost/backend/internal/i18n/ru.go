package i18n

// Русский каталог. Ключи ошибок API — "err."+код (см. internal/apperr). Каждый ключ обязан быть и в it.go:
// это проверяет i18n_test.go.
var ru = map[string]string{
	// Общие ошибки
	"err.bad_request":       "Некорректный запрос",
	"err.too_many_requests": "Слишком много попыток, подождите минуту",
	"err.unauthorized":      "Сессия недействительна",
	"err.forbidden":         "Недостаточно прав",
	"err.forbidden_origin":  "Запрос с чужого источника",
	"err.internal":          "Внутренняя ошибка",

	// Аккаунты и инвайты
	"err.invalid_invite":      "Инвайт недействителен или уже использован",
	"err.email_taken":         "Этот email уже зарегистрирован",
	"err.username_taken":      "Это имя уже занято",
	"err.invalid_credentials": "Неверный логин или пароль",
	"err.invite_ttl":          "Срок инвайта: от 1 до %d часов",

	// Проверка введённых данных
	"err.validation.email":             "Некорректный email",
	"err.validation.username_format":   "3–32 символа: латиница, цифры и дефис, не с дефиса и не на дефис",
	"err.validation.username_dashes":   "Два дефиса подряд недопустимы",
	"err.validation.username_reserved": "Это имя зарезервировано",
	"err.validation.password_short":    "Пароль короче 8 символов",
	"err.validation.password_long":     "Пароль длиннее 72 байт",
	"err.validation.slug":              "2–32 символа: латиница, цифры и дефис, не с дефиса и не на дефис, без двойных дефисов",

	// Сайты, файлы, сертификаты
	"err.site_limit":     "Достигнут лимит сайтов на аккаунт",
	"err.slug_taken":     "Сайт с таким именем уже есть",
	"err.not_found":      "Сайт не найден",
	"err.file_not_found": "Файл или папка не найдены",
	"err.bad_path":       "Недопустимый путь",
	"err.exists":         "Такой файл или папка уже есть",
	"err.is_dir":         "Это папка, а нужен файл",
	"err.not_dir":        "Это файл, а нужна папка",
	"err.too_large":      "Файл слишком большой для редактора",
	"err.not_text":       "Это не текстовый файл, редактировать нельзя",
	"err.quota_exceeded": "Превышена квота диска",
	"err.cert_state":     "Для этого сайта нельзя повторить выпуск сертификата",

	// Архив при деплое
	"err.archive.not_zip":      "Это не zip-архив или он повреждён",
	"err.archive.too_many":     "Слишком много файлов в архиве (максимум %d)",
	"err.archive.bad_path":     "Недопустимый путь в архиве: %s",
	"err.archive.special_file": "Символические ссылки и спецфайлы в архиве не допускаются: %s",
	"err.archive.duplicate":    "Файл встречается в архиве дважды: %s",
	"err.archive.corrupt":      "Повреждённый файл в архиве: %s",
	"err.archive.unreadable":   "Не удалось прочитать файл из архива: %s",
	"err.archive.no_index":     "В корне архива нет index.html",

	// FTP
	"err.ftp_unavailable": "FTP на сервере не включён",
	"err.ftp_auth":        "Неверное имя или пароль FTP",
	"err.ftp_revoked":     "Доступ по FTP отозван",
	"err.bad_offset":      "Смещение больше размера файла",
	"err.not_supported":   "Операция не поддерживается",

	"ftp.banner":            "Vladhost FTP. Только FTPS (явный TLS).",
	"ftp.banner_plain":      "Vladhost FTP. Рекомендуем FTPS (явный TLS): обычный FTP не шифрует пароль.",
	"ftp.too_many_conns":    "Слишком много соединений с вашего адреса",
	"ftp.too_many_failures": "Слишком много неудачных попыток, подождите",
}
