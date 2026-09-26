package httpapi

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"vladhost/internal/cronjobs"
)

// WithCron подключает раздел «Планировщик».
func WithCron(c *cronjobs.Service) Option { return func(s *Server) { s.cron = c } }

// requireCron отвечает 404, когда планировщик не подключён.
func (s *Server) requireCron(c *gin.Context) {
	if s.cron == nil {
		fail(c, http.StatusNotFound, "not_found")
		return
	}
	c.Next()
}

func cronID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		failErr(c, cronjobs.ErrNotFound)
		return 0, false
	}
	return id, true
}

// cronBody — поля задачи в запросе.
type cronBody struct {
	Name     string `json:"name"`
	Kind     string `json:"kind"`
	Schedule string `json:"schedule"`
	URL      string `json:"url"`
	Command  string `json:"command"`
	SiteID   int64  `json:"site_id"`
	Enabled  bool   `json:"enabled"`
}

func (b cronBody) input() cronjobs.Input {
	return cronjobs.Input{Name: b.Name, Kind: b.Kind, Schedule: b.Schedule, URL: b.URL, Command: b.Command, SiteID: b.SiteID, Enabled: b.Enabled}
}

func (s *Server) listCron(c *gin.Context) {
	jobs, err := s.cron.List(c.Request.Context(), c.GetInt64("uid"))
	if err != nil {
		failErr(c, err)
		return
	}
	if jobs == nil {
		jobs = []cronjobs.Job{}
	}
	c.JSON(http.StatusOK, gin.H{"jobs": jobs, "info": s.cron.Info()})
}

func (s *Server) createCron(c *gin.Context) {
	var in cronBody
	if c.ShouldBindJSON(&in) != nil {
		fail(c, http.StatusBadRequest, "bad_request")
		return
	}
	j, err := s.cron.Create(c.Request.Context(), c.GetInt64("uid"), in.input())
	if err != nil {
		failErr(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"job": j})
}

func (s *Server) updateCron(c *gin.Context) {
	id, ok := cronID(c)
	if !ok {
		return
	}
	var in cronBody
	if c.ShouldBindJSON(&in) != nil {
		fail(c, http.StatusBadRequest, "bad_request")
		return
	}
	j, err := s.cron.Update(c.Request.Context(), c.GetInt64("uid"), id, in.input())
	if err != nil {
		failErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"job": j})
}

func (s *Server) deleteCron(c *gin.Context) {
	id, ok := cronID(c)
	if !ok {
		return
	}
	if err := s.cron.Delete(c.Request.Context(), c.GetInt64("uid"), id); err != nil {
		failErr(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (s *Server) runCron(c *gin.Context) {
	id, ok := cronID(c)
	if !ok {
		return
	}
	run, err := s.cron.RunNow(c.Request.Context(), c.GetInt64("uid"), id)
	if err != nil {
		failErr(c, err)
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"run": run})
}

func (s *Server) cronRuns(c *gin.Context) {
	id, ok := cronID(c)
	if !ok {
		return
	}
	runs, err := s.cron.Runs(c.Request.Context(), c.GetInt64("uid"), id)
	if err != nil {
		failErr(c, err)
		return
	}
	if runs == nil {
		runs = []cronjobs.Run{}
	}
	c.JSON(http.StatusOK, gin.H{"runs": runs})
}
