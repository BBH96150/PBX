package api

import (
	"encoding/json"
	"net/http"

	"github.com/tendpos/sip-platform/control-plane/internal/store"
)

// Phase 4.4 self-serve signup. Public endpoint (no auth) — creates a fresh
// tenant + the first user inside it, all in one transaction. The user is
// granted tenant_admin on the new tenant via the memberships table.

type signupReq struct {
	CompanyName  string `json:"company_name"`
	Slug         string `json:"slug"`          // optional, auto-derived if empty
	Plan         string `json:"plan"`          // trial|starter|pro|enterprise, default trial
	BillingEmail string `json:"billing_email"` // optional
	BillingPhone string `json:"billing_phone"` // optional

	AdminEmail       string `json:"admin_email"`
	AdminPassword    string `json:"admin_password"`
	AdminDisplayName string `json:"admin_display_name"`
}

type signupResp struct {
	Tenant *store.Tenant `json:"tenant"`
	User   *store.User   `json:"user"`
}

func (s *Server) signup(w http.ResponseWriter, r *http.Request) {
	var req signupReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	t, u, err := s.store.CreateTenantWithAdmin(r.Context(), store.SignupInput{
		CompanyName:      req.CompanyName,
		Slug:             req.Slug,
		Plan:             req.Plan,
		BillingEmail:     req.BillingEmail,
		BillingPhone:     req.BillingPhone,
		AdminEmail:       req.AdminEmail,
		AdminPassword:    req.AdminPassword,
		AdminDisplayName: req.AdminDisplayName,
	})
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, signupResp{Tenant: t, User: u})
}
