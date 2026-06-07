package webhook

import "testing"

func TestRetriable(t *testing.T) {
	retry := []int{0, 429, 500, 502, 503}
	perm := []int{400, 401, 403, 404, 422}
	for _, c := range retry {
		if !retriable(c) {
			t.Errorf("status %d should be retriable", c)
		}
	}
	for _, c := range perm {
		if retriable(c) {
			t.Errorf("status %d should NOT be retriable", c)
		}
	}
}

func TestGuardAddress(t *testing.T) {
	blocked := []string{
		"127.0.0.1:443", "localhost:443", // localhost resolves? use literal below
		"10.0.0.5:443", "192.168.1.10:443", "172.16.0.1:443",
		"169.254.169.254:80", // cloud metadata
		"0.0.0.0:443",
		"[::1]:443",     // loopback IPv6
		"[fc00::1]:443", // unique-local IPv6
		"[fe80::1]:443", // link-local IPv6
	}
	for _, a := range blocked {
		if err := guardAddress(a); err == nil {
			t.Errorf("guardAddress(%q) = nil, want blocked", a)
		}
	}
	allowed := []string{
		"1.1.1.1:443", "8.8.8.8:443", "93.184.216.34:443",
		"[2606:4700:4700::1111]:443",
	}
	for _, a := range allowed {
		if err := guardAddress(a); err != nil {
			t.Errorf("guardAddress(%q) = %v, want allowed", a, err)
		}
	}
}
