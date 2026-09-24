package httpapi

import (
	"errors"
	"net"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"vladhost/internal/sites"
)

type siteJSON struct {
	sites.Site
	URL     string       `json:"url"`
	FTP     ftpBlock     `json:"ftp"`
	Domains []domainJSON `json:"domains"`
}

// ftpBlock — сведения для подключения по FTP. Пароль сюда не попадает: он показывается один раз при выдаче.
type ftpBlock struct {
	Available bool   `json:"available"` // на сервере включён FTP
	Enabled   bool   `json:"enabled"`   // у сайта выдан доступ
	Host      string `json:"host,omitempty"`
	Port      int    `json:"port,omitempty"`
	Username  string `json:"username,omitempty"`
	// AllowPlain — сервер принимает и обычный FTP без шифрования (интерфейс тогда предупреждает и рекомендует FTPS).
	AllowPlain bool `json:"allow_plain"`
}

func (s *Server) toJSON(st sites.Site) siteJSON { return s.toJSONWith(st, nil) }

func (s *Server) toJSONWith(st sites.Site, domains []sites.Domain) siteJSON {
	out := siteJSON{Site: st, URL: "https://" + st.Host, Domains: toDomainsJSON(domains)}
	if s.cfg.FTP.Addr != "" {
		_, port, _ := net.SplitHostPort(s.cfg.FTP.Addr)
		p, _ := strconv.Atoi(port)
		out.FTP = ftpBlock{Available: true, Enabled: st.FTPEnabled, Host: s.cfg.FTP.Host, Port: p, Username: s.sites.FTPUsername(st.Host), AllowPlain: s.cfg.FTP.AllowPlain}
	}
	return out
}

func (s *Server) listSites(c *gin.Context) {
	list, err := s.sites.List(c.Request.Context(), c.GetInt64("uid"))
	if err != nil {
		failErr(c, err)
		return
	}
	domains, err := s.sites.DomainsByUser(c.Request.Context(), c.GetInt64("uid"))
	if err != nil {
		failErr(c, err)
		return
	}
	out := make([]siteJSON, 0, len(list))
	for _, st := range list {
		out = append(out, s.toJSONWith(st, domains[st.ID]))
	}
	lim := s.sites.Limits()
	c.JSON(http.StatusOK, gin.H{
		"sites":         out,
		"limits":        gin.H{"max_sites": lim.MaxSites, "disk_quota_bytes": lim.DiskQuotaBytes},
		"domain_config": s.domainConfig(),
	})
}

func (s *Server) createSite(c *gin.Context) {
	var in struct {
		Slug string `json:"slug"`
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
	site, err := s.sites.Create(c.Request.Context(), *u, in.Slug)
	if err != nil {
		failErr(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"site": s.toJSON(*site)})
}

func (s *Server) retryCert(c *gin.Context) {
	id, ok := siteID(c)
	if !ok {
		return
	}
	site, err := s.sites.RetryCert(c.Request.Context(), c.GetInt64("uid"), id)
	if err != nil {
		failErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"site": s.toJSON(*site)})
}

// enableFTP выдаёт FTP-доступ к сайту или меняет его пароль. Пароль есть только в этом ответе.
func (s *Server) enableFTP(c *gin.Context) {
	id, ok := siteID(c)
	if !ok {
		return
	}
	if s.cfg.FTP.Addr == "" {
		fail(c, http.StatusConflict, "ftp_unavailable")
		return
	}
	site, password, err := s.sites.EnableFTP(c.Request.Context(), c.GetInt64("uid"), id)
	if err != nil {
		failErr(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"site": s.toJSON(*site), "password": password})
}

func (s *Server) disableFTP(c *gin.Context) {
	id, ok := siteID(c)
	if !ok {
		return
	}
	site, err := s.sites.DisableFTP(c.Request.Context(), c.GetInt64("uid"), id)
	if err != nil {
		failErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"site": s.toJSON(*site)})
}

func (s *Server) deleteSite(c *gin.Context) {
	id, ok := siteID(c)
	if !ok {
		return
	}
	if err := s.sites.Delete(c.Request.Context(), c.GetInt64("uid"), id); err != nil {
		failErr(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (s *Server) deploySite(c *gin.Context) {
	id, ok := siteID(c)
	if !ok {
		return
	}
	// Сжатый архив не может быть больше квоты на диск с запасом на заголовки multipart.
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, s.sites.Limits().DiskQuotaBytes+(1<<20))
	fh, err := c.FormFile("file")
	if err != nil {
		if _, tooBig := errors.AsType[*http.MaxBytesError](err); tooBig {
			failErr(c, sites.ErrQuota)
			return
		}
		fail(c, http.StatusBadRequest, "bad_request")
		return
	}
	f, err := fh.Open()
	if err != nil {
		failErr(c, err)
		return
	}
	defer func() { _ = f.Close() }()
	site, err := s.sites.Deploy(c.Request.Context(), c.GetInt64("uid"), id, f, fh.Size)
	if err != nil {
		failErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"site": s.toJSON(*site)})
}

func siteID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		failErr(c, sites.ErrNotFound)
		return 0, false
	}
	return id, true
}
