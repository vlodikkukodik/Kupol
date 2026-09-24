package i18n

// Catalogo italiano. Ogni chiave deve esistere anche in ru.go: lo verifica i18n_test.go.
var it = map[string]string{
	// Errori generici
	"err.bad_request":       "Richiesta non valida",
	"err.too_many_requests": "Troppi tentativi, riprova tra un minuto",
	"err.unauthorized":      "Sessione non valida",
	"err.forbidden":         "Permessi insufficienti",
	"err.forbidden_origin":  "Richiesta da un'origine non autorizzata",
	"err.internal":          "Errore interno",

	// Account e inviti
	"err.invalid_invite":      "Invito non valido o già utilizzato",
	"err.email_taken":         "Questa email è già registrata",
	"err.username_taken":      "Questo nome è già in uso",
	"err.invalid_credentials": "Nome utente o password errati",
	"err.invite_ttl":          "Durata dell'invito: da 1 a %d ore",

	// Validazione dei dati inseriti
	"err.validation.email":             "Email non valida",
	"err.validation.username_format":   "Da 3 a 32 caratteri: lettere latine, cifre e trattino, senza trattino all'inizio o alla fine",
	"err.validation.username_dashes":   "Due trattini consecutivi non sono ammessi",
	"err.validation.username_reserved": "Questo nome è riservato",
	"err.validation.password_short":    "La password ha meno di 8 caratteri",
	"err.validation.password_long":     "La password supera i 72 byte",
	"err.validation.slug":              "Da 2 a 32 caratteri: lettere latine, cifre e trattino, senza trattino all'inizio o alla fine e senza trattini doppi",

	// Siti, file, certificati
	"err.site_limit":     "Limite di siti per account raggiunto",
	"err.slug_taken":     "Esiste già un sito con questo nome",
	"err.not_found":      "Sito non trovato",
	"err.file_not_found": "File o cartella non trovati",
	"err.bad_path":       "Percorso non valido",
	"err.exists":         "Esiste già un file o una cartella con questo nome",
	"err.is_dir":         "È una cartella, ma serve un file",
	"err.not_dir":        "È un file, ma serve una cartella",
	"err.too_large":      "File troppo grande per l'editor",
	"err.not_text":       "Non è un file di testo, impossibile modificarlo",
	"err.quota_exceeded": "Quota disco superata",
	"err.cert_state":     "Non è possibile ripetere l'emissione del certificato per questo sito",

	// Archivio durante il deploy
	"err.archive.not_zip":      "Non è un archivio zip oppure è danneggiato",
	"err.archive.too_many":     "Troppi file nell'archivio (massimo %d)",
	"err.archive.bad_path":     "Percorso non valido nell'archivio: %s",
	"err.archive.special_file": "Collegamenti simbolici e file speciali non sono ammessi nell'archivio: %s",
	"err.archive.duplicate":    "Il file compare due volte nell'archivio: %s",
	"err.archive.corrupt":      "File danneggiato nell'archivio: %s",
	"err.archive.unreadable":   "Impossibile leggere il file dall'archivio: %s",
	"err.archive.no_index":     "Nella radice dell'archivio manca index.html",

	// FTP
	"err.ftp_unavailable": "L'FTP non è attivo sul server",
	"err.ftp_auth":        "Nome utente o password FTP errati",
	"err.ftp_revoked":     "Accesso FTP revocato",
	"err.bad_offset":      "Offset oltre la dimensione del file",
	"err.not_supported":   "Operazione non supportata",

	"ftp.banner":            "Vladhost FTP. Solo FTPS (TLS esplicito).",
	"ftp.banner_plain":      "Vladhost FTP. Consigliamo FTPS (TLS esplicito): l'FTP semplice non cifra la password.",
	"ftp.too_many_conns":    "Troppe connessioni dal tuo indirizzo",
	"ftp.too_many_failures": "Troppi tentativi falliti, attendi",
}
