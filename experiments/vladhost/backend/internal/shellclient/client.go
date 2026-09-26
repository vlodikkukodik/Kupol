// Package shellclient — клиент посредника оболочек (см. shellbroker): открывает сеанс в песочнице сайта и обменивается с ним кадрами.
// Им пользуются SSH-сервер панели и веб-терминал.
package shellclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"vladhost/internal/shellproto"
)

// RefusedError — посредник отказал: код объясняет причину (disabled, busy, bad_site, start_failed…).
type RefusedError struct{ Code string }

func (e *RefusedError) Error() string { return "shell refused: " + e.Code }

// Event — то, что пришло от сеанса: вывод, поток ошибок или итог.
type Event struct {
	Type byte // shellproto.FrameOut, FrameErr или FrameExit
	Data []byte
	Exit int
}

// Client знает адрес сокета посредника.
type Client struct {
	Socket  string
	Timeout time.Duration // на подключение; по умолчанию 5 с
}

// Session — открытый сеанс.
type Session struct {
	nc   net.Conn
	conn *shellproto.Conn
	once sync.Once
}

// Open подключается к посреднику и отправляет заголовок. Отказ приходит первым событием Recv (RefusedError).
func (c Client) Open(ctx context.Context, h shellproto.Header) (*Session, error) {
	d := net.Dialer{Timeout: c.Timeout}
	if d.Timeout <= 0 {
		d.Timeout = 5 * time.Second
	}
	nc, err := d.DialContext(ctx, "unix", c.Socket)
	if err != nil {
		return nil, fmt.Errorf("shell broker is not available: %w", err)
	}
	s := &Session{nc: nc, conn: shellproto.NewConn(nc)}
	if err := s.conn.WriteJSON(shellproto.FrameHeader, h); err != nil {
		// Посредник мог отказать и закрыть соединение раньше, чем мы дописали заголовок: причина отказа лежит в сокете.
		_ = nc.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
		if typ, payload, rerr := s.conn.Read(); rerr == nil && typ == shellproto.FrameFail {
			_ = nc.Close()
			return nil, &RefusedError{Code: string(payload)}
		}
		_ = nc.Close()
		return nil, err
	}
	return s, nil
}

// Stdin передаёт данные на ввод сеанса.
func (s *Session) Stdin(p []byte) error { return s.conn.Write(shellproto.FrameStdin, p) }

// CloseStdin сообщает, что ввода больше не будет.
func (s *Session) CloseStdin() error { return s.conn.Write(shellproto.FrameEOF, nil) }

// Resize меняет размер терминала.
func (s *Session) Resize(cols, rows int) error {
	return s.conn.WriteJSON(shellproto.FrameResize, shellproto.Resize{Cols: cols, Rows: rows})
}

// Signal посылает сеансу сигнал (INT, TERM, HUP, QUIT, KILL).
func (s *Session) Signal(name string) error {
	return s.conn.Write(shellproto.FrameSignal, []byte(name))
}

// Recv возвращает следующее событие. После итога (FrameExit) больше событий не будет.
func (s *Session) Recv() (Event, error) {
	for {
		typ, payload, err := s.conn.Read()
		if err != nil {
			return Event{}, err
		}
		switch typ {
		case shellproto.FrameOut, shellproto.FrameErr:
			return Event{Type: typ, Data: payload}, nil
		case shellproto.FrameExit:
			var e shellproto.Exit
			if err := json.Unmarshal(payload, &e); err != nil {
				return Event{}, err
			}
			return Event{Type: typ, Exit: e.Code}, nil
		case shellproto.FrameFail:
			return Event{}, &RefusedError{Code: string(payload)}
		}
	}
}

// Close закрывает соединение: посредник останавливает сеанс.
func (s *Session) Close() { s.once.Do(func() { _ = s.nc.Close() }) }

// Kill просит посредника закрыть все сеансы сайта (доступ отключён). Возвращает число закрытых.
func (c Client) Kill(ctx context.Context, site int64) (int, error) {
	s, err := c.Open(ctx, shellproto.Header{Op: shellproto.OpKill, Site: site})
	if err != nil {
		return 0, err
	}
	defer s.Close()
	_ = s.nc.SetReadDeadline(time.Now().Add(10 * time.Second))
	ev, err := s.Recv()
	if err != nil {
		return 0, err
	}
	if ev.Type != shellproto.FrameExit {
		return 0, errors.New("unexpected reply")
	}
	return ev.Exit, nil
}
