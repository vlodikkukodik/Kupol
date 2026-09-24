package httpapi

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"vladhost/internal/sites"
)

// domainJSON — домен для интерфейса. found — IP, которые нашлись в DNS (для подсказки, что исправить).
type domainJSON struct {
	sites.Domain
	Found []string `json:"found"`
}

func toDomainJSON(d sites.Domain) domainJSON { return domainJSON{Domain: d, Found: d.FoundIPs()} }

func toDomainsJSON(list []sites.Domain) []domainJSON {
	out := make([]domainJSON, 0, len(list))
	for _, d := range list {
		out = append(out, toDomainJSON(d))
	}
	return out
}

// domainConfig — что нужно интерфейсу, чтобы показать инструкцию: на какие IP направлять A-запись.
func (s *Server) domainConfig() gin.H {
	ips, perSite := s.sites.DomainInfo()
	return gin.H{"available": s.sites.DomainsEnabled(), "server_ips": ips, "per_site": perSite}
}

func domainID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("did"), 10, 64)
	if err != nil || id <= 0 {
		failErr(c, sites.ErrDomainNotFound)
		return 0, false
	}
	return id, true
}

func (s *Server) addDomain(c *gin.Context) {
	id, ok := siteID(c)
	if !ok {
		return
	}
	var in struct {
		Host string `json:"host"`
	}
	if c.ShouldBindJSON(&in) != nil {
		fail(c, http.StatusBadRequest, "bad_request")
		return
	}
	d, err := s.sites.AddDomain(c.Request.Context(), c.GetInt64("uid"), id, in.Host)
	if err != nil {
		failErr(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"domain": toDomainJSON(*d)})
}

func (s *Server) checkDomain(c *gin.Context) {
	id, ok := siteID(c)
	if !ok {
		return
	}
	did, ok := domainID(c)
	if !ok {
		return
	}
	d, err := s.sites.CheckDomain(c.Request.Context(), c.GetInt64("uid"), id, did)
	if err != nil {
		failErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"domain": toDomainJSON(*d)})
}

func (s *Server) removeDomain(c *gin.Context) {
	id, ok := siteID(c)
	if !ok {
		return
	}
	did, ok := domainID(c)
	if !ok {
		return
	}
	if err := s.sites.RemoveDomain(c.Request.Context(), c.GetInt64("uid"), id, did); err != nil {
		failErr(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}
