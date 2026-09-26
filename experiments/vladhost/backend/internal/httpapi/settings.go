package httpapi

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"vladhost/internal/sitecfg"
)

// settingsReply — настройки сайта и справочные значения для формы.
func settingsReply(set sitecfg.Settings) gin.H {
	if set.Index == nil {
		set.Index = []string{} // в JSON — пустой список, а не null
	}
	if set.ErrorPages == nil {
		set.ErrorPages = map[string]string{}
	}
	return gin.H{
		"settings":       set,
		"default_index":  sitecfg.DefaultIndex,
		"error_statuses": sitecfg.ErrorStatuses,
		"max_index":      sitecfg.MaxIndex,
	}
}

func (s *Server) getSiteSettings(c *gin.Context) {
	id, ok := siteID(c)
	if !ok {
		return
	}
	set, err := s.sites.Settings(c.Request.Context(), c.GetInt64("uid"), id)
	if err != nil {
		failErr(c, err)
		return
	}
	c.JSON(http.StatusOK, settingsReply(set))
}

func (s *Server) putSiteSettings(c *gin.Context) {
	id, ok := siteID(c)
	if !ok {
		return
	}
	var in sitecfg.Settings
	if c.ShouldBindJSON(&in) != nil {
		fail(c, http.StatusBadRequest, "bad_request")
		return
	}
	set, err := s.sites.UpdateSettings(c.Request.Context(), c.GetInt64("uid"), id, in)
	if err != nil {
		failErr(c, err)
		return
	}
	c.JSON(http.StatusOK, settingsReply(set))
}
