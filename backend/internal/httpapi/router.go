// Package httpapi собирает HTTP-маршрутизатор Go API.
package httpapi

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"kupol/internal/accounts"
	"kupol/internal/config"
	"kupol/internal/documents"
	"kupol/internal/ratelimit"
)

type Deps struct {
	Config    config.Config
	DB        *gorm.DB
	Log       *slog.Logger
	Accounts  *accounts.Service
	Documents *documents.Service
	Limiter   *ratelimit.Limiter
}

func New(d Deps) (*gin.Engine, error) {
	if d.Accounts == nil || d.Documents == nil || d.Limiter == nil {
		return nil, errors.New("httpapi: не заданы Accounts, Documents и Limiter")
	}
	gin.SetMode(gin.ReleaseMode)

	r := gin.New()
	r.HandleMethodNotAllowed = true
	r.RedirectTrailingSlash = false
	r.RedirectFixedPath = false
	// Пустой (не nil) список: не доверять X-Forwarded-For ни от кого, кроме явно заданных.
	trusted := d.Config.TrustedProxies
	if trusted == nil {
		trusted = []string{}
	}
	if err := r.SetTrustedProxies(trusted); err != nil {
		return nil, err
	}

	r.Use(
		requestIDMiddleware(),
		languageMiddleware(),
		recoveryMiddleware(d.Log),
		proxyTrustMiddleware(d.Config.ProxySecret, d.Log),
		accessLogMiddleware(d.Log),
		securityHeadersMiddleware(),
		bodyLimitMiddleware(MaxBodyBytes),
		csrfMiddleware(d.Config.SiteOrigin),
	)

	r.NoRoute(func(c *gin.Context) {
		Fail(c, http.StatusNotFound, CodeNotFound, "Дело не найдено")
	})
	r.NoMethod(func(c *gin.Context) {
		Fail(c, http.StatusMethodNotAllowed, CodeMethodNotAllowed, "Метод не поддерживается")
	})

	// Проверка здоровья — без сессий и лимитов: её опрашивают мониторинг и сам сайт каждую минуту.
	health := healthHandler(d.DB)
	r.GET("/health", health)
	r.GET("/api/health", health)

	auth := &authHandlers{svc: d.Accounts, secure: d.Config.CookieSecure(), log: d.Log}

	api := r.Group("/api",
		ipFloodMiddleware(d.Limiter, d.Config.Limits.APIPerMinute),
		sessionMiddleware(d.Accounts, d.Config.CookieSecure(), d.Log),
		userRateLimitMiddleware(d.Limiter, d.Config.Limits.APIPerMinute),
	)
	api.GET("/auth/session", auth.session)
	api.GET("/auth/captcha", auth.captcha)
	api.POST("/auth/register", auth.register)
	api.POST("/auth/login", auth.login)
	api.POST("/auth/logout", auth.logout)
	api.POST("/auth/restore", auth.restore)
	api.POST("/auth/email/confirm", auth.confirmEmail)

	me := api.Group("/me", requireAuth())
	me.POST("/password", auth.changePassword)
	me.DELETE("", auth.deleteAccount)
	me.PUT("/email", auth.setEmail)
	me.DELETE("/email", auth.removeEmail)
	me.GET("/totp", auth.totpStatus)
	me.POST("/totp/setup", auth.totpSetup)
	me.POST("/totp/enable", auth.totpEnable)
	me.POST("/totp/disable", auth.totpDisable)
	me.POST("/totp/recovery-codes", auth.totpRenew)

	// Team panel: роли и права проверяются на сервере (requireCapability), а не по тому, что показывает интерфейс.
	team := &teamHandlers{svc: d.Accounts, log: d.Log}
	api.GET("/team/roles", requireCapability(accounts.CapTeamPanel), team.roles)
	manage := api.Group("/team/members", requireCapability(accounts.CapManageTeam))
	manage.GET("", team.members)
	manage.PUT("/:login/roles/:role", team.grant)
	manage.DELETE("/:login/roles/:role", team.revoke)

	// Документы в team panel. Что именно можно с каждым документом, решает сервис по человеку и документу;
	// здесь достаточно быть членом команды (Гражданину — 401, вошедшему без ролей — 403).
	tdocs := &teamDocumentHandlers{svc: d.Documents, log: d.Log}
	api.GET("/team/document-types", requireCapability(accounts.CapTeamPanel), tdocs.meta)
	api.GET("/team/dashboard", requireCapability(accounts.CapTeamPanel), tdocs.dashboard)
	api.GET("/team/glossary", requireCapability(accounts.CapTeamPanel), tdocs.glossary)
	api.POST("/team/glossary", requireCapability(accounts.CapTeamPanel), tdocs.createTerm)
	api.PUT("/team/glossary/:id", requireCapability(accounts.CapTeamPanel), tdocs.updateTerm)
	api.DELETE("/team/glossary/:id", requireCapability(accounts.CapTeamPanel), tdocs.deleteTerm)
	api.GET("/team/site", requireCapability(accounts.CapTeamPanel), tdocs.siteGet)
	api.PUT("/team/site", requireCapability(accounts.CapTeamPanel), tdocs.siteUpdate)
	api.GET("/team/timeline", requireCapability(accounts.CapTeamPanel), tdocs.timelineList)
	api.POST("/team/timeline", requireCapability(accounts.CapTeamPanel), tdocs.timelineCreate)
	api.PUT("/team/timeline/:id", requireCapability(accounts.CapTeamPanel), tdocs.timelineUpdate)
	api.DELETE("/team/timeline/:id", requireCapability(accounts.CapTeamPanel), tdocs.timelineDelete)
	api.GET("/team/templates", requireCapability(accounts.CapTeamPanel), tdocs.templates)
	api.POST("/team/templates", requireCapability(accounts.CapTeamPanel), tdocs.createTemplate)
	api.GET("/team/templates/:id", requireCapability(accounts.CapTeamPanel), tdocs.template)
	api.PUT("/team/templates/:id", requireCapability(accounts.CapTeamPanel), tdocs.updateTemplate)
	api.DELETE("/team/templates/:id", requireCapability(accounts.CapTeamPanel), tdocs.deleteTemplate)
	td := api.Group("/team/documents", requireCapability(accounts.CapTeamPanel))
	td.GET("", tdocs.list)
	td.POST("", tdocs.create)
	td.POST("/import", tdocs.importDocument)
	td.GET("/:id", tdocs.get)
	td.GET("/:id/export", tdocs.export)
	td.PUT("/:id", tdocs.save)
	td.PUT("/:id/draft", tdocs.autosave)
	td.POST("/:id/preview", tdocs.preview)
	td.GET("/:id/lint", tdocs.lint)
	td.GET("/:id/review", tdocs.review)
	td.POST("/:id/submit", tdocs.submit)
	td.POST("/:id/withdraw", tdocs.withdraw)
	td.POST("/:id/verdict", tdocs.verdict)
	td.POST("/:id/archive", tdocs.archive)
	td.POST("/:id/unarchive", tdocs.unarchive)
	td.POST("/:id/comments", tdocs.addComment)
	td.PUT("/:id/comments/:cid", tdocs.resolveComment)
	td.DELETE("/:id/comments/:cid", tdocs.deleteComment)
	td.POST("/:id/lock", tdocs.takeLock)
	td.DELETE("/:id/lock", tdocs.releaseLock)
	td.GET("/:id/versions", tdocs.versions)
	td.GET("/:id/versions/:vid", tdocs.version)
	td.GET("/:id/versions/:vid/diff", tdocs.diff)
	td.POST("/:id/versions/:vid/restore", tdocs.restore)

	docs := &documentHandlers{svc: d.Documents, log: d.Log}
	api.GET("/documents", docs.list)
	api.GET("/search", docs.search)
	api.GET("/timeline", docs.timeline)
	api.GET("/site", docs.site)
	api.GET("/graph/:ref", docs.graph)
	api.GET("/documents/summary", docs.summary)
	api.GET("/documents/recent", docs.recent)
	api.GET("/documents/:ref", docs.get)

	// «Пометки на полях» (шаг 5.2): читает кто угодно, пишет и жалуется только вошедший.
	api.GET("/documents/:ref/remarks", docs.listRemarks)
	api.POST("/documents/:ref/remarks", requireAuth(), docs.createRemark)
	api.POST("/remarks/:id/report", requireAuth(), docs.reportRemark)
	api.DELETE("/remarks/:id", requireAuth(), docs.deleteRemark)
	api.GET("/team/remarks/reported", requireCapability(accounts.CapModerateComments), docs.reportedRemarks)

	return r, nil
}
