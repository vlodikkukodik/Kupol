// Package httpapi: JSON API панели (/api/...). Публичного API для внешних клиентов нет.
package httpapi

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"vladhost/internal/auth"
	"vladhost/internal/config"
	"vladhost/internal/sites"
)

type Server struct {
	svc   *auth.Service
	sites *sites.Service
	cfg   config.Config

	// На бою (Secure) cookie с префиксом __Host-: браузер не даст сайту на соседнем поддомене
	// подбросить свою cookie с этим именем на весь домен. Такой префикс требует Path=/.
	cookieName, cookiePath string
}

func New(svc *auth.Service, sitesSvc *sites.Service, cfg config.Config) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	s := &Server{svc: svc, sites: sitesSvc, cfg: cfg, cookieName: "vh_refresh", cookiePath: "/api/auth"}
	if cfg.CookieSecure {
		s.cookieName, s.cookiePath = "__Host-vh_refresh", "/"
	}
	r := gin.New()
	r.Use(gin.Recovery())
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
	limited.POST("/refresh", s.refresh)
	api.POST("/auth/logout", s.checkOrigin, s.logout)

	authed := api.Group("", s.requireAuth)
	authed.GET("/me", s.me)
	authed.GET("/sites", s.listSites)
	authed.POST("/sites", s.createSite)
	authed.DELETE("/sites/:id", s.deleteSite)
	authed.POST("/sites/:id/deploy", s.deploySite)
	authed.POST("/sites/:id/cert/retry", s.retryCert)
	authed.POST("/sites/:id/ftp", s.enableFTP)
	authed.DELETE("/sites/:id/ftp", s.disableFTP)
	authed.GET("/sites/:id/files", s.listFiles)
	authed.DELETE("/sites/:id/files", s.deleteFile)
	authed.POST("/sites/:id/files/mkdir", s.mkdir)
	authed.POST("/sites/:id/files/rename", s.renameFile)
	authed.POST("/sites/:id/files/upload", s.uploadFile)
	authed.GET("/sites/:id/file", s.readFile)
	authed.PUT("/sites/:id/file", s.saveFile)

	admin := authed.Group("", s.requireAdmin)
	admin.GET("/invites", s.listInvites)
	admin.POST("/invites", s.createInvite)
	return r
}

type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Field   string `json:"field,omitempty"`
}

func fail(c *gin.Context, status int, code, msg string) {
	c.AbortWithStatusJSON(status, gin.H{"error": apiError{Code: code, Message: msg}})
}

// failErr переводит доменную ошибку в HTTP-ответ; неизвестные ошибки не раскрываются клиенту.
func failErr(c *gin.Context, err error) {
	var ve *auth.ValidationError
	switch {
	case errors.As(err, &ve):
		c.AbortWithStatusJSON(http.StatusUnprocessableEntity,
			gin.H{"error": apiError{Code: "validation", Message: ve.Message, Field: ve.Field}})
	case errors.Is(err, auth.ErrInvalidInvite):
		fail(c, http.StatusUnprocessableEntity, "invalid_invite", err.Error())
	case errors.Is(err, auth.ErrEmailTaken):
		c.AbortWithStatusJSON(http.StatusConflict,
			gin.H{"error": apiError{Code: "email_taken", Message: err.Error(), Field: "email"}})
	case errors.Is(err, auth.ErrUsernameTaken):
		c.AbortWithStatusJSON(http.StatusConflict,
			gin.H{"error": apiError{Code: "username_taken", Message: err.Error(), Field: "username"}})
	case errors.Is(err, auth.ErrInvalidCredentials):
		fail(c, http.StatusUnauthorized, "invalid_credentials", err.Error())
	case errors.Is(err, auth.ErrInvalidToken):
		fail(c, http.StatusUnauthorized, "unauthorized", err.Error())
	case errors.Is(err, sites.ErrLimit):
		fail(c, http.StatusForbidden, "site_limit", err.Error())
	case errors.Is(err, sites.ErrSlugTaken):
		c.AbortWithStatusJSON(http.StatusConflict,
			gin.H{"error": apiError{Code: "slug_taken", Message: err.Error(), Field: "slug"}})
	case errors.Is(err, sites.ErrNotFound):
		fail(c, http.StatusNotFound, "not_found", err.Error())
	case errors.Is(err, sites.ErrFileNotFound):
		fail(c, http.StatusNotFound, "file_not_found", err.Error())
	case errors.Is(err, sites.ErrBadPath):
		fail(c, http.StatusUnprocessableEntity, "bad_path", err.Error())
	case errors.Is(err, sites.ErrExists):
		fail(c, http.StatusConflict, "exists", err.Error())
	case errors.Is(err, sites.ErrIsDir), errors.Is(err, sites.ErrNotDir):
		fail(c, http.StatusUnprocessableEntity, "wrong_type", err.Error())
	case errors.Is(err, sites.ErrTooLarge):
		fail(c, http.StatusRequestEntityTooLarge, "too_large", err.Error())
	case errors.Is(err, sites.ErrNotText):
		fail(c, http.StatusUnsupportedMediaType, "not_text", err.Error())
	case errors.Is(err, sites.ErrCertState):
		fail(c, http.StatusConflict, "cert_state", err.Error())
	case errors.Is(err, sites.ErrQuota):
		fail(c, http.StatusRequestEntityTooLarge, "quota_exceeded", err.Error())
	case sites.IsBadArchive(err):
		fail(c, http.StatusUnprocessableEntity, "bad_archive", err.Error())
	default:
		_ = c.Error(err)
		fail(c, http.StatusInternalServerError, "internal", "Внутренняя ошибка")
	}
}

func (s *Server) setRefreshCookie(c *gin.Context, value string, maxAge int) {
	c.SetSameSite(http.SameSiteStrictMode)
	c.SetCookie(s.cookieName, value, maxAge, s.cookiePath, "", s.cfg.CookieSecure, true)
}

func (s *Server) respondSession(c *gin.Context, sess *auth.Session) {
	s.setRefreshCookie(c, sess.RefreshToken, int(time.Until(sess.RefreshExpires).Seconds()))
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{
		"access_token": sess.AccessToken,
		"expires_in":   sess.ExpiresIn,
		"user":         sess.User,
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
		fail(c, http.StatusBadRequest, "bad_request", "Некорректный запрос")
		return
	}
	sess, err := s.svc.Register(c.Request.Context(), auth.RegisterInput{
		Invite: in.Invite, Email: in.Email, Username: in.Username, Password: in.Password,
	})
	if err != nil {
		failErr(c, err)
		return
	}
	s.respondSession(c, sess)
}

func (s *Server) login(c *gin.Context) {
	var in struct {
		Login    string `json:"login"`
		Password string `json:"password"`
	}
	if c.ShouldBindJSON(&in) != nil || in.Login == "" || in.Password == "" {
		fail(c, http.StatusBadRequest, "bad_request", "Некорректный запрос")
		return
	}
	sess, err := s.svc.Login(c.Request.Context(), in.Login, in.Password)
	if err != nil {
		failErr(c, err)
		return
	}
	s.respondSession(c, sess)
}

func (s *Server) refresh(c *gin.Context) {
	raw, err := c.Cookie(s.cookieName)
	if err != nil || raw == "" {
		fail(c, http.StatusUnauthorized, "unauthorized", auth.ErrInvalidToken.Error())
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
		if err := s.svc.Logout(c.Request.Context(), raw); err != nil {
			failErr(c, err)
			return
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
	c.JSON(http.StatusOK, gin.H{"user": u})
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
		fail(c, http.StatusUnprocessableEntity, "validation",
			"Срок инвайта: от 1 до "+strconv.Itoa(24*30)+" часов")
		return
	}
	inv, err := s.svc.CreateInvite(c.Request.Context(), c.GetInt64("uid"), time.Duration(in.TTLHours)*time.Hour)
	if err != nil {
		failErr(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"invite": inv})
}
