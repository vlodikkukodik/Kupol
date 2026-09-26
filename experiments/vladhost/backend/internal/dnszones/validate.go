package dnszones

import (
	"net/netip"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Правила записей — те же, что проверяет dns-sync.py на сервере: панель отказывает раньше и понятнее, сервер не верит панели.
const label = `[a-z0-9_]([a-z0-9_-]{0,61}[a-z0-9_])?`

var (
	domainRe = regexp.MustCompile(`^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z][a-z0-9-]{0,61}[a-z0-9]$`)
	nameRe   = regexp.MustCompile(`^(\*\.)?` + label + `(\.` + label + `)*$`)
	hostRe   = regexp.MustCompile(`^([a-z0-9_]([a-z0-9_-]{0,61}[a-z0-9_])?\.)+[a-z][a-z0-9-]{0,61}[a-z0-9]$`)
)

// Допустимые значения.
var (
	Types = []string{"A", "AAAA", "CNAME", "MX", "TXT", "SRV", "CAA"}
	TTLs  = []int{60, 120, 300, 600, 900, 1800, 3600, 7200, 14400, 43200, 86400}
)

const (
	maxTXT    = 2048
	maxNameLn = 253
)

// RecordInput — запись, как её присылает пользователь.
type RecordInput struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Value    string `json:"value"`
	Priority int    `json:"priority"`
	TTL      int    `json:"ttl"`
}

func validDomain(d string) bool {
	return len(d) <= maxNameLn && !strings.Contains(d, "..") && domainRe.MatchString(d)
}

func inTTLs(v int) bool {
	for _, t := range TTLs {
		if t == v {
			return true
		}
	}
	return false
}

func printable(v string, limit int) bool {
	if v == "" || len(v) > limit || !utf8.ValidString(v) {
		return false
	}
	for _, r := range v {
		if r < 32 || r == 127 {
			return false
		}
	}
	return true
}

// normalize приводит запись к каноническому виду и проверяет её; zone — домен зоны (для длины имени).
func normalize(in RecordInput, zone string) (RecordInput, error) {
	in.Type = strings.ToUpper(strings.TrimSpace(in.Type))
	in.Name = strings.ToLower(strings.TrimSpace(in.Name))
	if in.Name == "" {
		in.Name = "@"
	}
	if in.TTL == 0 {
		in.TTL = 300
	}
	known := false
	for _, t := range Types {
		known = known || t == in.Type
	}
	if !known {
		return in, ErrType
	}
	if in.Name != "@" && (!nameRe.MatchString(in.Name) || strings.Contains(in.Name, "..") || len(in.Name)+len(zone) >= maxNameLn) {
		return in, ErrName
	}
	if !inTTLs(in.TTL) {
		return in, ErrTTL
	}
	in.Value = strings.TrimSpace(in.Value)
	if in.Type != "MX" && in.Type != "SRV" {
		in.Priority = 0
	} else if in.Priority < 0 || in.Priority > 65535 {
		return in, ErrPriority
	}
	switch in.Type {
	case "A":
		ip, err := netip.ParseAddr(in.Value)
		if err != nil || !ip.Is4() || ip.IsUnspecified() || ip.IsMulticast() || ip.String() != in.Value {
			return in, ErrValue
		}
	case "AAAA":
		ip, err := netip.ParseAddr(in.Value)
		if err != nil || !ip.Is6() || ip.Is4In6() || ip.IsUnspecified() || ip.IsMulticast() {
			return in, ErrValue
		}
		in.Value = ip.String()
	case "CNAME":
		in.Value = strings.TrimSuffix(strings.ToLower(in.Value), ".")
		if in.Name == "@" || !hostRe.MatchString(in.Value) {
			if in.Name == "@" {
				return in, ErrCNAMEApex
			}
			return in, ErrValue
		}
	case "MX":
		in.Value = strings.TrimSuffix(strings.ToLower(in.Value), ".")
		if !hostRe.MatchString(in.Value) {
			return in, ErrValue
		}
	case "TXT":
		if !printable(in.Value, maxTXT) {
			return in, ErrValue
		}
	case "SRV":
		parts := strings.Split(strings.ToLower(in.Value), " ")
		if len(parts) != 3 {
			return in, ErrValue
		}
		w, err1 := strconv.Atoi(parts[0])
		p, err2 := strconv.Atoi(parts[1])
		if err1 != nil || err2 != nil || w < 0 || w > 65535 || p < 1 || p > 65535 || strconv.Itoa(w) != parts[0] || strconv.Itoa(p) != parts[1] {
			return in, ErrValue
		}
		target := strings.TrimSuffix(parts[2], ".")
		if parts[2] != "." && !hostRe.MatchString(target) {
			return in, ErrValue
		}
		if parts[2] == "." {
			target = "."
		}
		in.Value = parts[0] + " " + parts[1] + " " + target
	case "CAA":
		parts := strings.SplitN(in.Value, " ", 3)
		if len(parts) != 3 || (parts[0] != "0" && parts[0] != "128") || !(parts[1] == "issue" || parts[1] == "issuewild" || parts[1] == "iodef") ||
			!printable(parts[2], 255) || strings.Contains(strings.TrimSpace(parts[2]), " ") {
			return in, ErrValue
		}
		in.Value = parts[0] + " " + parts[1] + " " + strings.TrimSpace(parts[2])
	}
	return in, nil
}

// relativeName переводит полное имя в имя внутри зоны: «vh1._domainkey.example.com» в зоне example.com → «vh1._domainkey».
func relativeName(fqdn, zone string) (string, bool) {
	fqdn = strings.ToLower(strings.TrimSuffix(fqdn, "."))
	if fqdn == zone {
		return "@", true
	}
	if strings.HasSuffix(fqdn, "."+zone) {
		return strings.TrimSuffix(fqdn, "."+zone), true
	}
	return "", false
}
