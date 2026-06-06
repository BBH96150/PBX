// Package digest sends opt-in daily call-summary emails to tenants that enable
// them. Off by default per tenant; nothing is sent unless a tenant turns it on.
package digest

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/tendpos/sip-platform/control-plane/internal/smtp"
	"github.com/tendpos/sip-platform/control-plane/internal/store"
)

type Sender struct {
	store   *store.Store
	mailer  smtp.Mailer
	hourUTC int
}

func New(st *store.Store, mailer smtp.Mailer, hourUTC int) *Sender {
	if hourUTC < 0 || hourUTC > 23 {
		hourUTC = 13 // ~early morning US, after the day has closed in UTC
	}
	return &Sender{store: st, mailer: mailer, hourUTC: hourUTC}
}

// Run wakes every 20 minutes; once per day at/after the configured UTC hour it
// emails the prior day's summary to each digest-enabled tenant. The DB
// last_digest_on guard makes it idempotent across restarts.
func (d *Sender) Run(ctx context.Context) {
	t := time.NewTimer(2 * time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if time.Now().UTC().Hour() >= d.hourUTC && d.mailer.Configured() {
				d.runDue(ctx)
			}
			t.Reset(20 * time.Minute)
		}
	}
}

func (d *Sender) runDue(ctx context.Context) {
	now := time.Now().UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	yesterday := today.AddDate(0, 0, -1)

	tenants, err := d.store.ListDigestTenantsDue(ctx, today)
	if err != nil {
		slog.Error("digest: list tenants", "err", err)
		return
	}
	for _, t := range tenants {
		rep, err := d.store.GetCallReport(ctx, t.ID, yesterday, today)
		if err != nil {
			slog.Error("digest: report", "tenant", t.ID, "err", err)
			continue
		}
		recipients := d.recipients(ctx, t)
		if len(recipients) > 0 {
			subject := fmt.Sprintf("Daily call summary — %s — %s", t.Name, yesterday.Format("Jan 2, 2006"))
			body := buildDigestBody(t.Name, yesterday, rep)
			for _, to := range recipients {
				if err := d.mailer.Send(to, subject, body, nil); err != nil {
					slog.Error("digest: send", "to", to, "err", err)
				}
			}
		}
		// Mark sent regardless (don't re-attempt all day if there are no
		// recipients yet).
		if err := d.store.MarkDigestSent(ctx, t.ID, today); err != nil {
			slog.Error("digest: mark sent", "tenant", t.ID, "err", err)
		}
	}
}

func (d *Sender) recipients(ctx context.Context, t store.DigestTenant) []string {
	if t.AlertEmail != "" {
		return []string{t.AlertEmail}
	}
	admins, _ := d.store.ListAdminEmailsForTenant(ctx, t.ID)
	return admins
}

// buildDigestBody renders the plain-text digest. Pure — unit-tested.
func buildDigestBody(tenantName string, day time.Time, r store.CallReport) string {
	answerRate := 0
	if r.Total > 0 {
		answerRate = r.Answered * 100 / r.Total
	}
	avg := r.AvgTalkSec
	return "Call summary for " + tenantName + " — " + day.Format("Monday, Jan 2, 2006") + " (UTC)\n\n" +
		"Total calls:   " + strconv.Itoa(r.Total) + "\n" +
		"Answered:      " + strconv.Itoa(r.Answered) + " (" + strconv.Itoa(answerRate) + "%)\n" +
		"Avg talk time: " + strconv.Itoa(avg) + "s\n" +
		"Inbound:       " + strconv.Itoa(r.Inbound) + "\n" +
		"Outbound:      " + strconv.Itoa(r.Outbound) + "\n" +
		"Internal:      " + strconv.Itoa(r.Internal) + "\n\n" +
		"— your PBX\n"
}
