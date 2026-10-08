package webgw

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/textproto"
	"strconv"
	"strings"
	"time"
)

// Минимальный клиент FastCGI (роль «ответчик», одно соединение на запрос): достаточно, чтобы PHP-FPM выполнил скрипт и вернул ответ.
// Внешних зависимостей нет: протокол записей простой, а поведение при сбоях (обрыв, зависание, огромный ответ) здесь под контролем.

const (
	fcgiVersion     = 1
	fcgiBeginReq    = 1
	fcgiEndReq      = 3
	fcgiParams      = 4
	fcgiStdin       = 5
	fcgiStdout      = 6
	fcgiStderr      = 7
	fcgiResponder   = 1
	fcgiMaxContent  = 65535
	fcgiMaxHeaders  = 64 << 10
	fcgiMaxStderr   = 8 << 10
	fcgiDialTimeout = 3 * time.Second
	fcgiRequestID   = 1
)

var errFCGIDown = errors.New("fcgi: pool is not available")

type fcgiWriter struct{ w io.Writer }

func (f fcgiWriter) record(typ byte, data []byte) error {
	for {
		n := min(len(data), fcgiMaxContent)
		var h [8]byte
		h[0], h[1] = fcgiVersion, typ
		binary.BigEndian.PutUint16(h[2:], fcgiRequestID)
		binary.BigEndian.PutUint16(h[4:], uint16(n))
		if _, err := f.w.Write(h[:]); err != nil {
			return err
		}
		if _, err := f.w.Write(data[:n]); err != nil {
			return err
		}
		data = data[n:]
		if len(data) == 0 {
			return nil
		}
	}
}

func encodeLen(b *bytes.Buffer, n int) {
	if n < 128 {
		b.WriteByte(byte(n))
		return
	}
	var x [4]byte
	binary.BigEndian.PutUint32(x[:], uint32(n)|1<<31)
	b.Write(x[:])
}

func encodeParams(params map[string]string) []byte {
	var b bytes.Buffer
	for k, v := range params {
		encodeLen(&b, len(k))
		encodeLen(&b, len(v))
		b.WriteString(k)
		b.WriteString(v)
	}
	return b.Bytes()
}

// fcgiServe выполняет запрос в FastCGI-сервере по unix-сокету и передаёт ответ клиенту. Возвращает ошибку, только если ответ ещё не
// начат (тогда вызывающий покажет свою страницу ошибки): когда заголовки уже ушли, сбой просто обрывает передачу.
func fcgiServe(ctx context.Context, w http.ResponseWriter, r *http.Request, socket string, params map[string]string, timeout time.Duration) (started bool, err error) {
	d := net.Dialer{Timeout: fcgiDialTimeout}
	conn, err := d.DialContext(ctx, "unix", socket)
	if err != nil {
		return false, fmt.Errorf("%w: %v", errFCGIDown, err)
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Now().Add(timeout))

	// Запрос пишется в отдельной горутине: PHP может начать отвечать, не дочитав тело.
	werr := make(chan error, 1)
	go func() { werr <- fcgiSend(conn, r, params) }()

	br := bufio.NewReaderSize(conn, 32<<10)
	var (
		head    bytes.Buffer
		stderr  bytes.Buffer
		gotHead bool
	)
	for {
		typ, data, err := fcgiReadRecord(br)
		if err != nil {
			if gotHead {
				return true, err
			}
			select {
			case e := <-werr:
				if e != nil {
					err = e
				}
			default:
			}
			return false, err
		}
		switch typ {
		case fcgiStderr:
			if stderr.Len() < fcgiMaxStderr {
				stderr.Write(data)
			}
		case fcgiStdout:
			if gotHead {
				if _, err := w.Write(data); err != nil {
					return true, err
				}
				continue
			}
			head.Write(data)
			end, sep := headerEnd(head.Bytes())
			if end < 0 {
				if head.Len() > fcgiMaxHeaders {
					return false, errors.New("fcgi: response headers are too large")
				}
				continue
			}
			if err := writeCGIHeaders(w, head.Bytes()[:end]); err != nil {
				return false, err
			}
			gotHead = true
			if rest := head.Bytes()[end+sep:]; len(rest) > 0 && r.Method != http.MethodHead {
				if _, err := w.Write(rest); err != nil {
					return true, err
				}
			}
		case fcgiEndReq:
			if stderr.Len() > 0 {
				log.Printf("шлюз: php stderr: %s", strings.TrimSpace(stderr.String()))
			}
			if !gotHead {
				return false, errors.New("fcgi: empty response")
			}
			return true, nil
		}
	}
}

func fcgiSend(conn net.Conn, r *http.Request, params map[string]string) error {
	fw := fcgiWriter{conn}
	var begin [8]byte
	binary.BigEndian.PutUint16(begin[0:], fcgiResponder) // роль; флаг keep-conn = 0: соединение закрывается после ответа
	if err := fw.record(fcgiBeginReq, begin[:]); err != nil {
		return err
	}
	if err := fw.record(fcgiParams, encodeParams(params)); err != nil {
		return err
	}
	if err := fw.record(fcgiParams, nil); err != nil {
		return err
	}
	if r.Body != nil {
		buf := make([]byte, 32<<10)
		for {
			n, err := r.Body.Read(buf)
			if n > 0 {
				if e := fw.record(fcgiStdin, buf[:n]); e != nil {
					return e
				}
			}
			if err != nil {
				break
			}
		}
	}
	return fw.record(fcgiStdin, nil)
}

func fcgiReadRecord(br *bufio.Reader) (typ byte, data []byte, err error) {
	var h [8]byte
	if _, err = io.ReadFull(br, h[:]); err != nil {
		return 0, nil, err
	}
	if h[0] != fcgiVersion {
		return 0, nil, errors.New("fcgi: bad protocol version")
	}
	n := int(binary.BigEndian.Uint16(h[4:]))
	pad := int(h[6])
	buf := make([]byte, n+pad)
	if _, err = io.ReadFull(br, buf); err != nil {
		return 0, nil, err
	}
	return h[1], buf[:n], nil
}

// headerEnd ищет конец заголовков CGI-ответа (пустая строка) и возвращает смещение и длину разделителя.
func headerEnd(b []byte) (int, int) {
	if i := bytes.Index(b, []byte("\r\n\r\n")); i >= 0 {
		return i, 4
	}
	if i := bytes.Index(b, []byte("\n\n")); i >= 0 {
		return i, 2
	}
	return -1, 0
}

// writeCGIHeaders переносит заголовки CGI-ответа в ответ шлюза. Status: задаёт код (по умолчанию 200).
func writeCGIHeaders(w http.ResponseWriter, raw []byte) error {
	tp := textproto.NewReader(bufio.NewReader(bytes.NewReader(append(bytes.ReplaceAll(raw, []byte("\r\n"), []byte("\n")), '\n', '\n'))))
	hdr, err := tp.ReadMIMEHeader()
	if err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("fcgi: bad response headers: %w", err)
	}
	status := http.StatusOK
	if s := hdr.Get("Status"); s != "" {
		code, _, _ := strings.Cut(s, " ")
		n, err := strconv.Atoi(code)
		if err != nil || n < 100 || n > 599 {
			return errors.New("fcgi: bad status")
		}
		status = n
	}
	if loc := hdr.Get("Location"); loc != "" && status == http.StatusOK {
		status = http.StatusFound
	}
	out := w.Header()
	for k, vs := range hdr {
		switch textproto.CanonicalMIMEHeaderKey(k) {
		case "Status", "Connection", "Keep-Alive", "Transfer-Encoding", "Content-Length":
			continue // длину и способ передачи определяет сам шлюз
		}
		for _, v := range vs {
			out.Add(k, v)
		}
	}
	w.WriteHeader(status)
	return nil
}
