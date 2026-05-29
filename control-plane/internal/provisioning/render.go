package provisioning

import (
	"embed"
	"fmt"
	"io"
	"io/fs"
	"strings"
	"text/template"
)

//go:embed templates/*/*.tmpl
var templateFS embed.FS

// registry holds parsed templates keyed by "vendor/file".
type registry struct {
	tmpls map[string]*template.Template
}

func loadTemplates() (*registry, error) {
	r := &registry{tmpls: map[string]*template.Template{}}
	err := fs.WalkDir(templateFS, "templates", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if !strings.HasSuffix(path, ".tmpl") {
			return nil
		}
		raw, err := fs.ReadFile(templateFS, path)
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}
		key := strings.TrimPrefix(path, "templates/")
		key = strings.TrimSuffix(key, ".tmpl")
		t, err := template.New(key).Funcs(funcs).Parse(string(raw))
		if err != nil {
			return fmt.Errorf("parse %s: %w", path, err)
		}
		r.tmpls[key] = t
		return nil
	})
	if err != nil {
		return nil, err
	}
	return r, nil
}

func (r *registry) execute(w io.Writer, name string, data any) error {
	t, ok := r.tmpls[name]
	if !ok {
		return fmt.Errorf("template %q not found", name)
	}
	return t.Execute(w, data)
}

var funcs = template.FuncMap{
	"upper": strings.ToUpper,
	"lower": strings.ToLower,
	"default": func(def string, v string) string {
		if v == "" {
			return def
		}
		return v
	},
	// polycomTransport maps our generic "udp/tcp/tls" to Polycom's enum.
	"polycomTransport": func(t string) string {
		switch strings.ToLower(t) {
		case "tcp":
			return "TCPOnly"
		case "tls":
			return "TLS"
		default:
			return "UDPOnly"
		}
	},
}
