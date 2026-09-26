package httpapi

import (
	"errors"
	"log"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"vladhost/internal/activity"
	"vladhost/internal/auth"
)

// loginSecondFactor — второй шаг входа: билет из ответа /auth/login и код из приложения (или код восстановления).
func (s *Server) loginSecondFactor(c *gin.Context) {
	var in struct {
		Ticket string `json:"ticket"`
		Code   string `json:"code"`
	}
	if c.ShouldBindJSON(&in) != nil || in.Ticket == "" || in.Code == "" {
		fail(c, http.StatusBadRequest, "bad_request")
		return
	}
	uid := s.svc.TicketUser(in.Ticket)
	sess, recovery, err := s.svc.LoginSecondFactor(c.Request.Context(), in.Ticket, in.Code)
	if err != nil {
		if errors.Is(err, auth.ErrTwoFactorCode) && uid > 0 && s.activity != nil {
			s.record(c, uid, activity.KindLoginFailed, "2fa")
		}
		failErr(c, err)
		return
	}
	target := ""
	if recovery {
		target = "recovery"
	}
	s.record(c, sess.User.ID, activity.KindLogin, target)
	s.respondSession(c, sess)
}

func (s *Server) twoFactorStatus(c *gin.Context) {
	st, err := s.svc.TwoFactorStatus(c.Request.Context(), c.GetInt64("uid"))
	if err != nil {
		failErr(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, st)
}

func (s *Server) twoFactorSetup(c *gin.Context) {
	var in struct {
		Password string `json:"password"`
	}
	if c.ShouldBindJSON(&in) != nil || in.Password == "" {
		fail(c, http.StatusBadRequest, "bad_request")
		return
	}
	setup, err := s.svc.StartTwoFactor(c.Request.Context(), c.GetInt64("uid"), in.Password)
	if err != nil {
		failErr(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, setup)
}

func (s *Server) twoFactorEnable(c *gin.Context) {
	var in struct {
		Code string `json:"code"`
	}
	if c.ShouldBindJSON(&in) != nil || in.Code == "" {
		fail(c, http.StatusBadRequest, "bad_request")
		return
	}
	codes, err := s.svc.EnableTwoFactor(c.Request.Context(), c.GetInt64("uid"), in.Code)
	if err != nil {
		failErr(c, err)
		return
	}
	s.notifyTwoFactor(c, true)
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"recovery_codes": codes})
}

func (s *Server) twoFactorDisable(c *gin.Context) {
	var in struct {
		Password string `json:"password"`
		Code     string `json:"code"`
	}
	if c.ShouldBindJSON(&in) != nil || in.Password == "" || in.Code == "" {
		fail(c, http.StatusBadRequest, "bad_request")
		return
	}
	if err := s.svc.DisableTwoFactor(c.Request.Context(), c.GetInt64("uid"), in.Password, in.Code); err != nil {
		failErr(c, err)
		return
	}
	s.notifyTwoFactor(c, false)
	c.Status(http.StatusNoContent)
}

func (s *Server) twoFactorRecovery(c *gin.Context) {
	var in struct {
		Password string `json:"password"`
	}
	if c.ShouldBindJSON(&in) != nil || in.Password == "" {
		fail(c, http.StatusBadRequest, "bad_request")
		return
	}
	codes, err := s.svc.RegenerateRecoveryCodes(c.Request.Context(), c.GetInt64("uid"), in.Password)
	if err != nil {
		failErr(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"recovery_codes": codes})
}

func (s *Server) notifyTwoFactor(c *gin.Context, on bool) {
	if !s.mail.Enabled() {
		return
	}
	u, err := s.svc.UserByID(c.Request.Context(), c.GetInt64("uid"))
	if err != nil {
		return
	}
	if err := s.mail.SendTwoFactor(c.Request.Context(), *u, on); err != nil {
		log.Printf("почта: уведомление о втором факторе для %d: %v", u.ID, err)
	}
}

func (s *Server) listSessions(c *gin.Context) {
	list, err := s.svc.ListSessions(c.Request.Context(), c.GetInt64("uid"), c.GetInt64("sid"))
	if err != nil {
		failErr(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"sessions": list})
}

// revokeSession закрывает одну сессию. Текущую так не закрыть — для неё есть «Выйти».
func (s *Server) revokeSession(c *gin.Context) {
	sid, err := strconv.ParseInt(c.Param("sid"), 10, 64)
	if err != nil || sid <= 0 {
		fail(c, http.StatusBadRequest, "bad_request")
		return
	}
	if sid == c.GetInt64("sid") {
		fail(c, http.StatusUnprocessableEntity, "session_current")
		return
	}
	if err := s.svc.RevokeSession(c.Request.Context(), c.GetInt64("uid"), sid); err != nil {
		failErr(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (s *Server) revokeOtherSessions(c *gin.Context) {
	n, err := s.svc.RevokeOtherSessions(c.Request.Context(), c.GetInt64("uid"), c.GetInt64("sid"))
	if err != nil {
		failErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"revoked": n})
}
