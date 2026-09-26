// Package cronjobs — планировщик задач пользователя: HTTP-запросы и команды в песочнице сайта по расписанию cron.
// Расписание — пять полей (минута час день месяц день_недели) по UTC; чаще раза в MinIntervalMin минут задачи не ходят.
package cronjobs

import (
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/robfig/cron/v3"
)

// Разбор без секунд и без сокращений (@every, @hourly) и без смены часового пояса (CRON_TZ=…): расписание всегда по UTC.
var parser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)

var (
	errSchedule = errors.New("cronjobs: invalid schedule")
	errInterval = errors.New("cronjobs: schedule is too frequent")
)

// ParseSchedule разбирает расписание и проверяет минимальный промежуток между запусками.
func ParseSchedule(expr string, minMinutes int) (cron.Schedule, error) {
	expr = strings.TrimSpace(expr)
	if expr == "" || len(expr) > 100 || strings.ContainsAny(expr, "@\n\r") || strings.Contains(strings.ToUpper(expr), "TZ=") {
		return nil, errSchedule
	}
	sch, err := parser.Parse(expr)
	if err != nil {
		return nil, errSchedule
	}
	spec, ok := sch.(*cron.SpecSchedule)
	if !ok {
		return nil, errSchedule
	}
	// Запуски внутри суток задаются часами и минутами. Смотрим на все пары (час, минута) и на стык суток: если дни
	// подряд разрешены, последний запуск дня и первый запуск следующего тоже не должны быть ближе порога.
	var times []int
	for h := 0; h < 24; h++ {
		if spec.Hour&(1<<uint(h)) == 0 {
			continue
		}
		for m := 0; m < 60; m++ {
			if spec.Minute&(1<<uint(m)) != 0 {
				times = append(times, h*60+m)
			}
		}
	}
	if len(times) == 0 {
		return nil, errSchedule
	}
	slices.Sort(times)
	gap := 24*60 - times[len(times)-1] + times[0]
	for i := 1; i < len(times); i++ {
		gap = min(gap, times[i]-times[i-1])
	}
	if gap < minMinutes {
		return nil, errInterval
	}
	// Расписание, которое не наступает никогда (30 февраля), бесполезно: сразу отказываем.
	if sch.Next(time.Now().UTC()).IsZero() {
		return nil, errSchedule
	}
	return sch, nil
}
