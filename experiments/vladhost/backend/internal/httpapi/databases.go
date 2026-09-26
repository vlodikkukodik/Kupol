package httpapi

import (
	"crypto/subtle"
	"net"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"vladhost/internal/userdb"
)

// WithDatabases подключает раздел «Базы данных». internalKey — общий секрет с веб-клиентом (Adminer) для обмена токена входа.
func WithDatabases(d *userdb.Service, internalKey string) Option {
	return func(s *Server) { s.dbs, s.dbKey = d, internalKey }
}

func dbID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		failErr(c, userdb.ErrNotFound)
		return 0, false
	}
	return id, true
}

// requireDatabases отвечает 404, когда раздел выключен: как будто его нет.
func (s *Server) requireDatabases(c *gin.Context) {
	if !s.dbs.Enabled() {
		fail(c, http.StatusNotFound, "not_found")
		return
	}
	c.Next()
}

func (s *Server) listDatabases(c *gin.Context) {
	list, err := s.dbs.List(c.Request.Context(), c.GetInt64("uid"))
	if err != nil {
		failErr(c, err)
		return
	}
	if list == nil {
		list = []userdb.Database{}
	}
	c.JSON(http.StatusOK, gin.H{"databases": list, "info": s.dbs.Info()})
}

func (s *Server) createDatabase(c *gin.Context) {
	var in struct {
		Engine userdb.Engine `json:"engine"`
		Name   string        `json:"name"`
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
	d, pw, err := s.dbs.Create(c.Request.Context(), *u, in.Engine, in.Name)
	if err != nil {
		failErr(c, err)
		return
	}
	setAuditTarget(c, d.Name)
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusCreated, gin.H{"database": d, "password": pw})
}

func (s *Server) deleteDatabase(c *gin.Context) {
	id, ok := dbID(c)
	if !ok {
		return
	}
	if err := s.dbs.Delete(c.Request.Context(), c.GetInt64("uid"), id); err != nil {
		failErr(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (s *Server) resetDatabasePassword(c *gin.Context) {
	id, ok := dbID(c)
	if !ok {
		return
	}
	d, pw, err := s.dbs.ResetPassword(c.Request.Context(), c.GetInt64("uid"), id)
	if err != nil {
		failErr(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"database": d, "password": pw})
}

func (s *Server) setDatabaseAddrs(c *gin.Context) {
	id, ok := dbID(c)
	if !ok {
		return
	}
	var in struct {
		Addrs []string `json:"addrs"`
	}
	if c.ShouldBindJSON(&in) != nil {
		fail(c, http.StatusBadRequest, "bad_request")
		return
	}
	d, err := s.dbs.SetAddrs(c.Request.Context(), c.GetInt64("uid"), id, in.Addrs)
	if err != nil {
		failErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"database": d})
}

func (s *Server) openDatabaseWeb(c *gin.Context) {
	id, ok := dbID(c)
	if !ok {
		return
	}
	link, err := s.dbs.OpenWebClient(c.Request.Context(), c.GetInt64("uid"), id)
	if err != nil {
		failErr(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"url": link})
}

func (s *Server) checkDatabase(c *gin.Context) {
	id, ok := dbID(c)
	if !ok {
		return
	}
	d, err := s.dbs.Check(c.Request.Context(), c.GetInt64("uid"), id)
	if err != nil {
		failErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"database": d})
}

// dbSession обменивает одноразовый токен на данные входа веб-клиента. Только для самого веб-клиента: он обращается сюда
// с этой же машины и подтверждает себя общим секретом. Снаружи (через nginx) обработчик недоступен: запрос с заголовками
// прокси или не с loopback-адреса отклоняется, а nginx дополнительно закрывает путь /api/internal/.
func (s *Server) dbSession(c *gin.Context) {
	host, _, err := net.SplitHostPort(c.Request.RemoteAddr)
	ip := net.ParseIP(host)
	if err != nil || ip == nil || !ip.IsLoopback() || c.GetHeader("X-Forwarded-For") != "" || c.GetHeader("X-Real-IP") != "" ||
		s.dbKey == "" || subtle.ConstantTimeCompare([]byte(c.GetHeader("X-Vh-Internal")), []byte(s.dbKey)) != 1 {
		fail(c, http.StatusNotFound, "not_found")
		return
	}
	var in struct {
		Token string `json:"token"`
	}
	if c.ShouldBindJSON(&in) != nil || in.Token == "" {
		fail(c, http.StatusBadRequest, "bad_request")
		return
	}
	sess, err := s.dbs.Redeem(in.Token)
	if err != nil {
		failErr(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, sess)
}
