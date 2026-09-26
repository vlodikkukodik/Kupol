package httpapi

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"vladhost/internal/dnszones"
)

// WithDNS подключает собственный DNS: зоны своих доменов, записи и проверку делегирования.
func WithDNS(svc *dnszones.Service) Option {
	return func(s *Server) { s.dns = svc }
}

func (s *Server) requireDNS(c *gin.Context) {
	if !s.dns.Enabled() {
		fail(c, http.StatusNotFound, "not_found")
		return
	}
	c.Next()
}

func dnsID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		failErr(c, dnszones.ErrNotFound)
		return 0, false
	}
	return id, true
}

func (s *Server) dnsOverview(c *gin.Context) {
	uid := c.GetInt64("uid")
	info, err := s.dns.Info(c.Request.Context(), uid)
	if err != nil {
		failErr(c, err)
		return
	}
	zones, err := s.dns.List(c.Request.Context(), uid)
	if err != nil {
		failErr(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"info": info, "zones": zones})
}

func (s *Server) dnsEnableZone(c *gin.Context) {
	var in struct {
		Domain string `json:"domain"`
	}
	if c.ShouldBindJSON(&in) != nil {
		fail(c, http.StatusBadRequest, "bad_request")
		return
	}
	z, err := s.dns.EnableZone(c.Request.Context(), c.GetInt64("uid"), in.Domain)
	if err != nil {
		failErr(c, err)
		return
	}
	setAuditTarget(c, z.Domain)
	c.JSON(http.StatusCreated, gin.H{"zone": z})
}

// dnsVerifyZone подтверждает владение доменом (домен подключён к сайту или в его DNS найдена TXT-запись с кодом).
func (s *Server) dnsVerifyZone(c *gin.Context) {
	id, ok := dnsID(c)
	if !ok {
		return
	}
	z, err := s.dns.Verify(c.Request.Context(), c.GetInt64("uid"), id)
	if err != nil {
		failErr(c, err)
		return
	}
	setAuditTarget(c, z.Domain)
	c.JSON(http.StatusOK, gin.H{"zone": z})
}

func (s *Server) dnsDeleteZone(c *gin.Context) {
	id, ok := dnsID(c)
	if !ok {
		return
	}
	if err := s.dns.DeleteZone(c.Request.Context(), c.GetInt64("uid"), id); err != nil {
		failErr(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (s *Server) dnsDelegation(c *gin.Context) {
	id, ok := dnsID(c)
	if !ok {
		return
	}
	d, err := s.dns.Delegation(c.Request.Context(), c.GetInt64("uid"), id)
	if err != nil {
		failErr(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"delegation": d})
}

func (s *Server) dnsAddRecord(c *gin.Context) {
	id, ok := dnsID(c)
	if !ok {
		return
	}
	var in dnszones.RecordInput
	if c.ShouldBindJSON(&in) != nil {
		fail(c, http.StatusBadRequest, "bad_request")
		return
	}
	r, err := s.dns.SaveRecord(c.Request.Context(), c.GetInt64("uid"), id, 0, in)
	if err != nil {
		failErr(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"record": r})
}

func (s *Server) dnsUpdateRecord(c *gin.Context) {
	id, ok := dnsID(c)
	if !ok {
		return
	}
	var in dnszones.RecordInput
	if c.ShouldBindJSON(&in) != nil {
		fail(c, http.StatusBadRequest, "bad_request")
		return
	}
	uid := c.GetInt64("uid")
	zoneID, err := s.dns.ZoneOfRecord(c.Request.Context(), uid, id)
	if err != nil {
		failErr(c, err)
		return
	}
	r, err := s.dns.SaveRecord(c.Request.Context(), uid, zoneID, id, in)
	if err != nil {
		failErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"record": r})
}

func (s *Server) dnsDeleteRecord(c *gin.Context) {
	id, ok := dnsID(c)
	if !ok {
		return
	}
	if err := s.dns.DeleteRecord(c.Request.Context(), c.GetInt64("uid"), id); err != nil {
		failErr(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}
