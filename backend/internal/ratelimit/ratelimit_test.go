package ratelimit

import (
	"context"
	"strconv"
	"sync"
	"testing"
	"time"
)

// clock — управляемые часы для тестов.
type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

func newClock() *clock { return &clock{t: time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)} }

func TestAllowUpToLimitThenBlock(t *testing.T) {
	c := newClock()
	l := New(c.now)
	for i := 0; i < 3; i++ {
		if ok, _ := l.Allow("k", 3, time.Hour); !ok {
			t.Fatalf("запрос %d должен пройти", i+1)
		}
	}
	ok, retry := l.Allow("k", 3, time.Hour)
	if ok {
		t.Fatal("4-й запрос должен быть отклонён")
	}
	if retry != time.Hour {
		t.Fatalf("retryAfter = %s, ожидался час до истечения самого раннего события", retry)
	}
}

func TestWindowSlides(t *testing.T) {
	c := newClock()
	l := New(c.now)
	l.Allow("k", 2, 10*time.Minute) // t=0
	c.advance(4 * time.Minute)
	l.Allow("k", 2, 10*time.Minute) // t=4

	ok, retry := l.Allow("k", 2, 10*time.Minute) // t=4, лимит исчерпан
	if ok || retry != 6*time.Minute {
		t.Fatalf("ok=%v retry=%s, ожидалось отклонение и 6 минут до t=10", ok, retry)
	}

	c.advance(6 * time.Minute) // t=10: событие t=0 вышло из окна, t=4 ещё нет
	if ok, _ := l.Allow("k", 2, 10*time.Minute); !ok {
		t.Fatal("после выхода самого раннего события запрос должен пройти")
	}
	if ok, retry := l.Allow("k", 2, 10*time.Minute); ok || retry != 4*time.Minute {
		t.Fatalf("ok=%v retry=%s: теперь ждать до t=14 (4 минуты)", ok, retry)
	}
}

func TestRejectedRequestsDoNotExtendBlock(t *testing.T) {
	c := newClock()
	l := New(c.now)
	l.Allow("k", 1, time.Minute)
	for i := 0; i < 50; i++ {
		c.advance(time.Second)
		if ok, _ := l.Allow("k", 1, time.Minute); ok {
			t.Fatalf("на %d-й секунде лимит ещё действует", i+1)
		}
	}
	c.advance(11 * time.Second) // прошла минута с единственного принятого события
	if ok, _ := l.Allow("k", 1, time.Minute); !ok {
		t.Fatal("отказы не должны были продлевать блокировку")
	}
}

func TestKeysAreIndependent(t *testing.T) {
	l := New(newClock().now)
	l.Allow("a", 1, time.Hour)
	if ok, _ := l.Allow("a", 1, time.Hour); ok {
		t.Fatal("a исчерпан")
	}
	if ok, _ := l.Allow("b", 1, time.Hour); !ok {
		t.Fatal("b не должен зависеть от a")
	}
}

func TestBlockedDoesNotRecordAndHitDoes(t *testing.T) {
	c := newClock()
	l := New(c.now)
	const w = 15 * time.Minute

	for i := 0; i < 10; i++ {
		if blocked, _ := l.Blocked("login", 3, w); blocked {
			t.Fatal("проверка без записи не должна набирать события")
		}
	}
	l.Hit("login", w)
	l.Hit("login", w)
	if blocked, _ := l.Blocked("login", 3, w); blocked {
		t.Fatal("2 события из 3 — ещё не блокировка")
	}
	l.Hit("login", w)
	blocked, retry := l.Blocked("login", 3, w)
	if !blocked || retry != w {
		t.Fatalf("blocked=%v retry=%s", blocked, retry)
	}
	c.advance(w)
	if blocked, _ := l.Blocked("login", 3, w); blocked {
		t.Fatal("после окна блокировка снимается")
	}
}

func TestReset(t *testing.T) {
	l := New(newClock().now)
	l.Hit("k", time.Hour)
	l.Hit("k", time.Hour)
	l.Reset("k")
	if blocked, _ := l.Blocked("k", 1, time.Hour); blocked {
		t.Fatal("после Reset событий быть не должно")
	}
	l.Reset("missing") // не паникует
}

func TestSweepRemovesExpiredKeys(t *testing.T) {
	c := newClock()
	l := New(c.now)
	l.Hit("short", time.Minute)
	l.Hit("long", time.Hour)
	if l.Len() != 2 {
		t.Fatalf("ключей %d", l.Len())
	}
	c.advance(2 * time.Minute)
	if n := l.Sweep(); n != 1 || l.Len() != 1 {
		t.Fatalf("удалено %d, осталось %d: должен уйти только short", n, l.Len())
	}
	c.advance(time.Hour)
	l.Sweep()
	if l.Len() != 0 {
		t.Fatalf("осталось %d ключей", l.Len())
	}
}

func TestSweeperRunsUntilCancelled(t *testing.T) {
	c := newClock()
	l := New(c.now)
	l.Hit("k", time.Millisecond)
	c.advance(time.Second)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { l.RunSweeper(ctx, 5*time.Millisecond); close(done) }()

	deadline := time.After(2 * time.Second)
	for l.Len() != 0 {
		select {
		case <-deadline:
			t.Fatal("уборщик не очистил просроченный ключ")
		case <-time.After(5 * time.Millisecond):
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("уборщик не остановился по отмене контекста")
	}
}

func TestAllowIsAtomicUnderConcurrency(t *testing.T) {
	l := New(nil)
	const limit = 50
	var allowed, denied int
	var mu sync.Mutex
	var wg sync.WaitGroup
	for i := 0; i < 400; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ok, _ := l.Allow("shared", limit, time.Hour)
			mu.Lock()
			if ok {
				allowed++
			} else {
				denied++
			}
			mu.Unlock()
		}()
	}
	wg.Wait()
	if allowed != limit || denied != 400-limit {
		t.Fatalf("прошло %d, отклонено %d; ожидалось ровно %d прошедших", allowed, denied, limit)
	}
}

func TestManyKeysBounded(t *testing.T) {
	c := newClock()
	l := New(c.now)
	for i := 0; i < 10_000; i++ {
		l.Hit("ip:"+strconv.Itoa(i), time.Minute)
	}
	c.advance(time.Minute)
	if n := l.Sweep(); n != 10_000 || l.Len() != 0 {
		t.Fatalf("удалено %d, осталось %d", n, l.Len())
	}
}
