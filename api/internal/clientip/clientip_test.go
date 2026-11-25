package clientip

import (
	"net/http"
	"testing"
)

func req(remote, xff string) *http.Request {
	r, _ := http.NewRequest("GET", "/", nil)
	r.RemoteAddr = remote
	if xff != "" {
		r.Header.Set("X-Forwarded-For", xff)
	}
	return r
}

func TestIP(t *testing.T) {
	res, err := New([]string{"10.0.0.0/8", "192.168.1.1"})
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct{ name, remote, xff, want string }{
		{"untrusted peer ignores header", "203.0.113.9:5555", "1.2.3.4", "203.0.113.9"},
		{"trusted peer uses header", "10.1.2.3:80", "198.51.100.7", "198.51.100.7"},
		{"spoofed prefix skipped", "10.1.2.3:80", "6.6.6.6, 198.51.100.7, 10.9.9.9", "198.51.100.7"},
		{"all trusted falls back to peer", "10.1.2.3:80", "10.5.5.5", "10.1.2.3"},
		{"malformed header falls back", "10.1.2.3:80", "garbage", "10.1.2.3"},
		{"bare ip trusted", "192.168.1.1:1", "198.51.100.8", "198.51.100.8"},
		{"hop with port", "10.1.2.3:80", "198.51.100.7:4444", "198.51.100.7"},
		{"no header", "10.1.2.3:80", "", "10.1.2.3"},
	}
	for _, c := range cases {
		if got := res.IP(req(c.remote, c.xff)); got != c.want {
			t.Errorf("%s: got %q want %q", c.name, got, c.want)
		}
	}
	none, _ := New(nil)
	if got := none.IP(req("10.1.2.3:80", "1.1.1.1")); got != "10.1.2.3" {
		t.Errorf("no trusted proxies must ignore XFF, got %q", got)
	}
	if _, err := New([]string{"nope"}); err == nil {
		t.Error("expected error for bad entry")
	}
}
