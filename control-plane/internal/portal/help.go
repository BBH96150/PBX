package portal

import (
	"bytes"
	"embed"
	"html/template"
	"net/http"
	"regexp"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"

	gmhtml "github.com/yuin/goldmark/renderer/html"
)

// The customer knowledgebase, embedded so the portal can render it as an
// in-app Help Center. Source of truth lives in docs/kb/ at the repo root; this
// is a synced copy (the control-plane is its own Go module and can only embed
// files within its tree).
//
//go:embed kbcontent/*.md
var kbFS embed.FS

// markdown renders GFM with raw-HTML disabled (XSS-safe even though the content
// is trusted/our own).
var markdown = goldmark.New(
	goldmark.WithExtensions(extension.GFM),
	goldmark.WithRendererOptions(gmhtml.WithHardWraps()),
)

var (
	slugRe   = regexp.MustCompile(`^[a-z0-9-]+$`)
	mdLinkRe = regexp.MustCompile(`href="([A-Za-z0-9_-]+)\.md(#[^"]*)?"`)
)

// rewriteKBLinks turns inter-article links (`foo.md`) into portal routes
// (`/admin/help/foo`), and the index (`README.md`) into `/admin/help`.
func rewriteKBLinks(htmlStr string) string {
	return mdLinkRe.ReplaceAllStringFunc(htmlStr, func(m string) string {
		sub := mdLinkRe.FindStringSubmatch(m)
		target, anchor := sub[1], sub[2]
		if target == "README" {
			return `href="/admin/help` + anchor + `"`
		}
		return `href="/admin/help/` + target + anchor + `"`
	})
}

// renderKB reads kbcontent/<file>, renders markdown → HTML, rewrites links.
// Returns ok=false if the file is missing.
func renderKB(file string) (template.HTML, bool) {
	raw, err := kbFS.ReadFile("kbcontent/" + file)
	if err != nil {
		return "", false
	}
	var buf bytes.Buffer
	if err := markdown.Convert(raw, &buf); err != nil {
		return "", false
	}
	return template.HTML(rewriteKBLinks(buf.String())), true //nolint:gosec // trusted content, raw HTML disabled
}

// kbTitle extracts the first H1 ("# Title") for the page title.
func kbTitle(file, fallback string) string {
	raw, err := kbFS.ReadFile("kbcontent/" + file)
	if err != nil {
		return fallback
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.HasPrefix(line, "# ") {
			return strings.TrimSpace(line[2:])
		}
	}
	return fallback
}

// helpIndex renders the knowledgebase index (kbcontent/README.md).
func (s *Server) helpIndex(w http.ResponseWriter, r *http.Request) {
	body, ok := renderKB("README.md")
	if !ok {
		http.Error(w, "help unavailable", http.StatusInternalServerError)
		return
	}
	s.renderLayout(w, r, "Help Center", "help", map[string]any{
		"SelfService": true,
		"Body":        body,
		"IsIndex":     true,
	})
}

// helpArticle renders a single knowledgebase article by slug.
func (s *Server) helpArticle(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	if !slugRe.MatchString(slug) {
		http.NotFound(w, r)
		return
	}
	body, ok := renderKB(slug + ".md")
	if !ok {
		http.NotFound(w, r)
		return
	}
	s.renderLayout(w, r, kbTitle(slug+".md", "Help"), "help", map[string]any{
		"SelfService": true,
		"Body":        body,
	})
}
