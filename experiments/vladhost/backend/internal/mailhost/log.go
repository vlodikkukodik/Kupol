package mailhost

import (
	"context"
	"encoding/json"
	"log"
	"strings"
	"time"

	"vladhost/internal/apperr"
	"vladhost/internal/runtimes"

	"net/http"
)

// ActionMailLog — просьба исполнителю отдать журнал почты домена (deploy/bin/mail-log.py).
const ActionMailLog = "mail-log"

// MaxLogEvents — сколько последних событий возвращается за раз.
const MaxLogEvents = 100

var ErrLogFailed = apperr.New(http.StatusBadGateway, "mail_log_failed", "mail log is not available")

// LogEvent — событие доставки: принято, доставлено, не доставлено, отложено, отклонено.
type LogEvent struct {
	Time   string `json:"t"`
	Kind   string `json:"kind"`
	ID     string `json:"id"`
	From   string `json:"from"`
	To     string `json:"to"`
	Host   string `json:"host"`
	Via    string `json:"via"`
	Detail string `json:"detail"`
}

// QueueItem — письмо, которое ещё лежит в очереди отправки.
type QueueItem struct {
	ID     string   `json:"id"`
	Age    string   `json:"age"`
	Size   string   `json:"size"`
	From   string   `json:"from"`
	To     []string `json:"to"`
	Frozen bool     `json:"frozen"`
}

// Journal — журнал домена и его очередь.
type Journal struct {
	Events []LogEvent  `json:"events"`
	Queue  []QueueItem `json:"queue"`
}

var logKinds = map[string]bool{"received": true, "delivered": true, "failed": true, "deferred": true, "rejected": true}

// Log возвращает последние события почты домена и его очередь. Читает исполнитель (журнал exim доступен только root); ответ проверяется:
// неизвестные виды событий и лишние поля отбрасываются.
func (s *Service) Log(ctx context.Context, userID, domainID int64) (*Journal, error) {
	d, err := s.domain(ctx, userID, domainID)
	if err != nil {
		return nil, err
	}
	res, err := s.cfg.Applier.Do(ctx, runtimes.Request{Action: ActionMailLog, Host: d.Domain, ID: MaxLogEvents}, 30*time.Second)
	if err != nil {
		log.Printf("почта: журнал: %v", err)
		return nil, ErrHelperDown
	}
	if !res.OK {
		log.Printf("почта: журнал не получен: %s", res.Error)
		return nil, ErrLogFailed
	}
	var j Journal
	dec := json.NewDecoder(strings.NewReader(res.Output))
	if err := dec.Decode(&j); err != nil {
		log.Printf("почта: журнал: ответ не разобран: %v", err)
		return nil, ErrLogFailed
	}
	events := j.Events[:0]
	for _, e := range j.Events {
		if logKinds[e.Kind] {
			events = append(events, e)
		}
	}
	if len(events) > MaxLogEvents {
		events = events[:MaxLogEvents]
	}
	j.Events = events
	if j.Events == nil {
		j.Events = []LogEvent{}
	}
	if j.Queue == nil {
		j.Queue = []QueueItem{}
	}
	for i := range j.Queue {
		if j.Queue[i].To == nil {
			j.Queue[i].To = []string{}
		}
	}
	return &j, nil
}
