package httpapi

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"vladhost/internal/mailhost"
)

// WithMailHost подключает почту на своих доменах: домены, ящики, алиасы, проверку DNS.
func WithMailHost(svc *mailhost.Service) Option {
	return func(s *Server) { s.mailhost = svc }
}

func (s *Server) requireMailHost(c *gin.Context) {
	if !s.mailhost.Enabled() {
		fail(c, http.StatusNotFound, "not_found")
		return
	}
	c.Next()
}

func mailID(c *gin.Context, name string) (int64, bool) {
	id, err := strconv.ParseInt(c.Param(name), 10, 64)
	if err != nil || id <= 0 {
		failErr(c, mailhost.ErrNotFound)
		return 0, false
	}
	return id, true
}

func (s *Server) mailOverview(c *gin.Context) {
	uid := c.GetInt64("uid")
	info, err := s.mailhost.Info(c.Request.Context(), uid)
	if err != nil {
		failErr(c, err)
		return
	}
	list, err := s.mailhost.List(c.Request.Context(), uid)
	if err != nil {
		failErr(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"info": info, "domains": list})
}

func (s *Server) mailEnableDomain(c *gin.Context) {
	var in struct {
		Domain string `json:"domain"`
	}
	if c.ShouldBindJSON(&in) != nil {
		fail(c, http.StatusBadRequest, "bad_request")
		return
	}
	u, err := s.svc.UserByID(c.Request.Context(), c.GetInt64("uid"))
	if err != nil {
		failErr(c, err)
		return
	}
	d, err := s.mailhost.EnableDomain(c.Request.Context(), *u, in.Domain)
	if err != nil {
		failErr(c, err)
		return
	}
	setAuditTarget(c, d.Domain)
	c.JSON(http.StatusCreated, gin.H{"domain": d})
}

// mailVerifyDomain подтверждает владение доменом (подключён к сайту, есть подтверждённая зона у нас или в его DNS найдена TXT-запись с кодом).
func (s *Server) mailVerifyDomain(c *gin.Context) {
	id, ok := mailID(c, "id")
	if !ok {
		return
	}
	d, err := s.mailhost.Verify(c.Request.Context(), c.GetInt64("uid"), id)
	if err != nil {
		failErr(c, err)
		return
	}
	setAuditTarget(c, d.Domain)
	c.JSON(http.StatusOK, gin.H{"domain": d})
}

func (s *Server) mailSetDomain(c *gin.Context) {
	id, ok := mailID(c, "id")
	if !ok {
		return
	}
	var in struct {
		Enabled *bool `json:"enabled"`
	}
	if c.ShouldBindJSON(&in) != nil || in.Enabled == nil {
		fail(c, http.StatusBadRequest, "bad_request")
		return
	}
	d, err := s.mailhost.SetDomainEnabled(c.Request.Context(), c.GetInt64("uid"), id, *in.Enabled)
	if err != nil {
		failErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"domain": d})
}

func (s *Server) mailDeleteDomain(c *gin.Context) {
	id, ok := mailID(c, "id")
	if !ok {
		return
	}
	if err := s.mailhost.DeleteDomain(c.Request.Context(), c.GetInt64("uid"), id); err != nil {
		failErr(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (s *Server) mailDNS(c *gin.Context) {
	id, ok := mailID(c, "id")
	if !ok {
		return
	}
	res, err := s.mailhost.DNSInfo(c.Request.Context(), c.GetInt64("uid"), id)
	if err != nil {
		failErr(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, res)
}

// mailDNSAuto ставит MX, SPF, DKIM и DMARC в зону домена на наших серверах имён и сразу возвращает свежую проверку.
func (s *Server) mailDNSAuto(c *gin.Context) {
	id, ok := mailID(c, "id")
	if !ok {
		return
	}
	uid := c.GetInt64("uid")
	if err := s.mailhost.AutoConfigure(c.Request.Context(), uid, id); err != nil {
		failErr(c, err)
		return
	}
	res, err := s.mailhost.DNSInfo(c.Request.Context(), uid, id)
	if err != nil {
		failErr(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, res)
}

func (s *Server) mailLog(c *gin.Context) {
	id, ok := mailID(c, "id")
	if !ok {
		return
	}
	j, err := s.mailhost.Log(c.Request.Context(), c.GetInt64("uid"), id)
	if err != nil {
		failErr(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, j)
}

// Пароль сгенерированного ящика уходит в ответе один раз и нигде не хранится.
func (s *Server) mailCreateMailbox(c *gin.Context) {
	id, ok := mailID(c, "id")
	if !ok {
		return
	}
	var in struct {
		Local    string `json:"local"`
		Password string `json:"password"`
		QuotaMB  int    `json:"quota_mb"`
	}
	if c.ShouldBindJSON(&in) != nil {
		fail(c, http.StatusBadRequest, "bad_request")
		return
	}
	box, pw, err := s.mailhost.CreateMailbox(c.Request.Context(), c.GetInt64("uid"), id, in.Local, in.Password, in.QuotaMB)
	if err != nil {
		failErr(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusCreated, gin.H{"mailbox": box, "password": pw})
}

func (s *Server) mailUpdateMailbox(c *gin.Context) {
	id, ok := mailID(c, "id")
	if !ok {
		return
	}
	var in struct {
		QuotaMB *int  `json:"quota_mb"`
		Enabled *bool `json:"enabled"`
	}
	if c.ShouldBindJSON(&in) != nil {
		fail(c, http.StatusBadRequest, "bad_request")
		return
	}
	box, err := s.mailhost.UpdateMailbox(c.Request.Context(), c.GetInt64("uid"), id, in.QuotaMB, in.Enabled)
	if err != nil {
		failErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"mailbox": box})
}

func (s *Server) mailSetRules(c *gin.Context) {
	id, ok := mailID(c, "id")
	if !ok {
		return
	}
	var in struct {
		AutoReply mailhost.AutoReply `json:"autoreply"`
		Forward   mailhost.Forward   `json:"forward"`
	}
	if c.ShouldBindJSON(&in) != nil {
		fail(c, http.StatusBadRequest, "bad_request")
		return
	}
	box, err := s.mailhost.SetMailboxRules(c.Request.Context(), c.GetInt64("uid"), id, in.AutoReply, in.Forward)
	if err != nil {
		failErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"mailbox": box})
}

func (s *Server) mailSetPassword(c *gin.Context) {
	id, ok := mailID(c, "id")
	if !ok {
		return
	}
	var in struct {
		Password string `json:"password"`
	}
	if c.ShouldBindJSON(&in) != nil {
		fail(c, http.StatusBadRequest, "bad_request")
		return
	}
	pw, err := s.mailhost.SetMailboxPassword(c.Request.Context(), c.GetInt64("uid"), id, in.Password)
	if err != nil {
		failErr(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"password": pw})
}

func (s *Server) mailDeleteMailbox(c *gin.Context) {
	id, ok := mailID(c, "id")
	if !ok {
		return
	}
	if err := s.mailhost.DeleteMailbox(c.Request.Context(), c.GetInt64("uid"), id); err != nil {
		failErr(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (s *Server) mailSetAlias(c *gin.Context) {
	id, ok := mailID(c, "id")
	if !ok {
		return
	}
	var in struct {
		Local string   `json:"local"`
		To    []string `json:"to"`
	}
	if c.ShouldBindJSON(&in) != nil {
		fail(c, http.StatusBadRequest, "bad_request")
		return
	}
	a, err := s.mailhost.SetAlias(c.Request.Context(), c.GetInt64("uid"), id, in.Local, in.To)
	if err != nil {
		failErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"alias": a})
}

func (s *Server) mailDeleteAlias(c *gin.Context) {
	id, ok := mailID(c, "id")
	if !ok {
		return
	}
	if err := s.mailhost.DeleteAlias(c.Request.Context(), c.GetInt64("uid"), id); err != nil {
		failErr(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}
