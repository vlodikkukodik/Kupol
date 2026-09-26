package httpapi

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"vladhost/internal/sites"
)

func (s *Server) siteStats(c *gin.Context) {
	id, ok := siteID(c)
	if !ok {
		return
	}
	days, err := strconv.Atoi(c.DefaultQuery("days", "30"))
	if err != nil {
		failErr(c, sites.ErrStatsBadPeriod)
		return
	}
	sum, err := s.sites.Stats(c.Request.Context(), c.GetInt64("uid"), id, days)
	if err != nil {
		failErr(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, sum)
}
