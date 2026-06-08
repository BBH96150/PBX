package portal

import (
	"strings"
	"testing"
)

func TestRewriteKBLinks(t *testing.T) {
	cases := []struct{ in, want string }{
		{`href="getting-started-overview.md"`, `href="/admin/help/getting-started-overview"`},
		{`href="README.md"`, `href="/admin/help"`},
		{`href="paging-groups.md#modes"`, `href="/admin/help/paging-groups#modes"`},
		{`href="https://example.com/x"`, `href="https://example.com/x"`}, // external untouched
		{`href="/admin/me"`, `href="/admin/me"`},                          // non-md untouched
	}
	for _, c := range cases {
		if got := rewriteKBLinks(c.in); got != c.want {
			t.Errorf("rewriteKBLinks(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestSlugRegexp(t *testing.T) {
	good := []string{"paging-groups", "admin-two-factor", "first-time-setup"}
	bad := []string{"../etc/passwd", "Foo", "a/b", "a.md", "a b"}
	for _, s := range good {
		if !slugRe.MatchString(s) {
			t.Errorf("slug %q should be valid", s)
		}
	}
	for _, s := range bad {
		if slugRe.MatchString(s) {
			t.Errorf("slug %q should be rejected (traversal/format)", s)
		}
	}
}

func TestRenderKBIndexAndArticle(t *testing.T) {
	// The embedded index renders and its .md links are rewritten to routes.
	body, ok := renderKB("README.md")
	if !ok {
		t.Fatal("index README.md should render")
	}
	h := string(body)
	if !strings.Contains(h, "/admin/help/") {
		t.Error("rendered index should contain rewritten /admin/help/ links")
	}
	if strings.Contains(h, `.md"`) {
		t.Error("rendered index still has raw .md links")
	}

	// A known article renders with an <h1> and a non-empty title.
	if _, ok := renderKB("paging-groups.md"); !ok {
		t.Error("paging-groups.md should render")
	}
	if title := kbTitle("paging-groups.md", "fallback"); title == "" || title == "fallback" {
		t.Errorf("kbTitle should extract the H1, got %q", title)
	}

	// Missing file → not ok.
	if _, ok := renderKB("does-not-exist.md"); ok {
		t.Error("missing article should return ok=false")
	}
}
