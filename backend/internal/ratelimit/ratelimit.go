// Package ratelimit — ограничитель частоты со скользящим окном, ключи произвольные строки.
//
// Хранит счётчики в памяти одного процесса: API работает единственным экземпляром на VPS.
// После перезапуска счётчики обнуляются — для лимитов входа и регистрации это допустимо
// (перезапуск не в руках атакующего), а обещание «5 попыток за 15 минут» держится в остальное время.
package ratelimit

import (
	"context"
	"sync"
	"time"
)

type bucket struct {
	events []time.Time // по возрастанию
	window time.Duration
}

// Limiter — потокобезопасный ограничитель.
type Limiter struct {
	mu      sync.Mutex
	buckets map[string]*bucket
	now     func() time.Time
}

// New создаёт ограничитель. now == nil — настоящее время.
func New(now func() time.Time) *Limiter {
	if now == nil {
		now = time.Now
	}
	return &Limiter{buckets: make(map[string]*bucket), now: now}
}

// prune удаляет события старше окна; вызывать под l.mu.
func (b *bucket) prune(now time.Time, window time.Duration) {
	cut := now.Add(-window)
	i := 0
	for i < len(b.events) && !b.events[i].After(cut) {
		i++
	}
	if i > 0 {
		b.events = append(b.events[:0], b.events[i:]...)
	}
}

// retryAfter — через сколько освободится место, если событий уже >= limit; вызывать под l.mu.
func (b *bucket) retryAfter(now time.Time, limit int, window time.Duration) time.Duration {
	// пройти лимит можно, когда останется limit-1 событий: должно истечь len-limit+1 самых старых
	oldestToExpire := b.events[len(b.events)-limit]
	d := oldestToExpire.Add(window).Sub(now)
	if d < 0 {
		return 0
	}
	return d
}

// Blocked проверяет, исчерпан ли лимит по ключу, НЕ записывая событие.
// Возвращает, через сколько можно повторить.
func (l *Limiter) Blocked(key string, limit int, window time.Duration) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	b := l.buckets[key]
	if b == nil {
		return false, 0
	}
	now := l.now()
	b.prune(now, window)
	if len(b.events) < limit {
		return false, 0
	}
	return true, b.retryAfter(now, limit, window)
}

// Hit записывает событие по ключу (например, неудачный вход).
func (l *Limiter) Hit(key string, window time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.hit(key, window)
}

func (l *Limiter) hit(key string, window time.Duration) {
	b := l.buckets[key]
	if b == nil {
		b = &bucket{}
		l.buckets[key] = b
	}
	if window > b.window {
		b.window = window
	}
	b.events = append(b.events, l.now())
}

// Allow атомарно проверяет лимит и, если он не исчерпан, записывает событие.
// Отказанный запрос событием не считается (иначе повторы продлевали бы блокировку).
func (l *Limiter) Allow(key string, limit int, window time.Duration) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	if b := l.buckets[key]; b != nil {
		b.prune(now, window)
		if len(b.events) >= limit {
			return false, b.retryAfter(now, limit, window)
		}
	}
	l.hit(key, window)
	return true, 0
}

// Reset забывает все события ключа (например, после успешного входа).
func (l *Limiter) Reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.buckets, key)
}

// Len — число ключей в памяти (для тестов и мониторинга).
func (l *Limiter) Len() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.buckets)
}

// Sweep удаляет ключи, у которых не осталось событий в пределах их окна.
func (l *Limiter) Sweep() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	removed := 0
	for k, b := range l.buckets {
		b.prune(now, b.window)
		if len(b.events) == 0 {
			delete(l.buckets, k)
			removed++
		}
	}
	return removed
}

// RunSweeper периодически чистит память до отмены ctx.
func (l *Limiter) RunSweeper(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			l.Sweep()
		}
	}
}
