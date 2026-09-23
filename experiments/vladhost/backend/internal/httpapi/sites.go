package httpapi

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"vladhost/internal/sites"
)

type siteJSON struct {
	sites.Site
	URL string `json:"url"`
}

func toJSON(s sites.Site) siteJSON { return siteJSON{Site: s, URL: "https://" + s.Host} }

func (s *Server) listSites(c *gin.Context) {
	list, err := s.sites.List(c.Request.Context(), c.GetInt64("uid"))
	if err != nil {
		failErr(c, err)
		return
	}
	out := make([]siteJSON, 0, len(list))
	for _, st := range list {
		out = append(out, toJSON(st))
	}
	lim := s.sites.Limits()
	c.JSON(http.StatusOK, gin.H{
		"sites":  out,
		"limits": gin.H{"max_sites": lim.MaxSites, "disk_quota_bytes": lim.DiskQuotaBytes},
	})
}

func (s *Server) createSite(c *gin.Context) {
	var in struct {
		Slug string `json:"slug"`
	}
	if c.ShouldBindJSON(&in) != nil {
		fail(c, http.StatusBadRequest, "bad_request", "Некорректный запрос")
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
	c.JSON(http.StatusCreated, gin.H{"site": toJSON(*site)})
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
	c.JSON(http.StatusOK, gin.H{"site": toJSON(*site)})
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
			fail(c, http.StatusRequestEntityTooLarge, "quota_exceeded", sites.ErrQuota.Error())
			return
		}
		fail(c, http.StatusBadRequest, "bad_request", "Приложите zip-архив в поле file")
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
	c.JSON(http.StatusOK, gin.H{"site": toJSON(*site)})
}

func siteID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		fail(c, http.StatusNotFound, "not_found", sites.ErrNotFound.Error())
		return 0, false
	}
	return id, true
}
