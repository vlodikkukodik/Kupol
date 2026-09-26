package userdb

import (
	"strings"
	"testing"
)

func TestValidName(t *testing.T) {
	for _, ok := range []string{"a", "blog", "shop2", "0", strings.Repeat("a", 20)} {
		if !ValidName(ok) {
			t.Errorf("%q должно подходить", ok)
		}
	}
	for _, bad := range []string{"", "Blog", "my_db", "my-db", "a b", "имя", "a.b", "a;drop", "a`b", "a'b", "a\"b", strings.Repeat("a", 21), "a\n", "../x", "a/b"} {
		if ValidName(bad) {
			t.Errorf("%q принято", bad)
		}
	}
}

// Полные имена разных пользователей не пересекаются: в имени пользователя нет «_», а часть имени его не содержит.
func TestFullNamesDoNotCollide(t *testing.T) {
	seen := map[string]string{}
	for _, u := range []string{"a", "ab", "a-b", "a1", "john", "j-o-h-n"} {
		for _, n := range []string{"b", "b1", "ab", "blog", "x"} {
			full := FullName(u, n)
			if prev, dup := seen[full]; dup {
				t.Fatalf("%s и %s/%s дают одно имя %s", prev, u, n, full)
			}
			seen[full] = u + "/" + n
			if len(full) > 53 && len(u) <= 32 {
				t.Errorf("слишком длинное имя %q", full)
			}
		}
	}
	if got := FullName("john", "blog"); got != "john_blog" {
		t.Fatal(got)
	}
}

func TestNewPassword(t *testing.T) {
	seen := map[string]bool{}
	for range 50 {
		p, err := NewPassword()
		if err != nil || len(p) != 24 || seen[p] {
			t.Fatalf("%q %v", p, err)
		}
		seen[p] = true
		if strings.ContainsAny(p, "0OIl1'\"\\`; @:/%#?&=+ ") {
			t.Fatalf("в пароле неподходящий символ: %q", p)
		}
	}
}

func TestNormalizeAddr(t *testing.T) {
	good := map[string]string{
		"203.0.113.5": "203.0.113.5", " 203.0.113.5 ": "203.0.113.5", "203.0.113.5/32": "203.0.113.5", "203.0.113.77/24": "203.0.113.0/24",
		"10.0.0.0/25": "10.0.0.0/25", "::ffff:203.0.113.5": "203.0.113.5", "2001:DB8::1": "2001:db8::1", "198.51.100.0/24": "198.51.100.0/24",
	}
	for in, want := range good {
		if got, err := NormalizeAddr(in); err != nil || got != want {
			t.Errorf("%q → %q %v, ожидали %q", in, got, err, want)
		}
	}
	for _, bad := range []string{
		"", " ", "junk", "300.1.1.1", "1.2.3", "0.0.0.0", "0.0.0.0/0", "10.0.0.0/8", "10.0.0.0/23", "127.0.0.1", "::1", "224.0.0.1", "169.254.1.1", "fe80::1", "::",
		"2001:db8::/32", "2001:db8::/64", "fe80::1%eth0", "1.2.3.4;drop", "1.2.3.4,5.6.7.8", "1.2.3.4 5.6.7.8", "1.2.3\n.4", "'1.2.3.4'", "1.2.3.4/abc", "%", "*", "localhost", "example.com",
		strings.Repeat("1", 100),
	} {
		if got, err := NormalizeAddr(bad); err == nil {
			t.Errorf("%q принято как %q", bad, got)
		}
	}
}

func TestNormalizeAddrs(t *testing.T) {
	got, err := NormalizeAddrs([]string{"203.0.113.9", "203.0.113.5", "203.0.113.5/32", " 203.0.113.9 ", "198.51.100.0/24"})
	if err != nil || strings.Join(got, ",") != "198.51.100.0/24,203.0.113.5,203.0.113.9" {
		t.Fatalf("%v %v", got, err)
	}
	if got, err := NormalizeAddrs(nil); err != nil || len(got) != 0 {
		t.Fatalf("пустой список допустим: %v %v", got, err)
	}
	var many []string
	for i := 1; i <= MaxIPs+1; i++ {
		many = append(many, "203.0.113."+itoa(i))
	}
	if _, err := NormalizeAddrs(many); err == nil {
		t.Fatal("больше MaxIPs адресов принято")
	}
	if _, err := NormalizeAddrs([]string{"203.0.113.5", "junk"}); err == nil {
		t.Fatal("мусор в списке принят")
	}
	if _, err := NormalizeAddrs(many[:MaxIPs]); err != nil {
		t.Fatalf("ровно MaxIPs допустимо: %v", err)
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for ; n > 0; n /= 10 {
		b = append([]byte{byte('0' + n%10)}, b...)
	}
	return string(b)
}
