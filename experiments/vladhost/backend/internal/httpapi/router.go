// Package httpapi: JSON API панели (/api/...). Публичного API для внешних клиентов нет.
package httpapi

import (
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"vladhost/internal/activity"
	"vladhost/internal/apperr"
	"vladhost/internal/auth"
	"vladhost/internal/cms"
	"vladhost/internal/config"
	"vladhost/internal/cronjobs"
	"vladhost/internal/dnszones"
	"vladhost/internal/i18n"
	"vladhost/internal/mailhost"
	"vladhost/internal/notify"
	"vladhost/internal/runtimes"
	"vladhost/internal/shellaccess"
	"vladhost/internal/shellclient"
	"vladhost/internal/sites"
	support "vladhost/internal/tickets"
	"vladhost/internal/userdb"
)

type Server struct {
	svc         *auth.Service
	sites       *sites.Service
	cfg         config.Config
	mail        *notify.Service // nil — почта выключена
	dbs         *userdb.Service // nil — раздел «Базы данных» выключен
	cron        *cronjobs.Service
	rt          *runtimes.Service    // nil — среды выполнения выключены
	cms         *cms.Service         // nil — установка приложений выключена
	shell       *shellaccess.Service // nil — SSH и веб-терминал выключены
	shellBroker shellclient.Client
	activity    *activity.Service // nil — журнал действий выключен
	support     *support.Service  // nil — обращения в поддержку выключены
	dns         *dnszones.Service // nil — собственный DNS выключен
	mailhost    *mailhost.Service // nil — почта на своих доменах выключена
	tickets     *tickets
	dbKey       string // секрет обмена токена входа между панелью и веб-клиентом

	// На бою (Secure) cookie с префиксом __Host-: браузер не даст сайту на соседнем поддомене
	// подбросить свою cookie с этим именем на весь домен. Такой префикс требует Path=/.
	cookieName, cookiePath string
}

// Option настраивает необязательные части сервера.
type Option func(*Server)

// WithMail подключает отправку писем (подтверждение почты, сброс пароля). Без неё эти возможности выключены.
func WithMail(m *notify.Service) Option { return func(s *Server) { s.mail = m } }

func New(svc *auth.Service, sitesSvc *sites.Service, cfg config.Config, opts ...Option) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	s := &Server{svc: svc, sites: sitesSvc, cfg: cfg, cookieName: "vh_refresh", cookiePath: "/api/auth"}
	for _, o := range opts {
		o(s)
	}
	if cfg.CookieSecure {
		s.cookieName, s.cookiePath = "__Host-vh_refresh", "/"
	}
	r := gin.New()
	r.Use(gin.Recovery(), langMiddleware, clientMiddleware)
	// Перед Go стоит nginx на этой же машине.
	_ = r.SetTrustedProxies([]string{"127.0.0.1", "::1"})

	api := r.Group("/api")
	api.GET("/healthz", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"ok": true}) })

	perMinute, burst := cfg.AuthPerMinute, cfg.AuthBurst
	if perMinute == 0 {
		perMinute = 20
	}
	if burst == 0 {
		burst = 10
	}
	limited := api.Group("/auth", s.checkOrigin, newIPLimiter(perMinute, burst).middleware())
	limited.POST("/register", s.register)
	limited.POST("/login", s.login)
	limited.POST("/login/2fa", s.loginSecondFactor)
	limited.POST("/refresh", s.refresh)
	limited.POST("/forgot", s.forgotPassword)
	limited.POST("/reset", s.resetPassword)
	limited.POST("/verify-email", s.verifyEmail)
	api.POST("/auth/logout", s.checkOrigin, s.logout)

	// Смена пароля — тоже место для подбора текущего пароля, поэтому лимит на IP такой же, как у входа.
	pwLimiter := newIPLimiter(perMinute, burst)
	authed := api.Group("", s.requireAuth, s.audit())
	authed.GET("/activity", s.listActivity)
	authed.GET("/me", s.me)
	authed.PATCH("/me", s.checkOrigin, s.updateMe)
	authed.POST("/me/email/verify", s.checkOrigin, s.resendVerification)
	authed.POST("/me/password", s.checkOrigin, pwLimiter.middleware(), s.changePassword)
	authed.GET("/me/2fa", s.twoFactorStatus)
	authed.POST("/me/2fa/setup", s.checkOrigin, pwLimiter.middleware(), s.twoFactorSetup)
	authed.POST("/me/2fa/enable", s.checkOrigin, pwLimiter.middleware(), s.twoFactorEnable)
	authed.POST("/me/2fa/disable", s.checkOrigin, pwLimiter.middleware(), s.twoFactorDisable)
	authed.POST("/me/2fa/recovery", s.checkOrigin, pwLimiter.middleware(), s.twoFactorRecovery)
	authed.GET("/me/sessions", s.listSessions)
	authed.DELETE("/me/sessions/:sid", s.checkOrigin, s.revokeSession)
	authed.POST("/me/sessions/revoke-others", s.checkOrigin, s.revokeOtherSessions)
	authed.GET("/sites", s.listSites)
	authed.POST("/sites", s.createSite)
	authed.DELETE("/sites/:id", s.deleteSite)
	authed.POST("/sites/:id/deploy", s.deploySite)
	authed.POST("/sites/:id/cert/retry", s.retryCert)
	authed.POST("/sites/:id/domains", s.addDomain)
	authed.POST("/sites/:id/subdomains", s.addSubdomain)
	authed.PATCH("/sites/:id/domains/:did", s.updateDomain)
	authed.POST("/sites/:id/domains/:did/check", s.checkDomain)
	authed.DELETE("/sites/:id/domains/:did", s.removeDomain)
	authed.POST("/sites/:id/ftp", s.enableFTP)
	authed.DELETE("/sites/:id/ftp", s.disableFTP)
	authed.POST("/sites/:id/ftp/accounts", s.createFTPAccount)
	authed.PATCH("/sites/:id/ftp/accounts/:aid", s.updateFTPAccount)
	authed.POST("/sites/:id/ftp/accounts/:aid/password", s.resetFTPAccountPassword)
	authed.DELETE("/sites/:id/ftp/accounts/:aid", s.deleteFTPAccount)
	authed.GET("/sites/:id/files", s.listFiles)
	authed.DELETE("/sites/:id/files", s.deleteFile)
	authed.POST("/sites/:id/files/mkdir", s.mkdir)
	authed.POST("/sites/:id/files/rename", s.renameFile)
	authed.POST("/sites/:id/files/upload", s.uploadFile)
	authed.GET("/sites/:id/htaccess", s.checkHtaccess)
	authed.GET("/sites/:id/logs", s.siteLogs)
	authed.GET("/sites/:id/stats", s.siteStats)
	authed.POST("/sites/:id/certs/renew", s.renewCert)
	authed.GET("/sites/:id/settings", s.getSiteSettings)
	authed.PUT("/sites/:id/settings", s.putSiteSettings)
	authed.GET("/sites/:id/logs/download", s.downloadSiteLogs)
	authed.GET("/sites/:id/file", s.readFile)
	authed.PUT("/sites/:id/file", s.saveFile)
	authed.GET("/sites/:id/backups", s.listBackups)
	authed.POST("/sites/:id/backups", s.createBackup)
	authed.POST("/sites/:id/backups/:bid/restore", s.restoreBackup)
	authed.GET("/sites/:id/backups/:bid/download", s.downloadBackup)
	authed.GET("/sites/:id/archive", s.downloadArchive)

	sh := authed.Group("", s.requireShell)
	sh.GET("/ssh/keys", s.listSSHKeys)
	sh.POST("/ssh/keys", s.checkOrigin, s.addSSHKey)
	sh.POST("/ssh/keys/generate", s.checkOrigin, s.generateSSHKey)
	sh.DELETE("/ssh/keys/:id", s.checkOrigin, s.deleteSSHKey)
	sh.GET("/sites/:id/shell", s.getShell)
	sh.PUT("/sites/:id/shell", s.checkOrigin, s.setShell)
	sh.POST("/sites/:id/terminal", s.checkOrigin, s.terminalTicket)

	mh := authed.Group("/mail", s.requireMailHost)
	mh.GET("", s.mailOverview)
	mh.POST("/domains", s.checkOrigin, s.mailEnableDomain)
	mh.POST("/domains/:id/verify", s.checkOrigin, s.mailVerifyDomain)
	mh.PATCH("/domains/:id", s.checkOrigin, s.mailSetDomain)
	mh.DELETE("/domains/:id", s.checkOrigin, s.mailDeleteDomain)
	mh.GET("/domains/:id/dns", s.mailDNS)
	mh.POST("/domains/:id/dns/auto", s.checkOrigin, s.mailDNSAuto)
	mh.GET("/domains/:id/log", s.mailLog)
	mh.POST("/domains/:id/mailboxes", s.checkOrigin, s.mailCreateMailbox)
	mh.PUT("/domains/:id/aliases", s.checkOrigin, s.mailSetAlias)
	mh.PATCH("/mailboxes/:id", s.checkOrigin, s.mailUpdateMailbox)
	mh.PUT("/mailboxes/:id/rules", s.checkOrigin, s.mailSetRules)
	mh.PUT("/mailboxes/:id/password", s.checkOrigin, s.mailSetPassword)
	mh.DELETE("/mailboxes/:id", s.checkOrigin, s.mailDeleteMailbox)
	mh.DELETE("/aliases/:id", s.checkOrigin, s.mailDeleteAlias)

	tg := authed.Group("/tickets", s.requireSupport)
	tg.GET("", s.listTickets)
	tg.POST("", s.checkOrigin, s.createTicket)
	tg.GET("/summary", s.ticketsSummary)
	tg.GET("/:id", s.getTicket)
	tg.POST("/:id/messages", s.checkOrigin, s.replyTicket)
	tg.POST("/:id/close", s.checkOrigin, s.closeTicket)
	tg.POST("/:id/reopen", s.checkOrigin, s.reopenTicket)

	dg := authed.Group("/dns", s.requireDNS)
	dg.GET("", s.dnsOverview)
	dg.POST("/zones", s.checkOrigin, s.dnsEnableZone)
	dg.DELETE("/zones/:id", s.checkOrigin, s.dnsDeleteZone)
	dg.POST("/zones/:id/verify", s.checkOrigin, s.dnsVerifyZone)
	dg.GET("/zones/:id/delegation", s.dnsDelegation)
	dg.POST("/zones/:id/records", s.checkOrigin, s.dnsAddRecord)
	dg.PUT("/records/:id", s.checkOrigin, s.dnsUpdateRecord)
	dg.DELETE("/records/:id", s.checkOrigin, s.dnsDeleteRecord)

	cmsg := authed.Group("/sites/:id/cms", s.requireCMS)
	cmsg.GET("", s.getCMS)
	cmsg.POST("", s.checkOrigin, s.installCMS)
	cmsg.GET("/job", s.cmsJob)

	rtg := authed.Group("/sites/:id/runtime", s.requireRuntimes)
	rtg.GET("", s.getRuntime)
	rtg.PUT("", s.checkOrigin, s.setRuntime)
	rtg.POST("/restart", s.checkOrigin, s.restartRuntime)
	rtg.GET("/logs", s.runtimeLogs)

	cr := authed.Group("/cron", s.requireCron)
	cr.GET("", s.listCron)
	cr.POST("", s.checkOrigin, s.createCron)
	cr.PUT("/:id", s.checkOrigin, s.updateCron)
	cr.DELETE("/:id", s.checkOrigin, s.deleteCron)
	cr.POST("/:id/run", s.checkOrigin, s.runCron)
	cr.GET("/:id/runs", s.cronRuns)

	dbs := authed.Group("/databases", s.requireDatabases)
	dbs.GET("", s.listDatabases)
	dbs.POST("", s.checkOrigin, s.createDatabase)
	dbs.DELETE("/:id", s.checkOrigin, s.deleteDatabase)
	dbs.POST("/:id/password", s.checkOrigin, s.resetDatabasePassword)
	dbs.PUT("/:id/addrs", s.checkOrigin, s.setDatabaseAddrs)
	dbs.POST("/:id/web", s.checkOrigin, s.openDatabaseWeb)
	dbs.POST("/:id/check", s.checkOrigin, s.checkDatabase)
	// Служебный обмен токена веб-клиента: не под requireAuth, защищён адресом loopback и общим секретом (см. dbSession).
	api.POST("/internal/db-session", s.requireDatabases, s.dbSession)
	// Веб-терминал: вход подтверждает одноразовый билет, выданный авторизованным запросом (заголовок Authorization у WebSocket не задать).
	api.GET("/terminal/ws", s.requireShell, s.terminalSocket)

	admin := authed.Group("", s.requireAdmin)
	admin.GET("/invites", s.listInvites)
	admin.POST("/invites", s.createInvite)
	admin.GET("/admin/tickets", s.requireSupport, s.adminTickets)
	return r
}

type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Field   string `json:"field,omitempty"`
}

// lang — язык ответа для этого запроса (см. langMiddleware).
func lang(c *gin.Context) i18n.Lang {
	if l, ok := c.Get("lang"); ok {
		if v, ok := l.(i18n.Lang); ok {
			return v
		}
	}
	return i18n.Default
}

// langMiddleware выбирает язык по Accept-Language: фронтенд ставит его по переключателю в шапке.
func langMiddleware(c *gin.Context) {
	l := i18n.FromAcceptLanguage(c.GetHeader("Accept-Language"))
	c.Set("lang", l)
	c.Header("Content-Language", string(l))
	c.Writer.Header().Add("Vary", "Accept-Language")
	c.Next()
}

// fail отвечает ошибкой с текстом из каталога: ключ "err."+code, args подставляются в текст.
func fail(c *gin.Context, status int, code string, args ...any) {
	c.AbortWithStatusJSON(status, gin.H{"error": apiError{Code: code, Message: i18n.T(lang(c), "err."+code, args...)}})
}

// failErr переводит ошибку домена в HTTP-ответ; неизвестные ошибки не раскрываются клиенту.
func failErr(c *gin.Context, err error) {
	if ae, ok := errors.AsType[*apperr.Error](err); ok {
		c.AbortWithStatusJSON(ae.Status, gin.H{"error": apiError{
			Code: ae.Code, Message: i18n.T(lang(c), "err."+ae.Code, ae.Args...), Field: ae.Field,
		}})
		return
	}
	_ = c.Error(err)
	fail(c, http.StatusInternalServerError, "internal")
}

func (s *Server) setRefreshCookie(c *gin.Context, value string, maxAge int) {
	c.SetSameSite(http.SameSiteStrictMode)
	c.SetCookie(s.cookieName, value, maxAge, s.cookiePath, "", s.cfg.CookieSecure, true)
}

func (s *Server) respondSession(c *gin.Context, sess *auth.Session) {
	s.setRefreshCookie(c, sess.RefreshToken, int(time.Until(sess.RefreshExpires).Seconds()))
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{
		"access_token":      sess.AccessToken,
		"expires_in":        sess.ExpiresIn,
		"user":              sess.User,
		"mail_enabled":      s.mail.Enabled(),
		"databases_enabled": s.dbs.Enabled(), "shell_enabled": s.shell.Enabled(), "mailhost_enabled": s.mailhost.Enabled(), "dns_enabled": s.dns.Enabled(),
	})
}

func (s *Server) register(c *gin.Context) {
	var in struct {
		Invite   string `json:"invite"`
		Email    string `json:"email"`
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if c.ShouldBindJSON(&in) != nil {
		fail(c, http.StatusBadRequest, "bad_request")
		return
	}
	sess, err := s.svc.Register(c.Request.Context(), auth.RegisterInput{
		Invite: in.Invite, Email: in.Email, Username: in.Username, Password: in.Password, Lang: string(lang(c)),
	})
	if err != nil {
		failErr(c, err)
		return
	}
	s.sendVerification(c, sess.User) // письмо с подтверждением; сбой почты регистрацию не ломает
	s.record(c, sess.User.ID, activity.KindRegister, "")
	s.respondSession(c, sess)
}

func (s *Server) login(c *gin.Context) {
	var in struct {
		Login    string `json:"login"`
		Password string `json:"password"`
	}
	if c.ShouldBindJSON(&in) != nil || in.Login == "" || in.Password == "" {
		fail(c, http.StatusBadRequest, "bad_request")
		return
	}
	sess, ticket, err := s.svc.Login(c.Request.Context(), in.Login, in.Password)
	if err != nil {
		// Неудачный вход пишется тому, чей аккаунт пытались открыть; ответ от этого не меняется (не выдаём, есть ли такой аккаунт).
		if errors.Is(err, auth.ErrInvalidCredentials) && s.activity != nil {
			if uid := s.svc.UserIDByLogin(c.Request.Context(), in.Login); uid > 0 {
				s.record(c, uid, activity.KindLoginFailed, "")
			}
		}
		failErr(c, err)
		return
	}
	if sess == nil { // пароль верен, нужен код второго фактора: сессии пока нет
		c.Header("Cache-Control", "no-store")
		c.JSON(http.StatusOK, gin.H{"two_factor": true, "ticket": ticket})
		return
	}
	s.record(c, sess.User.ID, activity.KindLogin, "")
	s.respondSession(c, sess)
}

func (s *Server) refresh(c *gin.Context) {
	raw, err := c.Cookie(s.cookieName)
	if err != nil || raw == "" {
		fail(c, http.StatusUnauthorized, "unauthorized")
		return
	}
	sess, err := s.svc.Refresh(c.Request.Context(), raw)
	if err != nil {
		if errors.Is(err, auth.ErrInvalidToken) {
			s.setRefreshCookie(c, "", -1)
		}
		failErr(c, err)
		return
	}
	s.respondSession(c, sess)
}

func (s *Server) logout(c *gin.Context) {
	if raw, err := c.Cookie(s.cookieName); err == nil && raw != "" {
		var uid int64
		if s.activity != nil {
			uid = s.svc.SessionUser(c.Request.Context(), raw)
		}
		if err := s.svc.Logout(c.Request.Context(), raw); err != nil {
			failErr(c, err)
			return
		}
		if uid > 0 {
			s.record(c, uid, activity.KindLogout, "")
		}
	}
	s.setRefreshCookie(c, "", -1)
	c.Status(http.StatusNoContent)
}

func (s *Server) me(c *gin.Context) {
	u, err := s.svc.UserByID(c.Request.Context(), c.GetInt64("uid"))
	if err != nil {
		failErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"user": u, "mail_enabled": s.mail.Enabled(), "databases_enabled": s.dbs.Enabled(), "shell_enabled": s.shell.Enabled(), "mailhost_enabled": s.mailhost.Enabled(), "dns_enabled": s.dns.Enabled()})
}

func (s *Server) changePassword(c *gin.Context) {
	var in struct {
		Current string `json:"current_password"`
		New     string `json:"new_password"`
	}
	if c.ShouldBindJSON(&in) != nil || in.Current == "" || in.New == "" {
		fail(c, http.StatusBadRequest, "bad_request")
		return
	}
	sess, err := s.svc.ChangePassword(c.Request.Context(), c.GetInt64("uid"), c.GetInt64("sid"), in.Current, in.New)
	if err != nil {
		failErr(c, err)
		return
	}
	s.notifyPasswordChanged(c, sess.User)
	s.respondSession(c, sess)
}

func (s *Server) listInvites(c *gin.Context) {
	list, err := s.svc.ListInvites(c.Request.Context())
	if err != nil {
		failErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"invites": list})
}

func (s *Server) createInvite(c *gin.Context) {
	var in struct {
		TTLHours int `json:"ttl_hours"`
	}
	// Пустое тело допустимо: тогда берётся срок по умолчанию.
	_ = c.ShouldBindJSON(&in)
	if in.TTLHours == 0 {
		in.TTLHours = 24 * 7
	}
	if in.TTLHours < 1 || in.TTLHours > 24*30 {
		fail(c, http.StatusUnprocessableEntity, "invite_ttl", 24*30)
		return
	}
	inv, err := s.svc.CreateInvite(c.Request.Context(), c.GetInt64("uid"), time.Duration(in.TTLHours)*time.Hour)
	if err != nil {
		failErr(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"invite": inv})
}
