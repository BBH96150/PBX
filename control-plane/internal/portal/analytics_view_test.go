package portal

import (
	"html/template"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/tendpos/sip-platform/control-plane/internal/store"
)

func TestPct(t *testing.T) {
	cases := []struct {
		part, total, want int
	}{
		{0, 0, 0},
		{5, 0, 0},   // div-by-zero guard
		{5, -1, 0},  // negative total guard
		{1, 4, 25},
		{1, 3, 33},  // integer truncation
		{50, 50, 100},
		{3, 2, 150}, // part > total (bar scaling can exceed when total is a max-of-set)
	}
	for _, c := range cases {
		if got := pct(c.part, c.total); got != c.want {
			t.Errorf("pct(%d,%d) = %d, want %d", c.part, c.total, got, c.want)
		}
	}
}

func TestAnalyticsDays(t *testing.T) {
	cases := map[string]int{
		"":     30,
		"30":   30,
		"7":    7,
		"90":   90,
		"99":   30, // unsupported value falls back to default
		"junk": 30,
	}
	for in, want := range cases {
		if got := analyticsDays(in); got != want {
			t.Errorf("analyticsDays(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestLeaderboardScaling(t *testing.T) {
	rows := []store.LabelCount{
		{Label: "+15551112222", Count: 10},
		{Label: "+15553334444", Count: 5},
		{Label: "+15555556666", Count: 0},
	}
	bars := leaderboard(rows)
	if len(bars) != 3 {
		t.Fatalf("got %d bars, want 3", len(bars))
	}
	if bars[0].Pct != 100 || bars[1].Pct != 50 || bars[2].Pct != 0 {
		t.Errorf("unexpected pct scaling: %d/%d/%d", bars[0].Pct, bars[1].Pct, bars[2].Pct)
	}
	// Empty input must not panic or divide by zero.
	if got := leaderboard(nil); len(got) != 0 {
		t.Errorf("leaderboard(nil) = %v, want empty", got)
	}
}

// TestAnalyticsTemplateParseAndRender parses the embedded template tree the same
// way New() does and renders the analytics content block through the layout,
// guarding against template syntax errors and missing-field panics.
func TestAnalyticsTemplateParseAndRender(t *testing.T) {
	srv := &Server{}
	tmpl := template.New("").Funcs(template.FuncMap{
		"deref":       funcs["deref"],
		"dyntemplate": srv.dyntemplate,
		"humandur":    humandur,
		"insightFor":  insightFor,
	})
	if _, err := tmpl.ParseFS(tmplFS, "templates/*.html"); err != nil {
		t.Fatalf("ParseFS: %v", err)
	}
	srv.tmpls = tmpl

	tenant := &store.Tenant{ID: uuid.New(), Name: "Acme"}
	rep := store.CallReport{
		Total: 12, Answered: 9, AvgTalkSec: 95,
		Inbound: 5, Outbound: 6, Internal: 1,
		PerDay: []store.DayCount{{Day: "2026-06-01", Count: 7}, {Day: "2026-06-02", Count: 5}},
	}
	rep.PerHour[9] = 4
	rep.PerHour[14] = 8

	data := map[string]any{
		"Title":       "test",
		"ContentName": "analytics_content",
		"Tenant":      tenant,
		"NavActive":   "analytics",
		"Report":      rep,
		"AnswerRate":  75,
		"TotalTalk":   855,
		"DayBars":     []labelBarView{{Label: "2026-06-01", Count: 7, Pct: 100}, {Label: "2026-06-02", Count: 5, Pct: 71}},
		"HourBars":    []labelBarView{{Label: "09", Count: 4, Pct: 50}, {Label: "14", Count: 8, Pct: 100}},
		"TopCallers":  []labelBarView{{Label: "+15551112222", Count: 6, Pct: 100}},
		"TopDests":    []labelBarView{{Label: "1001", Count: 4, Pct: 100}},
		"Days":        30,
		"DayFrom":     "2026-06-01",
		"DayTo":       "2026-06-02",
	}
	if err := srv.tmpls.ExecuteTemplate(httptest.NewRecorder(), "layout", data); err != nil {
		t.Fatalf("render analytics: %v", err)
	}

	// Empty-window render (no bars / no leaderboards) must also be panic-free.
	empty := map[string]any{
		"Title": "test", "ContentName": "analytics_content",
		"Tenant": tenant, "NavActive": "analytics",
		"Report": store.CallReport{}, "AnswerRate": 0, "TotalTalk": 0,
		"DayBars": []labelBarView{}, "HourBars": []labelBarView{},
		"TopCallers": []labelBarView{}, "TopDests": []labelBarView{},
		"Days": 30, "DayFrom": "", "DayTo": "",
	}
	if err := srv.tmpls.ExecuteTemplate(httptest.NewRecorder(), "layout", empty); err != nil {
		t.Fatalf("render empty analytics: %v", err)
	}
}
