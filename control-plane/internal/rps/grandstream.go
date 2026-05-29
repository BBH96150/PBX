package rps

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Grandstream is the adapter for Grandstream Device Management System
// (GDMS) at https://www.gdms.cloud.
//
// Model: an admin pre-creates a "site" (or organization) in GDMS and
// configures the provisioning URL. Our integration adds devices into that
// site, which causes GDMS to populate the device's redirection cache.
//
// Auth: GDMS uses OAuth2 client-credentials in production. For brevity,
// this adapter takes a long-lived API token as a Bearer header — same
// shape as Polycom/Yealink. Swap in a real OAuth2 token refresher when
// going to prod (golang.org/x/oauth2/clientcredentials).
//
// Wire format: based on the public GDMS REST docs. Verify against the
// live sandbox before relying on the response payload.
type Grandstream struct {
	APIBase    string // typically https://www.gdms.cloud
	APIToken   string
	OrgID      string // GDMS organization / site ID
	HTTPClient *http.Client
}

func NewGrandstream(apiBase, token, orgID string) *Grandstream {
	if apiBase == "" {
		apiBase = "https://www.gdms.cloud"
	}
	return &Grandstream{
		APIBase:    apiBase,
		APIToken:   token,
		OrgID:      orgID,
		HTTPClient: &http.Client{Timeout: 10 * time.Second},
	}
}

func (g *Grandstream) Name() string { return "grandstream" }

func (g *Grandstream) Register(ctx context.Context, req RegisterRequest) error {
	if g.APIToken == "" || g.OrgID == "" {
		return fmtErr("grandstream", "register", fmt.Errorf("api_token and org_id required"))
	}
	body, _ := json.Marshal(map[string]any{
		"mac":   MACPlain(req.MAC),
		"orgId": g.OrgID,
		"model": req.Model,
	})
	r, err := http.NewRequestWithContext(ctx, http.MethodPost,
		g.APIBase+"/api/v1.0.0/device/data/add", bytes.NewReader(body))
	if err != nil {
		return fmtErr("grandstream", "build request", err)
	}
	r.Header.Set("Authorization", "Bearer "+g.APIToken)
	r.Header.Set("Content-Type", "application/json")

	resp, err := g.HTTPClient.Do(r)
	if err != nil {
		return fmtErr("grandstream", "post device", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusCreated || resp.StatusCode == http.StatusConflict {
		return nil
	}
	snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	return fmtErr("grandstream", "post device",
		fmt.Errorf("status %d: %s", resp.StatusCode, string(snippet)))
}

func (g *Grandstream) Unregister(ctx context.Context, mac string) error {
	if g.APIToken == "" {
		return fmtErr("grandstream", "unregister", fmt.Errorf("api_token required"))
	}
	body, _ := json.Marshal(map[string]string{"mac": MACPlain(mac)})
	r, err := http.NewRequestWithContext(ctx, http.MethodPost,
		g.APIBase+"/api/v1.0.0/device/data/delete", bytes.NewReader(body))
	if err != nil {
		return fmtErr("grandstream", "build request", err)
	}
	r.Header.Set("Authorization", "Bearer "+g.APIToken)
	r.Header.Set("Content-Type", "application/json")

	resp, err := g.HTTPClient.Do(r)
	if err != nil {
		return fmtErr("grandstream", "delete device", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusNoContent || resp.StatusCode == http.StatusNotFound {
		return nil
	}
	snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	return fmtErr("grandstream", "delete device",
		fmt.Errorf("status %d: %s", resp.StatusCode, string(snippet)))
}
