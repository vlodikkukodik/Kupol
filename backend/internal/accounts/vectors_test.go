package accounts

import (
	"encoding/json"
	"os"
	"testing"
)

// loginVector — запись из testdata/login_rules.json (общий файл с фронтендом).
type loginVector struct {
	Input      *string `json:"input"`
	Codepoints []int   `json:"codepoints"`
	OK         bool    `json:"ok"`
	Reserved   bool    `json:"reserved"`
	Note       string  `json:"note"`
}

func (v loginVector) value() string {
	if v.Input != nil {
		return *v.Input
	}
	rs := make([]rune, len(v.Codepoints))
	for i, c := range v.Codepoints {
		rs[i] = rune(c)
	}
	return string(rs)
}

func TestLoginRulesMatchSharedVectors(t *testing.T) {
	raw, err := os.ReadFile("testdata/login_rules.json")
	if err != nil {
		t.Fatal(err)
	}
	var file struct {
		Vectors []loginVector `json:"vectors"`
	}
	if err := json.Unmarshal(raw, &file); err != nil {
		t.Fatal(err)
	}
	if len(file.Vectors) < 30 {
		t.Fatalf("векторов %d: файл повреждён или обрезан", len(file.Vectors))
	}
	for _, v := range file.Vectors {
		in := v.value()
		_, err := NormalizeLogin(in)
		wantOK := v.OK && !v.Reserved
		if (err == nil) != wantOK {
			t.Errorf("%q (%s): ошибка=%v, ожидалась допустимость %v", in, v.Note, err, wantOK)
		}
	}
}
