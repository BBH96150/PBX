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

// Polycom is the adapter for Polycom (Poly) ZTP — https://api.ztp.poly.com.
//
// Model: an admin pre-creates a "profile" in the Poly ZTP portal that
// contains the provisioning URL + auth scheme. Our integration is to bind
// MACs to that profile; the profile's URL points back at our provisioning
// HTTPS endpoint.
//
// API surface used:
//   POST   {APIBase}/v1/devices    body: {"mac":"<plain>","profile_id":"<id>"}
//   DELETE {APIBase}/v1/devices/{mac}
//   Auth:  Authorization: Bearer <APIToken>
//
// Field names / response shape are based on the public Polycom ZTP docs;
// they should be re-verified against the live sandbox before relying on
// the response payload. Adapter is tagged in logs so failures are obvious.
type Polycom struct {
	APIBase    string
	APIToken   string
	ProfileID  string
	HTTPClient *http.Client
}

// NewPolycom returns an adapter. APIBase defaults to the production
// endpoint when empty.
func NewPolycom(apiBase, token, profileID string) *Polycom {
	if apiBase == "" {
		apiBase = "https://api.ztp.poly.com"
	}
	return &Polycom{
		APIBase:    apiBase,
		APIToken:   token,
		ProfileID:  profileID,
		HTTPClient: &http.Client{Timeout: 10 * time.Second},
	}
}

func (p *Polycom) Name() string { return "polycom" }

func (p *Polycom) Register(ctx context.Context, req RegisterRequest) error {
	if p.APIToken == "" || p.ProfileID == "" {
		return fmtErr("polycom", "register", fmt.Errorf("api_token and profile_id required"))
	}
	body, _ := json.Marshal(map[string]string{
		"mac":        MACPlain(req.MAC),
		"profile_id": p.ProfileID,
	})
	r, err := http.NewRequestWithContext(ctx, http.MethodPost,
		p.APIBase+"/v1/devices", bytes.NewReader(body))
	if err != nil {
		return fmtErr("polycom", "build request", err)
	}
	r.Header.Set("Authorization", "Bearer "+p.APIToken)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Accept", "application/json")

	resp, err := p.HTTPClient.Do(r)
	if err != nil {
		return fmtErr("polycom", "post device", err)
	}
	defer resp.Body.Close()

	// 200/201 = added; 409 = already registered (treat as success — Register
	// is idempotent by contract).
	if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusCreated || resp.StatusCode == http.StatusConflict {
		return nil
	}
	snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	return fmtErr("polycom", "post device",
		fmt.Errorf("status %d: %s", resp.StatusCode, string(snippet)))
}

func (p *Polycom) Unregister(ctx context.Context, mac string) error {
	if p.APIToken == "" {
		return fmtErr("polycom", "unregister", fmt.Errorf("api_token required"))
	}
	r, err := http.NewRequestWithContext(ctx, http.MethodDelete,
		p.APIBase+"/v1/devices/"+MACPlain(mac), nil)
	if err != nil {
		return fmtErr("polycom", "build request", err)
	}
	r.Header.Set("Authorization", "Bearer "+p.APIToken)

	resp, err := p.HTTPClient.Do(r)
	if err != nil {
		return fmtErr("polycom", "delete device", err)
	}
	defer resp.Body.Close()

	// 204 = deleted; 404 = already gone (treat as success).
	if resp.StatusCode == http.StatusNoContent || resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusNotFound {
		return nil
	}
	snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	return fmtErr("polycom", "delete device",
		fmt.Errorf("status %d: %s", resp.StatusCode, string(snippet)))
}
