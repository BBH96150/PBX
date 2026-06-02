package portal

import (
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// deviceDetail shows a device, its bound lines, and a form to bind an
// extension to a line key — the portal half of zero-touch provisioning.
func (s *Server) deviceDetail(w http.ResponseWriter, r *http.Request) {
	tid, ok := s.parseTenantParam(w, r)
	if !ok {
		return
	}
	mac, err := url.PathUnescape(chi.URLParam(r, "mac"))
	if err != nil {
		http.Error(w, "bad mac", 400)
		return
	}
	tenant, err := s.store.GetTenant(r.Context(), tid)
	if err != nil {
		s.errPage(w, r, err)
		return
	}
	device, err := s.store.GetDevice(r.Context(), mac)
	if err != nil {
		s.errPage(w, r, err)
		return
	}
	if device.TenantID != tid {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	lines, _ := s.store.ListDeviceLinesDetailed(r.Context(), device.MAC)
	exts := mustExtensions(r.Context(), s.store, tid)

	s.renderLayout(w, r, tenant.Name+" · "+device.MAC, "device", map[string]any{
		"Tenant":     tenant,
		"Device":     device,
		"Lines":      lines,
		"Extensions": exts,
	})
}

func (s *Server) deviceLineAdd(w http.ResponseWriter, r *http.Request) {
	tid, ok := s.parseTenantParam(w, r)
	if !ok {
		return
	}
	mac, err := url.PathUnescape(chi.URLParam(r, "mac"))
	if err != nil {
		http.Error(w, "bad mac", 400)
		return
	}
	redirect := "/admin/tenants/" + tid.String() + "/devices/" + url.PathEscape(mac)

	// Confirm the device belongs to this tenant before touching its lines.
	device, err := s.store.GetDevice(r.Context(), mac)
	if err != nil || device.TenantID != tid {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", 400)
		return
	}
	lineNumber := atoiOr(r.FormValue("line_number"), 0)
	if lineNumber < 1 || lineNumber > 32 {
		s.flashErr(w, r, redirect, errors.New("line number must be between 1 and 32"))
		return
	}
	extID, err := uuid.Parse(r.FormValue("extension_id"))
	if err != nil {
		s.flashErr(w, r, redirect, errors.New("pick an extension to bind"))
		return
	}
	if _, err := s.store.CreateDeviceLine(r.Context(), device.MAC, lineNumber, extID,
		strings.TrimSpace(r.FormValue("label"))); err != nil {
		s.flashErr(w, r, redirect, friendlyDupErr(err, "that line number is already bound on this device"))
		return
	}
	s.auditNested(r, tid, "device.line.bound", "device_line", nil,
		map[string]any{"device_mac": device.MAC, "line_number": lineNumber, "extension_id": extID.String()})
	http.Redirect(w, r, redirect+"?flash=Line+bound.", http.StatusSeeOther)
}

func (s *Server) deviceLineRemove(w http.ResponseWriter, r *http.Request) {
	tid, ok := s.parseTenantParam(w, r)
	if !ok {
		return
	}
	mac, err := url.PathUnescape(chi.URLParam(r, "mac"))
	if err != nil {
		http.Error(w, "bad mac", 400)
		return
	}
	lineID, err := uuid.Parse(chi.URLParam(r, "lineID"))
	if err != nil {
		http.Error(w, "bad line id", 400)
		return
	}
	redirect := "/admin/tenants/" + tid.String() + "/devices/" + url.PathEscape(mac)
	if err := s.store.DeleteDeviceLineForTenant(r.Context(), tid, lineID); err != nil {
		s.flashErr(w, r, redirect, err)
		return
	}
	s.auditNested(r, tid, "device.line.unbound", "device_line", &lineID, nil)
	http.Redirect(w, r, redirect+"?flash=Line+unbound.", http.StatusSeeOther)
}
