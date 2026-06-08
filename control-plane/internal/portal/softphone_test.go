package portal

import "testing"

func TestDeriveWebSocketURL(t *testing.T) {
	cases := []struct {
		name, base, want string
	}{
		{"empty → loopback dev", "", "ws://127.0.0.1:5066"},
		{"https → wss via Caddy /ws", "https://pbx.tendpos.com", "wss://pbx.tendpos.com/ws"},
		{"https with port stripped", "https://pbx.tendpos.com:8443", "wss://pbx.tendpos.com/ws"},
		{"https with trailing path", "https://pbx.tendpos.com/admin", "wss://pbx.tendpos.com/ws"},
		{"http dev → ws :5066", "http://localhost:8080", "ws://localhost:5066"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := deriveWebSocketURL(c.base); got != c.want {
				t.Errorf("deriveWebSocketURL(%q) = %q, want %q", c.base, got, c.want)
			}
		})
	}
}

func TestStripPort(t *testing.T) {
	cases := []struct{ in, want string }{
		{"host.example.com", "host.example.com"},
		{"host.example.com:5066", "host.example.com"},
		{"host.example.com/path", "host.example.com"},
		{"host.example.com:443/path", "host.example.com"},
	}
	for _, c := range cases {
		if got := stripPort(c.in); got != c.want {
			t.Errorf("stripPort(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
