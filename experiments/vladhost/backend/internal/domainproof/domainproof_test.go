package domainproof

import (
	"context"
	"errors"
	"testing"
)

func TestTokenIsRandomAndLongEnough(t *testing.T) {
	seen := map[string]bool{}
	for range 50 {
		tok := Token()
		if len(tok) != 32 || seen[tok] {
			t.Fatalf("код %q", tok)
		}
		seen[tok] = true
	}
}

func TestRecordNameAndValue(t *testing.T) {
	if got := RecordName("Example.COM."); got != "_vladhost-verify.example.com" {
		t.Fatal(got)
	}
	if got := RecordValue("abc"); got != "vladhost-verify=abc" {
		t.Fatal(got)
	}
}

func TestCheck(t *testing.T) {
	lookup := func(records []string, err error) LookupTXT {
		return func(_ context.Context, name string) ([]string, error) {
			if name != "_vladhost-verify.example.com" {
				t.Fatalf("запрошено имя %q", name)
			}
			return records, err
		}
	}
	tok := "0123456789abcdef0123456789abcdef"
	cases := []struct {
		name    string
		records []string
		err     error
		want    bool
	}{
		{"есть", []string{"v=spf1 -all", "vladhost-verify=" + tok}, nil, true},
		{"в кавычках и с пробелами", []string{` "vladhost-verify=` + tok + `" `}, nil, true},
		{"чужой код", []string{"vladhost-verify=deadbeef"}, nil, false},
		{"код как часть другого", []string{"vladhost-verify=" + tok + "x"}, nil, false},
		{"без префикса", []string{tok}, nil, false},
		{"пусто", nil, nil, false},
		{"ошибка DNS вместе с записью", []string{"vladhost-verify=" + tok}, errors.New("timeout"), true},
		{"ошибка DNS", nil, errors.New("timeout"), false},
	}
	for _, tc := range cases {
		if got := Check(context.Background(), lookup(tc.records, tc.err), "example.com", tok); got != tc.want {
			t.Errorf("%s: %v", tc.name, got)
		}
	}
	if Check(context.Background(), lookup([]string{"vladhost-verify="}, nil), "example.com", "") {
		t.Error("пустой код не принимается")
	}
	if Check(context.Background(), nil, "example.com", tok) {
		t.Error("без резолвера — нет")
	}
}
