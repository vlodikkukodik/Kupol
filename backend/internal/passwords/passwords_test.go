package passwords

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// Лёгкие параметры только чтобы тесты шли быстро; DefaultParams проверяется отдельно.
var fast = Params{MemoryKiB: 64, Iterations: 1, Parallelism: 1}

func newHasher(t *testing.T, p Params) *Hasher {
	t.Helper()
	h, err := NewHasher(p, 4)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func TestHashAndVerify(t *testing.T) {
	h := newHasher(t, fast)
	ctx := context.Background()

	enc, err := h.Hash(ctx, "Изделие К-19 — секрет")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(enc, "$argon2id$v=19$m=64,t=1,p=1$") {
		t.Fatalf("формат: %s", enc)
	}

	ok, rehash, err := h.Verify(ctx, "Изделие К-19 — секрет", enc)
	if err != nil || !ok || rehash {
		t.Fatalf("верный пароль: ok=%v rehash=%v err=%v", ok, rehash, err)
	}
	for _, wrong := range []string{"", "изделие к-19 — секрет", "Изделие К-19 — секрет ", "x"} {
		if ok, _, err := h.Verify(ctx, wrong, enc); err != nil || ok {
			t.Errorf("неверный пароль %q принят: ok=%v err=%v", wrong, ok, err)
		}
	}
}

func TestSaltIsRandom(t *testing.T) {
	h := newHasher(t, fast)
	a, _ := h.Hash(context.Background(), "same")
	b, _ := h.Hash(context.Background(), "same")
	if a == b {
		t.Fatal("одинаковые пароли дали одинаковый хеш — соль не случайна")
	}
}

func TestDefaultParamsWork(t *testing.T) {
	h := newHasher(t, DefaultParams)
	enc, err := h.Hash(context.Background(), "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(enc, "m=19456,t=2,p=1") {
		t.Fatalf("параметры по умолчанию: %s", enc)
	}
	if ok, rehash, err := h.Verify(context.Background(), "correct horse battery staple", enc); err != nil || !ok || rehash {
		t.Fatalf("ok=%v rehash=%v err=%v", ok, rehash, err)
	}
}

func TestNeedsRehashWhenParamsWeaker(t *testing.T) {
	old := newHasher(t, Params{MemoryKiB: 32, Iterations: 1, Parallelism: 1})
	enc, _ := old.Hash(context.Background(), "pw")

	cur := newHasher(t, fast) // «текущие» параметры сильнее
	ok, rehash, err := cur.Verify(context.Background(), "pw", enc)
	if err != nil || !ok || !rehash {
		t.Fatalf("ожидалось ok+rehash: ok=%v rehash=%v err=%v", ok, rehash, err)
	}
	// неверный пароль rehash не требует
	if ok, rehash, _ := cur.Verify(context.Background(), "nope", enc); ok || rehash {
		t.Fatalf("неверный пароль: ok=%v rehash=%v", ok, rehash)
	}
}

func TestVerifyRejectsMalformedHashes(t *testing.T) {
	h := newHasher(t, fast)
	good, _ := h.Hash(context.Background(), "pw")
	parts := strings.Split(good, "$")

	bad := map[string]string{
		"пусто":                "",
		"мусор":                "not a hash",
		"другой алгоритм":      "$argon2i$v=19$m=64,t=1,p=1$" + parts[4] + "$" + parts[5],
		"другая версия":        "$argon2id$v=16$m=64,t=1,p=1$" + parts[4] + "$" + parts[5],
		"мало полей":           "$argon2id$v=19$m=64,t=1$" + parts[4] + "$" + parts[5],
		"порядок параметров":   "$argon2id$v=19$t=1,m=64,p=1$" + parts[4] + "$" + parts[5],
		"нечисловой параметр":  "$argon2id$v=19$m=x,t=1,p=1$" + parts[4] + "$" + parts[5],
		"бомба по памяти":      "$argon2id$v=19$m=4194304,t=1,p=1$" + parts[4] + "$" + parts[5],
		"бомба по проходам":    "$argon2id$v=19$m=64,t=1000,p=1$" + parts[4] + "$" + parts[5],
		"нулевые проходы":      "$argon2id$v=19$m=64,t=0,p=1$" + parts[4] + "$" + parts[5],
		"мало памяти":          "$argon2id$v=19$m=4,t=1,p=1$" + parts[4] + "$" + parts[5],
		"соль не base64":       "$argon2id$v=19$m=64,t=1,p=1$!!!$" + parts[5],
		"короткая соль":        "$argon2id$v=19$m=64,t=1,p=1$AAAA$" + parts[5],
		"короткий ключ":        "$argon2id$v=19$m=64,t=1,p=1$" + parts[4] + "$AAAA",
		"лишний хвост":         good + "$x",
		"параллелизм > памяти": "$argon2id$v=19$m=8,t=1,p=8$" + parts[4] + "$" + parts[5],
	}
	for name, enc := range bad {
		ok, _, err := h.Verify(context.Background(), "pw", enc)
		if ok || !errors.Is(err, ErrInvalidHash) {
			t.Errorf("%s: ok=%v err=%v, ожидалась ErrInvalidHash", name, ok, err)
		}
	}
}

func TestVerifyDummyDoesRealWorkAndNeverMatches(t *testing.T) {
	h := newHasher(t, DefaultParams)
	// Холостая проверка должна быть настоящим вычислением: сопоставима по времени с обычной.
	enc, _ := h.Hash(context.Background(), "pw")

	timeit := func(f func()) time.Duration {
		best := time.Hour
		for i := 0; i < 3; i++ {
			s := time.Now()
			f()
			if d := time.Since(s); d < best {
				best = d
			}
		}
		return best
	}
	real := timeit(func() { _, _, _ = h.Verify(context.Background(), "nope", enc) })
	dummy := timeit(func() { _ = h.VerifyDummy(context.Background(), "nope") })
	// допускаем двукратное расхождение: отличить «нет логина» по времени нельзя, а тест не хрупкий
	if dummy < real/2 || dummy > real*2 {
		t.Fatalf("время холостой проверки %s сильно отличается от настоящей %s", dummy, real)
	}
}

func TestConcurrencyLimitHonoursContext(t *testing.T) {
	h, err := NewHasher(fast, 1)
	if err != nil {
		t.Fatal(err)
	}
	h.sem <- struct{}{} // единственное место занято

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := h.Hash(ctx, "pw"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Hash при занятом хешере: %v", err)
	}
	if _, _, err := h.Verify(ctx, "pw", h.dummy); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Verify при занятом хешере: %v", err)
	}

	<-h.sem // освободили
	if _, err := h.Hash(context.Background(), "pw"); err != nil {
		t.Fatalf("после освобождения: %v", err)
	}
}

func TestNewHasherValidatesParams(t *testing.T) {
	if _, err := NewHasher(fast, 0); err == nil {
		t.Error("maxConcurrent=0 должен отвергаться")
	}
	for _, p := range []Params{{0, 1, 1}, {64, 0, 1}, {64, 1, 0}, {4, 1, 1}} {
		if _, err := NewHasher(p, 1); err == nil {
			t.Errorf("параметры %+v должны отвергаться", p)
		}
	}
}
