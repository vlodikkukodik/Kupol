package httpapi

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"vladhost/internal/sites"
)

func (s *Server) listBackups(c *gin.Context) {
	id, ok := siteID(c)
	if !ok {
		return
	}
	list, err := s.sites.ListBackups(c.Request.Context(), c.GetInt64("uid"), id)
	if err != nil {
		failErr(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"backups": list})
}

func (s *Server) createBackup(c *gin.Context) {
	id, ok := siteID(c)
	if !ok {
		return
	}
	b, err := s.sites.CreateBackup(c.Request.Context(), c.GetInt64("uid"), id)
	if err != nil {
		failErr(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"backup": b})
}

func (s *Server) restoreBackup(c *gin.Context) {
	id, ok := siteID(c)
	if !ok {
		return
	}
	bid, ok := backupID(c)
	if !ok {
		return
	}
	site, err := s.sites.RestoreBackup(c.Request.Context(), c.GetInt64("uid"), id, bid)
	if err != nil {
		failErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"site": s.toJSON(*site)})
}

func (s *Server) downloadBackup(c *gin.Context) {
	id, ok := siteID(c)
	if !ok {
		return
	}
	bid, ok := backupID(c)
	if !ok {
		return
	}
	err := s.sites.WriteBackupZip(c.Request.Context(), c.GetInt64("uid"), id, bid, func() {
		c.Header("Content-Type", "application/zip")
		c.Header("Content-Disposition", `attachment; filename="site-`+strconv.FormatInt(id, 10)+`-backup-`+strconv.FormatInt(bid, 10)+`.zip"`)
		c.Header("Cache-Control", "no-store")
		c.Status(http.StatusOK)
	}, c.Writer)
	if err != nil && !c.Writer.Written() {
		failErr(c, err)
	}
}

func (s *Server) downloadArchive(c *gin.Context) {
	id, ok := siteID(c)
	if !ok {
		return
	}
	err := s.sites.WriteSiteArchive(c.Request.Context(), c.GetInt64("uid"), id, func() {
		c.Header("Content-Type", "application/zip")
		c.Header("Content-Disposition", `attachment; filename="site-`+strconv.FormatInt(id, 10)+`.zip"`)
		c.Header("Cache-Control", "no-store")
		c.Status(http.StatusOK)
	}, c.Writer)
	if err != nil && !c.Writer.Written() {
		failErr(c, err)
	}
}

func backupID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("bid"), 10, 64)
	if err != nil || id <= 0 {
		failErr(c, sites.ErrBackupNotFound)
		return 0, false
	}
	return id, true
}
