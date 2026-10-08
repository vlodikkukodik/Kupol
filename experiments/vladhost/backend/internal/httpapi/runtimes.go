package httpapi

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"vladhost/internal/runtimes"
)

// WithRuntimes подключает раздел «Среда выполнения» сайта (PHP, Node.js, Python).
func WithRuntimes(r *runtimes.Service) Option { return func(s *Server) { s.rt = r } }

// requireRuntimes отвечает 404, пока на сервере не установлена ни одна среда.
func (s *Server) requireRuntimes(c *gin.Context) {
	if !s.rt.Enabled() {
		fail(c, http.StatusNotFound, "not_found")
		return
	}
	c.Next()
}

func (s *Server) getRuntime(c *gin.Context) {
	id, ok := siteID(c)
	if !ok {
		return
	}
	v, err := s.rt.Get(c.Request.Context(), c.GetInt64("uid"), id)
	if err != nil {
		failErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"runtime": v})
}

func (s *Server) setRuntime(c *gin.Context) {
	id, ok := siteID(c)
	if !ok {
		return
	}
	var in struct {
		Runtime string `json:"runtime"`
		Version string `json:"version"`
		Command string `json:"command"`
	}
	if c.ShouldBindJSON(&in) != nil {
		fail(c, http.StatusBadRequest, "bad_request")
		return
	}
	v, err := s.rt.Set(c.Request.Context(), c.GetInt64("uid"), id, runtimes.Input{Runtime: in.Runtime, Version: in.Version, Command: in.Command})
	if err != nil {
		failErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"runtime": v})
}

func (s *Server) restartRuntime(c *gin.Context) {
	id, ok := siteID(c)
	if !ok {
		return
	}
	v, err := s.rt.Restart(c.Request.Context(), c.GetInt64("uid"), id)
	if err != nil {
		failErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"runtime": v})
}

func (s *Server) runtimeLogs(c *gin.Context) {
	id, ok := siteID(c)
	if !ok {
		return
	}
	out, err := s.rt.Logs(c.Request.Context(), c.GetInt64("uid"), id)
	if err != nil {
		failErr(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"logs": out})
}
