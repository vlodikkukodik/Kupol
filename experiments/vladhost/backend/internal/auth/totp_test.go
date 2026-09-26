package auth

import (
	"testing"
	"time"
)

// Контрольные значения RFC 6238 (приложение B, SHA-1), последние 6 цифр.
func TestTOTPMatchesRFC6238(t *testing.T) {
	secret := []byte("12345678901234567890")
	for _, tc := range []struct {
		unix int64
		want string
	}{
		{59, "287082"},
		{1111111109, "081804"},
		{1111111111, "050471"},
		{1234567890, "005924"},
		{2000000000, "279037"},
	} {
		if got := totpCode(secret, tc.unix/totpPeriod); got != tc.want {
			t.Errorf("t=%d: %s, нужно %s", tc.unix, got, tc.want)
		}
	}
}

func TestMatchTOTPWindowAndReplay(t *testing.T) {
	secret := []byte("12345678901234567890")
	now := time.Unix(1_700_000_000, 0)
	cur := now.Unix() / totpPeriod
	for d, ok := range map[int64]bool{-2: false, -1: true, 0: true, 1: true, 2: false} {
		_, got := matchTOTP(secret, totpCode(secret, cur+d), now, 0)
		if got != ok {
			t.Errorf("сдвиг %d: %v, нужно %v", d, got, ok)
		}
	}
	// Уже принятый шаг и более ранние не принимаются повторно.
	if _, ok := matchTOTP(secret, totpCode(secret, cur), now, cur); ok {
		t.Error("повтор кода принят")
	}
	if step, ok := matchTOTP(secret, totpCode(secret, cur+1), now, cur); !ok || step != cur+1 {
		t.Error("следующий шаг должен приниматься")
	}
}

func TestSealOpen(t *testing.T) {
	s := &Service{secret: []byte("secret")}
	sealed, err := s.seal([]byte("key"))
	if err != nil {
		t.Fatal(err)
	}
	if got, err := s.open(sealed); err != nil || string(got) != "key" {
		t.Fatalf("%q %v", got, err)
	}
	if _, err := (&Service{secret: []byte("other")}).open(sealed); err == nil {
		t.Fatal("другой ключ не должен расшифровывать")
	}
}

func TestTicketsExpireAndLimitTries(t *testing.T) {
	var ts ticketStore
	now := time.Now()
	raw, _ := ts.put(7, now)
	for i := range ticketTries {
		if uid, ok := ts.take(raw, now); !ok || uid != 7 {
			t.Fatalf("попытка %d отклонена", i+1)
		}
	}
	if _, ok := ts.take(raw, now); ok {
		t.Fatal("попытки должны кончиться")
	}
	raw, _ = ts.put(7, now)
	if _, ok := ts.take(raw, now.Add(ticketTTL+time.Second)); ok {
		t.Fatal("просроченный билет принят")
	}
}

func TestNormalizeCode(t *testing.T) {
	if normalizeCode(" 123 456 ") != "123456" || normalizeCode("ABCDE-fghij") != "abcdefghij" {
		t.Fatal("нормализация кода")
	}
}
