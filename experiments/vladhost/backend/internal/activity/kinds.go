package activity

// События, которые пишутся явно (вход и подобные действия вне группы «вошедший пользователь»).
const (
	KindLogin         = "auth.login"
	KindLoginFailed   = "auth.login_failed"
	KindLogout        = "auth.logout"
	KindRegister      = "auth.register"
	KindPasswordReset = "auth.password_reset"
	KindEmailVerified = "auth.email_verified"
	KindFTPLogin      = "ftp.login"
)

// Skip — у маршрута нет события: он в журнал не пишется (проверки, служебные и обращения в поддержку).
const Skip = "-"

// RouteKinds — что означает успешный запрос вошедшего пользователя: «метод путь» → событие. Маршрут, которого здесь нет, но который меняет
// данные, пишется как «other» с шаблоном маршрута в поле «объект»: новые возможности не остаются вне журнала, пока им не подберут имя.
var RouteKinds = map[string]string{
	"PATCH /api/me":                   "profile.update",
	"POST /api/me/email/verify":       "auth.email_verify_sent",
	"POST /api/me/password":           "auth.password_change",
	"POST /api/invites":               "admin.invite",
	"POST /api/sites":                 "site.create",
	"DELETE /api/sites/:id":           "site.delete",
	"POST /api/sites/:id/deploy":      "site.deploy",
	"PUT /api/sites/:id/settings":     "site.settings",
	"POST /api/sites/:id/cert/retry":  "cert.renew",
	"POST /api/sites/:id/certs/renew": "cert.renew",

	"POST /api/sites/:id/domains":                    "domain.add",
	"PATCH /api/sites/:id/domains/:did":              "domain.update",
	"DELETE /api/sites/:id/domains/:did":             "domain.remove",
	"POST /api/sites/:id/domains/:did/check":         Skip,
	"POST /api/sites/:id/subdomains":                 "domain.sub_add",
	"POST /api/sites/:id/ftp":                        "ftp.enable",
	"DELETE /api/sites/:id/ftp":                      "ftp.disable",
	"POST /api/sites/:id/ftp/accounts":               "ftp.account_add",
	"PATCH /api/sites/:id/ftp/accounts/:aid":         "ftp.account_update",
	"POST /api/sites/:id/ftp/accounts/:aid/password": "ftp.password",
	"DELETE /api/sites/:id/ftp/accounts/:aid":        "ftp.account_delete",

	"DELETE /api/sites/:id/files":              "files.change",
	"POST /api/sites/:id/files/mkdir":          "files.change",
	"POST /api/sites/:id/files/rename":         "files.change",
	"POST /api/sites/:id/files/upload":         "files.change",
	"PUT /api/sites/:id/file":                  "files.change",
	"POST /api/sites/:id/backups":              "backup.create",
	"POST /api/sites/:id/backups/:bid/restore": "backup.restore",

	// скачивание данных сайта — тоже событие безопасности: кто и когда забрал копию
	"GET /api/sites/:id/backups/:bid/download": "backup.download",
	"GET /api/sites/:id/archive":               "backup.archive",

	"PUT /api/sites/:id/shell":     "shell.change",
	"POST /api/sites/:id/terminal": "shell.terminal",
	"POST /api/ssh/keys":           "ssh.key_add",
	"POST /api/ssh/keys/generate":  "ssh.key_generate",
	"DELETE /api/ssh/keys/:id":     "ssh.key_delete",

	"POST /api/sites/:id/cms":             "cms.install",
	"PUT /api/sites/:id/runtime":          "runtime.set",
	"POST /api/sites/:id/runtime/restart": "runtime.restart",

	"POST /api/cron":                   "cron.create",
	"PUT /api/cron/:id":                "cron.update",
	"DELETE /api/cron/:id":             "cron.delete",
	"POST /api/cron/:id/run":           "cron.run",
	"POST /api/databases":              "db.create",
	"DELETE /api/databases/:id":        "db.delete",
	"POST /api/databases/:id/password": "db.password",
	"PUT /api/databases/:id/addrs":     "db.addrs",
	"POST /api/databases/:id/web":      "db.web",
	"POST /api/databases/:id/check":    Skip, // обновление размера базы: ничего не меняет

	"POST /api/mail/domains":               "mail.domain_add",
	"PATCH /api/mail/domains/:id":          "mail.domain_update",
	"POST /api/mail/domains/:id/verify":    "mail.domain_verify",
	"DELETE /api/mail/domains/:id":         "mail.domain_delete",
	"POST /api/mail/domains/:id/dns/auto":  "mail.dns_auto",
	"POST /api/mail/domains/:id/mailboxes": "mail.mailbox_add",
	"PUT /api/mail/domains/:id/aliases":    "mail.alias_set",
	"PATCH /api/mail/mailboxes/:id":        "mail.mailbox_update",
	"PUT /api/mail/mailboxes/:id/rules":    "mail.mailbox_rules",
	"PUT /api/mail/mailboxes/:id/password": "mail.mailbox_password",
	"DELETE /api/mail/mailboxes/:id":       "mail.mailbox_delete",
	"DELETE /api/mail/aliases/:id":         "mail.alias_delete",

	"POST /api/dns/zones":             "dns.zone_add",
	"DELETE /api/dns/zones/:id":       "dns.zone_delete",
	"POST /api/dns/zones/:id/verify":  "dns.zone_verify",
	"POST /api/dns/zones/:id/records": "dns.record_add",
	"PUT /api/dns/records/:id":        "dns.record_update",
	"DELETE /api/dns/records/:id":     "dns.record_delete",

	"POST /api/tickets":              Skip,
	"POST /api/tickets/:id/messages": Skip,
	"POST /api/tickets/:id/close":    Skip,
	"POST /api/tickets/:id/reopen":   Skip,
}

// KindOf возвращает событие маршрута: (событие, есть ли маршрут в таблице). Skip означает «не писать».
func KindOf(key string) (string, bool) {
	k, ok := RouteKinds[key]
	return k, ok
}

// Audited: чтение, которое всё же пишется (скачивание данных), — маршрут есть в таблице и не пропускается.
func Audited(key string) bool {
	k, ok := RouteKinds[key]
	return ok && k != Skip
}
