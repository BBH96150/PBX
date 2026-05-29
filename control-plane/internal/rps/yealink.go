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

// Yealink is the adapter for Yealink Redirect & Provisioning Service (RPS).
//
// Model: admin pre-creates a "Server" entry in the Yealink RPS / YDMP /
// YMCS portal containing the provisioning URL. Our integration binds MACs
// to that Server. The phone, on first boot, queries Yealink's RPS, gets
// redirected to our URL.
//
// API base + path: production Yealink RPS / YDMP REST endpoints. The exact
// shape varies across the Yealink RPS legacy ("api.yealink.com/rps"), the
// newer YDMP (yms.yealink.com), and the per-region YMCS Cloud APIs.
// This adapter targets the Bearer-token + POST-JSON shape that's common
// across the modern endpoints; double-check the live request against
// Yealink's docs / sandbox before relying on the response payload.
type Yealink struct {
	APIBase    string // e.g. https://yms.yealink.com or https://api.yealink.com
	APIToken   string // Bearer token issued by the Yealink portal
	ServerName string // the pre-created Server entry name in the portal
	HTTPClient *http.Client
}

func NewYealink(apiBase, token, serverName string) *Yealink {
	if apiBase == "" {
		apiBase = "https://yms.yealink.com"
	}
	return &Yealink{
		APIBase:    apiBase,
		APIToken:   token,
		ServerName: serverName,
		HTTPClient: &http.Client{Timeout: 10 * time.Second},
	}
}

func (y *Yealink) Name() string { return "yealink" }

func (y *Yealink) Register(ctx context.Context, req RegisterRequest) error {
	if y.APIToken == "" || y.ServerName == "" {
		return fmtErr("yealink", "register", fmt.Errorf("api_token and server_name required"))
	}
	body, _ := json.Marshal(map[string]any{
		"mac":        MACPlain(req.MAC),
		"serverName": y.ServerName,
		"model":      req.Model,
	})
	r, err := http.NewRequestWithContext(ctx, http.MethodPost,
		y.APIBase+"/api/open/v1/rps/device", bytes.NewReader(body))
	if err != nil {
		return fmtErr("yealink", "build request", err)
	}
	r.Header.Set("Authorization", "Bearer "+y.APIToken)
	r.Header.Set("Content-Type", "application/json")

	resp, err := y.HTTPClient.Do(r)
	if err != nil {
		return fmtErr("yealink", "post device", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusCreated || resp.StatusCode == http.StatusConflict {
		return nil
	}
	snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	return fmtErr("yealink", "post device",
		fmt.Errorf("status %d: %s", resp.StatusCode, string(snippet)))
}

func (y *Yealink) Unregister(ctx context.Context, mac string) error {
	if y.APIToken == "" {
		return fmtErr("yealink", "unregister", fmt.Errorf("api_token required"))
	}
	r, err := http.NewRequestWithContext(ctx, http.MethodDelete,
		y.APIBase+"/api/open/v1/rps/device/"+MACPlain(mac), nil)
	if err != nil {
		return fmtErr("yealink", "build request", err)
	}
	r.Header.Set("Authorization", "Bearer "+y.APIToken)

	resp, err := y.HTTPClient.Do(r)
	if err != nil {
		return fmtErr("yealink", "delete device", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNoContent || resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusNotFound {
		return nil
	}
	snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	return fmtErr("yealink", "delete device",
		fmt.Errorf("status %d: %s", resp.StatusCode, string(snippet)))
}
