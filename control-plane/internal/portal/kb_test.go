package portal

import (
	"io/fs"
	"regexp"
	"strings"
	"testing"
)

var kbLinkRe = regexp.MustCompile(`\]\(([a-z0-9-]+)\.md(?:#[^)]*)?\)`)

// TestKBLinkIntegrity guards the in-app Help Center against broken or orphaned
// articles: every `.md` link (in any article or the index) must resolve to an
// embedded article, and every article must be reachable from the index.
func TestKBLinkIntegrity(t *testing.T) {
	entries, err := fs.ReadDir(kbFS, "kbcontent")
	if err != nil {
		t.Fatalf("ReadDir kbcontent: %v", err)
	}
	articles := map[string]bool{}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".md") {
			slug := strings.TrimSuffix(e.Name(), ".md")
			if slug != "README" {
				articles[slug] = true
			}
		}
	}
	if len(articles) < 20 {
		t.Fatalf("only %d KB articles embedded — kbcontent looks incomplete", len(articles))
	}

	// 1. No broken links anywhere.
	for _, e := range entries {
		raw, err := kbFS.ReadFile("kbcontent/" + e.Name())
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range kbLinkRe.FindAllStringSubmatch(string(raw), -1) {
			target := m[1]
			if target == "README" {
				continue
			}
			if !articles[target] {
				t.Errorf("%s links to missing article %q.md", e.Name(), target)
			}
		}
	}

	// 2. No orphans — every article is linked from the index.
	idx, err := kbFS.ReadFile("kbcontent/README.md")
	if err != nil {
		t.Fatal(err)
	}
	indexed := map[string]bool{}
	for _, m := range kbLinkRe.FindAllStringSubmatch(string(idx), -1) {
		indexed[m[1]] = true
	}
	for slug := range articles {
		if !indexed[slug] {
			t.Errorf("article %q.md is not linked from the Help Center index (README.md)", slug)
		}
	}
}
