package httpapi

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"vladhost/internal/auth"
	support "vladhost/internal/tickets"
)

// WithTickets подключает обращения в поддержку.
func WithTickets(svc *support.Service) Option {
	return func(s *Server) { s.support = svc }
}

func (s *Server) requireSupport(c *gin.Context) {
	if s.support == nil {
		fail(c, http.StatusNotFound, "not_found")
		return
	}
	c.Next()
}

// viewer — вошедший пользователь целиком (роль нужна, чтобы отличить поддержку от владельца тикета).
func (s *Server) viewer(c *gin.Context) (auth.User, bool) {
	u, err := s.svc.UserByID(c.Request.Context(), c.GetInt64("uid"))
	if err != nil {
		failErr(c, err)
		return auth.User{}, false
	}
	return *u, true
}

func ticketID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		failErr(c, support.ErrNotFound)
		return 0, false
	}
	return id, true
}

func (s *Server) listTickets(c *gin.Context) {
	list, err := s.support.List(c.Request.Context(), c.GetInt64("uid"))
	if err != nil {
		failErr(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"tickets": list, "categories": support.Categories, "max_open": support.MaxOpenPerUser, "max_subject": support.MaxSubject, "max_body": support.MaxBody})
}

func (s *Server) adminTickets(c *gin.Context) {
	u, ok := s.viewer(c)
	if !ok {
		return
	}
	list, err := s.support.AdminList(c.Request.Context(), u, c.Query("status"), c.Query("q"))
	if err != nil {
		failErr(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"tickets": list})
}

func (s *Server) ticketsSummary(c *gin.Context) {
	u, ok := s.viewer(c)
	if !ok {
		return
	}
	n, err := s.support.Waiting(c.Request.Context(), u)
	if err != nil {
		failErr(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"waiting": n})
}

func (s *Server) createTicket(c *gin.Context) {
	var in struct {
		Subject  string `json:"subject"`
		Category string `json:"category"`
		Message  string `json:"message"`
	}
	if c.ShouldBindJSON(&in) != nil {
		fail(c, http.StatusBadRequest, "bad_request")
		return
	}
	u, ok := s.viewer(c)
	if !ok {
		return
	}
	t, err := s.support.Create(c.Request.Context(), u, in.Subject, in.Category, in.Message)
	if err != nil {
		failErr(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"ticket": t})
}

func (s *Server) getTicket(c *gin.Context) {
	id, ok := ticketID(c)
	if !ok {
		return
	}
	u, ok := s.viewer(c)
	if !ok {
		return
	}
	v, err := s.support.Get(c.Request.Context(), u, id)
	if err != nil {
		failErr(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"ticket": v})
}

func (s *Server) replyTicket(c *gin.Context) {
	id, ok := ticketID(c)
	if !ok {
		return
	}
	var in struct {
		Message string `json:"message"`
	}
	if c.ShouldBindJSON(&in) != nil {
		fail(c, http.StatusBadRequest, "bad_request")
		return
	}
	u, ok := s.viewer(c)
	if !ok {
		return
	}
	if _, err := s.support.Reply(c.Request.Context(), u, id, in.Message); err != nil {
		failErr(c, err)
		return
	}
	v, err := s.support.Get(c.Request.Context(), u, id)
	if err != nil {
		failErr(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"ticket": v})
}

func (s *Server) closeTicket(c *gin.Context)  { s.ticketState(c, false) }
func (s *Server) reopenTicket(c *gin.Context) { s.ticketState(c, true) }

func (s *Server) ticketState(c *gin.Context, reopen bool) {
	id, ok := ticketID(c)
	if !ok {
		return
	}
	u, ok := s.viewer(c)
	if !ok {
		return
	}
	var err error
	if reopen {
		err = s.support.Reopen(c.Request.Context(), u, id)
	} else {
		err = s.support.Close(c.Request.Context(), u, id)
	}
	if err != nil {
		failErr(c, err)
		return
	}
	v, err := s.support.Get(c.Request.Context(), u, id)
	if err != nil {
		failErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ticket": v})
}
