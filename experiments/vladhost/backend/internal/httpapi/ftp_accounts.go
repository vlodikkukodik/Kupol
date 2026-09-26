package httpapi

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"vladhost/internal/sites"
)

// ftpAccountJSON — дополнительный FTP-аккаунт сайта. Пароль сюда не попадает: он показывается один раз при выдаче.
type ftpAccountJSON struct {
	ID          int64      `json:"id"`
	Name        string     `json:"name"`
	Username    string     `json:"username"` // полный логин для FTP-клиента
	Dir         string     `json:"dir"`
	ReadOnly    bool       `json:"read_only"`
	Enabled     bool       `json:"enabled"`
	LastLoginAt *time.Time `json:"last_login_at"`
	CreatedAt   time.Time  `json:"created_at"`
}

func (s *Server) toAccountJSON(st sites.Site, a sites.FTPAccount) ftpAccountJSON {
	return ftpAccountJSON{
		ID: a.ID, Name: a.Name, Username: s.sites.FTPAccountUsername(st.Host, a.Name), Dir: a.Dir,
		ReadOnly: a.ReadOnly, Enabled: a.Enabled, LastLoginAt: a.LastLoginAt, CreatedAt: a.CreatedAt,
	}
}

func (s *Server) toAccountsJSON(st sites.Site, list []sites.FTPAccount) []ftpAccountJSON {
	out := make([]ftpAccountJSON, 0, len(list))
	for _, a := range list {
		out = append(out, s.toAccountJSON(st, a))
	}
	return out
}

func accountID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("aid"), 10, 64)
	if err != nil || id <= 0 {
		fail(c, http.StatusNotFound, "ftp_account_not_found")
		return 0, false
	}
	return id, true
}

// ftpAccountReply отвечает состоянием аккаунта; пароль (если он выдавался) есть только в этом ответе.
func (s *Server) ftpAccountReply(c *gin.Context, status int, st *sites.Site, a *sites.FTPAccount, password string) {
	body := gin.H{"account": s.toAccountJSON(*st, *a)}
	if password != "" {
		body["password"] = password
		c.Header("Cache-Control", "no-store")
	}
	c.JSON(status, body)
}

func (s *Server) createFTPAccount(c *gin.Context) {
	id, ok := siteID(c)
	if !ok {
		return
	}
	if s.cfg.FTP.Addr == "" {
		fail(c, http.StatusConflict, "ftp_unavailable")
		return
	}
	var in struct {
		Name     string `json:"name"`
		Dir      string `json:"dir"`
		ReadOnly bool   `json:"read_only"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		fail(c, http.StatusBadRequest, "bad_request")
		return
	}
	a, st, pw, err := s.sites.CreateFTPAccount(c.Request.Context(), c.GetInt64("uid"), id, in.Name, in.Dir, in.ReadOnly)
	if err != nil {
		failErr(c, err)
		return
	}
	s.ftpAccountReply(c, http.StatusCreated, st, a, pw)
}

func (s *Server) updateFTPAccount(c *gin.Context) {
	id, ok := siteID(c)
	if !ok {
		return
	}
	aid, ok := accountID(c)
	if !ok {
		return
	}
	var in struct {
		Enabled  *bool   `json:"enabled"`
		ReadOnly *bool   `json:"read_only"`
		Dir      *string `json:"dir"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		fail(c, http.StatusBadRequest, "bad_request")
		return
	}
	a, st, err := s.sites.UpdateFTPAccount(c.Request.Context(), c.GetInt64("uid"), id, aid,
		sites.FTPAccountPatch{Enabled: in.Enabled, ReadOnly: in.ReadOnly, Dir: in.Dir})
	if err != nil {
		failErr(c, err)
		return
	}
	s.ftpAccountReply(c, http.StatusOK, st, a, "")
}

func (s *Server) resetFTPAccountPassword(c *gin.Context) {
	id, ok := siteID(c)
	if !ok {
		return
	}
	aid, ok := accountID(c)
	if !ok {
		return
	}
	a, st, pw, err := s.sites.ResetFTPAccountPassword(c.Request.Context(), c.GetInt64("uid"), id, aid)
	if err != nil {
		failErr(c, err)
		return
	}
	s.ftpAccountReply(c, http.StatusOK, st, a, pw)
}

func (s *Server) deleteFTPAccount(c *gin.Context) {
	id, ok := siteID(c)
	if !ok {
		return
	}
	aid, ok := accountID(c)
	if !ok {
		return
	}
	if err := s.sites.DeleteFTPAccount(c.Request.Context(), c.GetInt64("uid"), id, aid); err != nil {
		failErr(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}
