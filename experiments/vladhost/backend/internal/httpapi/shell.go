package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/gin-gonic/gin"

	"vladhost/internal/shellaccess"
	"vladhost/internal/shellclient"
	"vladhost/internal/shellproto"
)

// WithShell подключает SSH-ключи, включение доступа к оболочке и веб-терминал.
func WithShell(svc *shellaccess.Service, broker shellclient.Client) Option {
	return func(s *Server) { s.shell, s.shellBroker = svc, broker; s.tickets = newTickets() }
}

func (s *Server) requireShell(c *gin.Context) {
	if !s.shell.Enabled() {
		fail(c, http.StatusNotFound, "not_found")
		return
	}
	c.Next()
}

func (s *Server) listSSHKeys(c *gin.Context) {
	keys, err := s.shell.ListKeys(c.Request.Context(), c.GetInt64("uid"))
	if err != nil {
		failErr(c, err)
		return
	}
	if keys == nil {
		keys = []shellaccess.Key{}
	}
	c.JSON(http.StatusOK, gin.H{"keys": keys, "max_keys": shellaccess.MaxKeys, "host_fingerprint": s.shell.HostFingerprint()})
}

func (s *Server) addSSHKey(c *gin.Context) {
	var in struct {
		Name      string `json:"name"`
		PublicKey string `json:"public_key"`
	}
	if c.ShouldBindJSON(&in) != nil {
		fail(c, http.StatusBadRequest, "bad_request")
		return
	}
	k, err := s.shell.AddKey(c.Request.Context(), c.GetInt64("uid"), in.Name, in.PublicKey)
	if err != nil {
		failErr(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"key": k})
}

// generateSSHKey создаёт пару ключей; закрытая часть уходит в ответе один раз и нигде не хранится.
func (s *Server) generateSSHKey(c *gin.Context) {
	var in struct {
		Name string `json:"name"`
	}
	if c.ShouldBindJSON(&in) != nil {
		fail(c, http.StatusBadRequest, "bad_request")
		return
	}
	g, err := s.shell.GenerateKey(c.Request.Context(), c.GetInt64("uid"), in.Name)
	if err != nil {
		failErr(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusCreated, gin.H{"key": g.Key, "private_key": g.PrivateKey})
}

func (s *Server) deleteSSHKey(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		failErr(c, shellaccess.ErrKeyNotFound)
		return
	}
	if err := s.shell.DeleteKey(c.Request.Context(), c.GetInt64("uid"), id); err != nil {
		failErr(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (s *Server) getShell(c *gin.Context) {
	id, ok := siteID(c)
	if !ok {
		return
	}
	st, err := s.shell.Status(c.Request.Context(), c.GetInt64("uid"), id)
	if err != nil {
		failErr(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"shell": st})
}

func (s *Server) setShell(c *gin.Context) {
	id, ok := siteID(c)
	if !ok {
		return
	}
	var in struct {
		Enabled bool `json:"enabled"`
	}
	if c.ShouldBindJSON(&in) != nil {
		fail(c, http.StatusBadRequest, "bad_request")
		return
	}
	var st *shellaccess.Status
	var err error
	if in.Enabled {
		st, err = s.shell.Enable(c.Request.Context(), c.GetInt64("uid"), id)
	} else {
		st, err = s.shell.Disable(c.Request.Context(), c.GetInt64("uid"), id)
	}
	if err != nil {
		failErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"shell": st})
}

// --- веб-терминал ---

// ticketTTL — сколько билет на терминал ждёт подключения WebSocket.
const ticketTTL = 30 * time.Second

type ticket struct {
	userID, siteID int64
	host           string
	expires        time.Time
}

// tickets — одноразовые билеты: WebSocket из браузера не может передать заголовок Authorization, поэтому вход в терминал
// подтверждается билетом, который выдан авторизованным запросом и сгорает при первом использовании.
type tickets struct {
	mu sync.Mutex
	m  map[string]ticket
}

func newTickets() *tickets { return &tickets{m: map[string]ticket{}} }

func (t *tickets) issue(userID, siteID int64, host string) (string, error) {
	var b [24]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	id := hex.EncodeToString(b[:])
	t.mu.Lock()
	defer t.mu.Unlock()
	now := time.Now()
	for k, v := range t.m {
		if now.After(v.expires) {
			delete(t.m, k)
		}
	}
	if len(t.m) > 10000 {
		return "", errors.New("too many tickets")
	}
	t.m[id] = ticket{userID: userID, siteID: siteID, host: host, expires: now.Add(ticketTTL)}
	return id, nil
}

func (t *tickets) redeem(id string) (ticket, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	tk, ok := t.m[id]
	delete(t.m, id)
	if !ok || time.Now().After(tk.expires) {
		return ticket{}, false
	}
	return tk, true
}

func (s *Server) terminalTicket(c *gin.Context) {
	id, ok := siteID(c)
	if !ok {
		return
	}
	g, err := s.shell.WebGrant(c.Request.Context(), c.GetInt64("uid"), id)
	if err != nil {
		failErr(c, err)
		return
	}
	t, err := s.tickets.issue(g.UserID, g.SiteID, g.Host)
	if err != nil {
		fail(c, http.StatusServiceUnavailable, "internal")
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"ticket": t})
}

const terminalIdle = 30 * time.Minute

type termControl struct {
	Type string `json:"type"`
	Cols int    `json:"cols"`
	Rows int    `json:"rows"`
	Code int    `json:"code"`
	Err  string `json:"error,omitempty"`
}

// terminalSocket — WebSocket терминала: двоичные кадры — данные терминала в обе стороны, текстовые — управление (изменение размера).
func (s *Server) terminalSocket(c *gin.Context) {
	tk, ok := s.tickets.redeem(c.Query("ticket"))
	if !ok {
		fail(c, http.StatusUnauthorized, "unauthorized")
		return
	}
	opts := &websocket.AcceptOptions{}
	if s.cfg.PanelOrigin != "" {
		if u, err := url.Parse(s.cfg.PanelOrigin); err == nil {
			opts.OriginPatterns = []string{u.Host}
		}
	} else {
		opts.OriginPatterns = []string{"*"} // разработка и тесты: источник панели не задан
	}
	ws, err := websocket.Accept(c.Writer, c.Request, opts)
	if err != nil {
		return
	}
	defer func() { _ = ws.CloseNow() }()
	ws.SetReadLimit(1 << 20)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sess, err := s.shellBroker.Open(ctx, shellproto.Header{Op: shellproto.OpShell, Site: tk.siteID, Host: tk.host, PTY: true, Term: "xterm-256color", Cols: 80, Rows: 24})
	if err != nil {
		writeControl(ctx, ws, termControl{Type: "error", Err: "unavailable"})
		_ = ws.Close(websocket.StatusInternalError, "shell unavailable")
		return
	}
	defer sess.Close()

	var last time.Time
	var lastMu sync.Mutex
	touch := func() { lastMu.Lock(); last = time.Now(); lastMu.Unlock() }
	touch()

	// Бездействие и «живость» соединения.
	go func() {
		t := time.NewTicker(20 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				lastMu.Lock()
				idle := time.Since(last)
				lastMu.Unlock()
				if idle > terminalIdle {
					cancel()
					_ = ws.Close(websocket.StatusNormalClosure, "idle")
					return
				}
				pctx, pc := context.WithTimeout(ctx, 10*time.Second)
				if err := ws.Ping(pctx); err != nil {
					pc()
					cancel()
					return
				}
				pc()
			}
		}
	}()

	// Ввод браузера → сеанс.
	go func() {
		defer cancel()
		for {
			typ, data, err := ws.Read(ctx)
			if err != nil {
				return
			}
			touch()
			switch typ {
			case websocket.MessageBinary:
				if sess.Stdin(data) != nil {
					return
				}
			case websocket.MessageText:
				var ctl termControl
				if json.Unmarshal(data, &ctl) == nil && ctl.Type == "resize" && ctl.Cols > 0 && ctl.Rows > 0 {
					_ = sess.Resize(ctl.Cols, ctl.Rows)
				}
			}
		}
	}()

	// Вывод сеанса → браузер.
	go func() {
		<-ctx.Done()
		sess.Close()
	}()
	for {
		ev, err := sess.Recv()
		if err != nil {
			var ref *shellclient.RefusedError
			if errors.As(err, &ref) {
				writeControl(ctx, ws, termControl{Type: "error", Err: ref.Code})
			}
			break
		}
		touch()
		if ev.Type == shellproto.FrameExit {
			writeControl(ctx, ws, termControl{Type: "exit", Code: ev.Exit})
			break
		}
		if err := ws.Write(ctx, websocket.MessageBinary, ev.Data); err != nil {
			break
		}
	}
	_ = ws.Close(websocket.StatusNormalClosure, "")
}

func writeControl(ctx context.Context, ws *websocket.Conn, c termControl) {
	data, _ := json.Marshal(c)
	wctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	_ = ws.Write(wctx, websocket.MessageText, data)
}
