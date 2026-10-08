package httpapi

import (
	"errors"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"

	"vladhost/internal/activity"
	"vladhost/internal/auth"
)

// sendVerification отправляет ссылку подтверждения адреса; ошибки только пишутся в журнал: почта не должна ломать вход.
func (s *Server) sendVerification(c *gin.Context, u auth.User) {
	if !s.mail.Enabled() || u.EmailVerifiedAt != nil {
		return
	}
	raw, fresh, err := s.svc.IssueMailToken(c.Request.Context(), u.ID, auth.TokenVerify)
	if err != nil {
		if !errors.Is(err, auth.ErrMailCooldown) && !errors.Is(err, auth.ErrAlreadyVerified) {
			log.Printf("почта: ссылка подтверждения для %d: %v", u.ID, err)
		}
		return
	}
	if err := s.mail.SendVerification(c.Request.Context(), *fresh, raw); err != nil {
		log.Printf("почта: письмо подтверждения для %d: %v", u.ID, err)
	}
}

func (s *Server) notifyPasswordChanged(c *gin.Context, u auth.User) {
	if !s.mail.Enabled() {
		return
	}
	if err := s.mail.SendPasswordChanged(c.Request.Context(), u); err != nil {
		log.Printf("почта: уведомление о смене пароля для %d: %v", u.ID, err)
	}
}

// forgotPassword присылает ссылку для сброса пароля. Ответ один и тот же для любого адреса (существует ли он — не раскрываем);
// частые запросы для одного аккаунта молча игнорируются.
func (s *Server) forgotPassword(c *gin.Context) {
	var in struct {
		Email string `json:"email"`
	}
	if c.ShouldBindJSON(&in) != nil {
		fail(c, http.StatusBadRequest, "bad_request")
		return
	}
	if !s.mail.Enabled() {
		fail(c, http.StatusConflict, "mail_unavailable")
		return
	}
	u, err := s.svc.UserByEmail(c.Request.Context(), in.Email)
	if err != nil {
		if errors.Is(err, auth.ErrInvalidCredentials) {
			c.Status(http.StatusAccepted) // такого адреса нет — отвечаем так же, как если бы письмо ушло
			return
		}
		failErr(c, err)
		return
	}
	raw, fresh, err := s.svc.IssueMailToken(c.Request.Context(), u.ID, auth.TokenReset)
	if err != nil {
		if errors.Is(err, auth.ErrMailCooldown) {
			c.Status(http.StatusAccepted)
			return
		}
		failErr(c, err)
		return
	}
	fresh.Lang = string(lang(c)) // пишем на том языке, на котором человек сейчас пользуется страницей
	if err := s.mail.SendPasswordReset(c.Request.Context(), *fresh, raw); err != nil {
		log.Printf("почта: письмо сброса пароля для %d: %v", u.ID, err)
	}
	c.Status(http.StatusAccepted)
}

// resetPassword задаёт новый пароль по ссылке из письма.
func (s *Server) resetPassword(c *gin.Context) {
	var in struct {
		Token    string `json:"token"`
		Password string `json:"password"`
	}
	if c.ShouldBindJSON(&in) != nil || in.Token == "" {
		fail(c, http.StatusBadRequest, "bad_request")
		return
	}
	u, err := s.svc.ResetPassword(c.Request.Context(), in.Token, in.Password)
	if err != nil {
		failErr(c, err)
		return
	}
	u.Lang = string(lang(c))
	s.record(c, u.ID, activity.KindPasswordReset, "")
	s.notifyPasswordChanged(c, *u)
	c.Status(http.StatusNoContent)
}

// verifyEmail подтверждает адрес по ссылке из письма (страница открывается и без входа в панель).
func (s *Server) verifyEmail(c *gin.Context) {
	var in struct {
		Token string `json:"token"`
	}
	if c.ShouldBindJSON(&in) != nil || in.Token == "" {
		fail(c, http.StatusBadRequest, "bad_request")
		return
	}
	u, err := s.svc.VerifyEmail(c.Request.Context(), in.Token)
	if err != nil {
		failErr(c, err)
		return
	}
	s.record(c, u.ID, activity.KindEmailVerified, "")
	c.Status(http.StatusNoContent)
}

// resendVerification отправляет письмо подтверждения ещё раз.
func (s *Server) resendVerification(c *gin.Context) {
	if !s.mail.Enabled() {
		fail(c, http.StatusConflict, "mail_unavailable")
		return
	}
	raw, u, err := s.svc.IssueMailToken(c.Request.Context(), c.GetInt64("uid"), auth.TokenVerify)
	if err != nil {
		failErr(c, err)
		return
	}
	if err := s.mail.SendVerification(c.Request.Context(), *u, raw); err != nil {
		failErr(c, err)
		return
	}
	c.Status(http.StatusAccepted)
}

// updateMe меняет язык писем и согласие на уведомления.
func (s *Server) updateMe(c *gin.Context) {
	var in struct {
		Lang        *string `json:"lang"`
		NotifyEmail *bool   `json:"notify_email"`
	}
	if c.ShouldBindJSON(&in) != nil || (in.Lang != nil && *in.Lang != "ru" && *in.Lang != "it") {
		fail(c, http.StatusBadRequest, "bad_request")
		return
	}
	u, err := s.svc.UpdatePreferences(c.Request.Context(), c.GetInt64("uid"), auth.Preferences{Lang: in.Lang, NotifyEmail: in.NotifyEmail})
	if err != nil {
		failErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"user": u, "mail_enabled": s.mail.Enabled(), "databases_enabled": s.dbs.Enabled(), "shell_enabled": s.shell.Enabled(), "mailhost_enabled": s.mailhost.Enabled(), "dns_enabled": s.dns.Enabled()})
}
