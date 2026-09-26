package httpapi

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"vladhost/internal/cms"
	"vladhost/internal/i18n"
)

// WithCMS подключает раздел «Установка приложений» (WordPress в один клик).
func WithCMS(c *cms.Service) Option { return func(s *Server) { s.cms = c } }

// requireCMS отвечает 404, пока установщик не подключён.
func (s *Server) requireCMS(c *gin.Context) {
	if s.cms == nil {
		fail(c, http.StatusNotFound, "not_found")
		return
	}
	c.Next()
}

func (s *Server) getCMS(c *gin.Context) {
	id, ok := siteID(c)
	if !ok {
		return
	}
	st, err := s.cms.Status(c.Request.Context(), c.GetInt64("uid"), id)
	if err != nil {
		failErr(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"cms": st})
}

func (s *Server) installCMS(c *gin.Context) {
	id, ok := siteID(c)
	if !ok {
		return
	}
	var in struct {
		CMS        string `json:"cms"`
		Title      string `json:"title"`
		AdminUser  string `json:"admin_user"`
		AdminEmail string `json:"admin_email"`
		Locale     string `json:"locale"`
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
	if err := s.cms.Start(c.Request.Context(), *u, id, cms.Input{CMS: in.CMS, Title: in.Title, AdminUser: in.AdminUser, AdminEmail: in.AdminEmail, Locale: in.Locale}); err != nil {
		failErr(c, err)
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"status": "started"})
}

// cmsFailure — отказ установки для интерфейса: сообщение уже на языке пользователя.
type cmsFailure struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Step    string `json:"step"`
	Detail  string `json:"detail,omitempty"`
}

func (s *Server) cmsJob(c *gin.Context) {
	id, ok := siteID(c)
	if !ok {
		return
	}
	// Владелец проверяется через состояние сайта: чужой сайт даёт 404 так же, как в остальных разделах.
	if _, err := s.cms.Status(c.Request.Context(), c.GetInt64("uid"), id); err != nil {
		failErr(c, err)
		return
	}
	v, apiErr := s.cms.TakeJob(c.GetInt64("uid"), id)
	c.Header("Cache-Control", "no-store")
	if v == nil {
		c.JSON(http.StatusOK, gin.H{"job": nil})
		return
	}
	out := gin.H{"status": v.Status, "step": v.Step, "steps": v.Steps}
	if v.Result != nil {
		out["result"] = v.Result
	}
	if v.Failure != nil && apiErr != nil {
		out["failure"] = cmsFailure{Code: v.Failure.Code, Step: v.Failure.Step, Detail: v.Failure.Detail,
			Message: i18n.T(lang(c), "err."+apiErr.Code, apiErr.Args...)}
	}
	c.JSON(http.StatusOK, gin.H{"job": out})
}
