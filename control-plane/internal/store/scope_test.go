package store

import "testing"

func TestScopeForRole(t *testing.T) {
	cases := map[string]string{
		"super_admin":  "admin",
		"tenant_admin": "admin",
		"user":         "write",
		"viewer":       "read", // anything else
		"":             "read",
	}
	for role, want := range cases {
		if got := ScopeForRole(role); got != want {
			t.Errorf("ScopeForRole(%q) = %q, want %q", role, got, want)
		}
	}
}
