package audit

import (
	"net/http"
	"testing"
)

func TestFromRequest(t *testing.T) {
	cases := []struct {
		name       string
		remoteAddr string
		xff        string
		ua         string
		wantIP     string
	}{
		{"remote host:port", "203.0.113.5:54321", "", "curl/8", "203.0.113.5"},
		{"xff single", "10.0.0.1:1", "198.51.100.7", "Mozilla", "198.51.100.7"},
		{"xff list takes leftmost", "10.0.0.1:1", "198.51.100.7, 70.0.0.1, 10.0.0.2", "Mozilla", "198.51.100.7"},
		{"remoteaddr without port falls through", "justhost", "", "x", "justhost"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r, _ := http.NewRequest("GET", "/", nil)
			r.RemoteAddr = c.remoteAddr
			if c.xff != "" {
				r.Header.Set("X-Forwarded-For", c.xff)
			}
			r.Header.Set("User-Agent", c.ua)
			ip, ua := FromRequest(r)
			if ip != c.wantIP {
				t.Errorf("ip = %q, want %q", ip, c.wantIP)
			}
			if ua != c.ua {
				t.Errorf("ua = %q, want %q", ua, c.ua)
			}
		})
	}
}

func TestFromRequestNil(t *testing.T) {
	ip, ua := FromRequest(nil)
	if ip != "" || ua != "" {
		t.Errorf("nil request: got (%q,%q), want empty", ip, ua)
	}
}
