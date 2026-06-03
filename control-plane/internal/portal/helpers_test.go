package portal

import "testing"

func TestHumandur(t *testing.T) {
	i := func(n int) *int { return &n }
	cases := []struct {
		in   any
		want string
	}{
		{nil, "—"},
		{(*int)(nil), "—"},
		{i(0), "—"},
		{i(-5), "—"},
		{i(7), "7s"},
		{i(65), "1m05s"},
		{i(3600), "1h00m"},
		{i(3725), "1h02m"},
		{42, "42s"},
	}
	for _, c := range cases {
		if got := humandur(c.in); got != c.want {
			t.Errorf("humandur(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestCSVSafe(t *testing.T) {
	cases := map[string]string{
		"":             "",
		"hello":        "hello",
		"+14155551234": "'+14155551234",
		"=1+1":         "'=1+1",
		"-cmd":         "'-cmd",
		"@SUM(A1)":     "'@SUM(A1)",
		"Front Desk":   "Front Desk",
	}
	for in, want := range cases {
		if got := csvSafe(in); got != want {
			t.Errorf("csvSafe(%q) = %q, want %q", in, got, want)
		}
	}
}
