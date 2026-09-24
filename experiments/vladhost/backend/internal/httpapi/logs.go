package httpapi

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"vladhost/internal/sites"
)

// logQuery разбирает параметры журнала: kind, status, q, limit, before (RFC 3339 с долями секунды).
func logQuery(c *gin.Context) (sites.LogQuery, bool) {
	q := sites.LogQuery{Kind: c.DefaultQuery("kind", "access"), Status: c.Query("status"), Text: strings.TrimSpace(c.Query("q"))}
	if v := c.Query("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			failErr(c, sites.ErrLogsBadQuery)
			return q, false
		}
		q.Limit = n
	}
	if v := c.Query("before"); v != "" {
		t, err := time.Parse(time.RFC3339Nano, v)
		if err != nil {
			failErr(c, sites.ErrLogsBadQuery)
			return q, false
		}
		q.Before = t
	}
	return q, true
}

func (s *Server) siteLogs(c *gin.Context) {
	id, ok := siteID(c)
	if !ok {
		return
	}
	q, ok := logQuery(c)
	if !ok {
		return
	}
	page, err := s.sites.Logs(c.Request.Context(), c.GetInt64("uid"), id, q)
	if err != nil {
		failErr(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, page)
}

func (s *Server) downloadSiteLogs(c *gin.Context) {
	id, ok := siteID(c)
	if !ok {
		return
	}
	kind := c.DefaultQuery("kind", "access")
	err := s.sites.WriteLogs(c.Request.Context(), c.GetInt64("uid"), id, kind, func() {
		c.Header("Content-Type", "text/plain; charset=utf-8")
		c.Header("Content-Disposition", `attachment; filename="site-`+strconv.FormatInt(id, 10)+`-`+kind+`.log"`)
		c.Header("Cache-Control", "no-store")
		c.Status(http.StatusOK)
	}, c.Writer)
	if err != nil && !c.Writer.Written() {
		failErr(c, err)
	}
}
