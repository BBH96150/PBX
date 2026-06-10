//go:build integration

package store

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

// insertLocationRow inserts a minimal valid Kamailio usrloc `location` row for
// (username, domain). Every NOT NULL column on the table has a DEFAULT
// (migration 0028), so we only populate ruid (UNIQUE, to avoid collisions),
// username, domain and contact — the rest take their defaults. A row present
// means the AOR is registered/online.
func insertLocationRow(t *testing.T, s *Store, username, domain string) {
	t.Helper()
	_, err := s.DB.Exec(context.Background(),
		`INSERT INTO location (ruid, username, domain, contact)
		 VALUES ($1, $2, $3, $4)`,
		uuid.NewString(), username, domain, "sip:"+username+"@"+domain)
	if err != nil {
		t.Fatalf("insert location row: %v", err)
	}
}

func TestIntegrationListExtensionPresence(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	ten := makeTenant(t, s)

	// Two active extensions under ONE sip_domain (a tenant may only have one
	// primary domain — makeExtension creates a fresh primary each call, so we
	// build the domain + both extensions explicitly here).
	domain := "pr-" + uuid.NewString()[:8] + ".sip.local"
	sd, err := s.CreateSIPDomain(ctx, ten.ID, domain, true)
	if err != nil {
		t.Fatalf("CreateSIPDomain: %v", err)
	}
	online, err := s.CreateExtension(ctx, ten.ID, sd.ID, "1001", "1001", "pw", "Online User")
	if err != nil {
		t.Fatalf("CreateExtension 1001: %v", err)
	}
	offline, err := s.CreateExtension(ctx, ten.ID, sd.ID, "1002", "1002", "pw", "Offline User")
	if err != nil {
		t.Fatalf("CreateExtension 1002: %v", err)
	}

	// Register only the first extension by inserting a matching location row.
	insertLocationRow(t, s, online.SIPUsername, domain)

	pres, err := s.ListExtensionPresenceForTenant(ctx, ten.ID)
	if err != nil {
		t.Fatalf("ListExtensionPresenceForTenant: %v", err)
	}
	if len(pres) != 2 {
		t.Fatalf("expected 2 extensions, got %d: %+v", len(pres), pres)
	}

	byExt := map[string]ExtensionPresence{}
	for _, p := range pres {
		byExt[p.Extension] = p
	}

	if got := byExt["1001"]; got.Status != "online" {
		t.Errorf("ext 1001: want online, got %q (%+v)", got.Status, got)
	}
	if byExt["1001"].ExtensionID != online.ID {
		t.Errorf("ext 1001 id mismatch: got %v want %v", byExt["1001"].ExtensionID, online.ID)
	}
	if got := byExt["1002"]; got.Status != "offline" {
		t.Errorf("ext 1002: want offline, got %q (%+v)", got.Status, got)
	}
	if byExt["1002"].ExtensionID != offline.ID {
		t.Errorf("ext 1002 id mismatch: got %v want %v", byExt["1002"].ExtensionID, offline.ID)
	}

	// Ordered by extension number.
	if pres[0].Extension != "1001" || pres[1].Extension != "1002" {
		t.Errorf("unexpected order: %q, %q", pres[0].Extension, pres[1].Extension)
	}
}
