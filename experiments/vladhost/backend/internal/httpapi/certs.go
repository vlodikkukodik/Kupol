package httpapi

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// renewCert перевыпускает сертификат адреса сайта или одного из его доменов заново.
func (s *Server) renewCert(c *gin.Context) {
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
	if err := s.sites.RenewCert(c.Request.Context(), c.GetInt64("uid"), id, in.Host); err != nil {
		failErr(c, err)
		return
	}
	c.Status(http.StatusAccepted)
}
