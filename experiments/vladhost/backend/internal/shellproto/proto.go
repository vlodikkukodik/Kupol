// Package shellproto — протокол между панелью и посредником оболочек (vladhost shell-broker, работает от root). Панель не имеет прав
// запускать процессы под пользователями сайтов, поэтому просит посредника: тот проверяет запрос и запускает оболочку в песочнице
// systemd. По unix-сокету идут кадры: тип (1 байт), длина (4 байта, big endian), содержимое.
package shellproto

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"sync"
)

// Кадры от панели к посреднику.
const (
	FrameHeader byte = 'H' // JSON Header, ровно один раз и первым
	FrameStdin  byte = 'D' // данные для ввода
	FrameEOF    byte = 'E' // конец ввода
	FrameResize byte = 'W' // JSON Resize
	FrameSignal byte = 'S' // имя сигнала (INT, TERM, HUP, QUIT)
)

// Кадры от посредника к панели.
const (
	FrameOut  byte = 'O' // вывод (у терминала — всё вместе)
	FrameErr  byte = 'R' // поток ошибок (только без терминала)
	FrameExit byte = 'Q' // JSON Exit: сеанс закончился
	FrameFail byte = 'F' // короткий код отказа: сеанс не начался
)

// Операции заголовка.
const (
	OpShell = "shell" // интерактивная оболочка
	OpExec  = "exec"  // одна команда
	OpSFTP  = "sftp"  // подсистема SFTP
	OpKill  = "kill"  // закрыть все сеансы сайта (доступ отключён)
)

// Пределы: кадр и заголовок не должны позволять раздуть память посредника.
const (
	MaxFrame   = 1 << 20
	MaxCommand = 4096
)

var ErrFrameTooLarge = errors.New("shellproto: frame too large")

// Header — запрос на сеанс.
type Header struct {
	Op   string `json:"op"`
	Site int64  `json:"site"`
	Host string `json:"host"`
	// PTY — нужен ли терминал (для shell всегда; для exec — если клиент запросил pty).
	PTY  bool   `json:"pty"`
	Term string `json:"term"`
	Cols int    `json:"cols"`
	Rows int    `json:"rows"`
	Cmd  string `json:"cmd"`
}

// Resize — новый размер окна терминала.
type Resize struct {
	Cols int `json:"cols"`
	Rows int `json:"rows"`
}

// Exit — итог сеанса.
type Exit struct {
	Code int `json:"code"`
}

// Conn — соединение с записью, безопасной для нескольких горутин.
type Conn struct {
	rw io.ReadWriter
	mu sync.Mutex
}

// NewConn оборачивает соединение.
func NewConn(rw io.ReadWriter) *Conn { return &Conn{rw: rw} }

// Write отправляет кадр; длинное содержимое режется на кадры не больше 64 КБ.
func (c *Conn) Write(typ byte, payload []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	for {
		n := min(len(payload), 64<<10)
		var h [5]byte
		h[0] = typ
		binary.BigEndian.PutUint32(h[1:], uint32(n))
		if _, err := c.rw.Write(h[:]); err != nil {
			return err
		}
		if _, err := c.rw.Write(payload[:n]); err != nil {
			return err
		}
		payload = payload[n:]
		if len(payload) == 0 {
			return nil
		}
	}
}

// WriteJSON отправляет кадр с JSON.
func (c *Conn) WriteJSON(typ byte, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return c.Write(typ, data)
}

// Read читает следующий кадр.
func (c *Conn) Read() (byte, []byte, error) {
	var h [5]byte
	if _, err := io.ReadFull(c.rw, h[:]); err != nil {
		return 0, nil, err
	}
	n := binary.BigEndian.Uint32(h[1:])
	if n > MaxFrame {
		return 0, nil, ErrFrameTooLarge
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(c.rw, buf); err != nil {
		return 0, nil, err
	}
	return h[0], buf, nil
}
