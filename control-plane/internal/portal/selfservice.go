package portal

import (
	"errors"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/tendpos/sip-platform/control-plane/internal/store"
)

// errBadEmail is shown when the vm-email recipient is non-empty but malformed.
var errBadEmail = errors.New("voicemail notification email must contain @")

// currentUser resolves the logged-in end-user from the session token, or nil.
func (s *Server) currentUser(r *http.Request) *store.User {
	tok := tokenFromCtx(r.Context())
	if tok == nil {
		return nil
	}
	u, _ := s.userFromSessionToken(r.Context(), tok)
	return u
}

// ownedExtension resolves {extensionID} and hard-verifies that the logged-in
// user OWNS it (extension.user_id == user.id). This is the only authorization
// the self-service area relies on — never trust the URL id. Writes the HTTP
// error itself and returns ok=false on any failure.
func (s *Server) ownedExtension(w http.ResponseWriter, r *http.Request) (*store.Extension, *store.User, bool) {
	u := s.currentUser(r)
	if u == nil {
		http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
		return nil, nil, false
	}
	id, err := uuid.Parse(chi.URLParam(r, "extensionID"))
	if err != nil {
		http.Error(w, "bad extension id", http.StatusBadRequest)
		return nil, nil, false
	}
	ext, err := s.lookupExtensionByID(r.Context(), id)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return nil, nil, false
	}
	if !userOwnsExtension(ext, u) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return nil, nil, false
	}
	return ext, u, true
}

// userOwnsExtension is the pure ownership predicate the self-service guard
// relies on: the extension must have an owner set AND it must be exactly this
// user. A same-tenant non-owner (or a cross-tenant user) fails — tenant match
// is deliberately NOT sufficient, which is what closes the prior authz gap.
func userOwnsExtension(ext *store.Extension, u *store.User) bool {
	return ext != nil && u != nil && ext.UserID != nil && *ext.UserID == u.ID
}

// meHome lists the user's own extensions. One → straight to it; none → an empty
// state; several → a chooser.
func (s *Server) meHome(w http.ResponseWriter, r *http.Request) {
	u := s.currentUser(r)
	if u == nil {
		http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
		return
	}
	exts, _ := s.store.FindOwnedExtensions(r.Context(), u.ID)
	// Personal speed dials render on the home page alongside the extension list
	// (so a user with a single extension still sees them, we DON'T auto-redirect
	// when they have favorites — only when there's exactly one extension and the
	// page would otherwise be a bare chooser). Keep the existing single-extension
	// shortcut, but still surface speed dials there too by rendering home.
	dials, _ := s.store.ListSpeedDialsForUser(r.Context(), u.ID)
	if len(exts) == 1 && len(dials) == 0 {
		http.Redirect(w, r, "/admin/me/extensions/"+exts[0].ID.String(), http.StatusSeeOther)
		return
	}
	s.renderLayout(w, r, "My extensions", "me", map[string]any{
		"SelfService": true,
		"Extensions":  exts,
		"SpeedDials":  dials,
		"SessionUser": u,
	})
}

// meSpeedDialCreate adds a personal speed dial for the SESSION user. The user_id
// is derived from the session — never from the request body. The number is run
// through the same sanitizeDialTarget guard the CALL button uses, so only a
// dialable target can be stored.
func (s *Server) meSpeedDialCreate(w http.ResponseWriter, r *http.Request) {
	u := s.currentUser(r)
	if u == nil {
		http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
		return
	}
	if u.TenantID == nil {
		// Speed dials are tenant-scoped for integrity; a super-admin (no tenant)
		// has no self-service extensions and thus no speed dials.
		http.Error(w, "speed dials require a tenant", http.StatusBadRequest)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	redirect := "/admin/me"
	label := strings.TrimSpace(r.FormValue("label"))
	if label == "" {
		s.flashErr(w, r, redirect, errors.New("enter a label"))
		return
	}
	number, valid := sanitizeDialTarget(r.FormValue("number"))
	if !valid {
		s.flashErr(w, r, redirect, errors.New("enter a valid number or extension"))
		return
	}
	d, err := s.store.CreateSpeedDial(r.Context(), store.CreateSpeedDialInput{
		UserID:   u.ID,
		TenantID: *u.TenantID,
		Label:    label,
		Number:   number,
	})
	if err != nil {
		s.flashErr(w, r, redirect, err)
		return
	}
	s.auditNested(r, *u.TenantID, "speed_dial.created", "speed_dial", &d.ID, map[string]any{"number": number})
	http.Redirect(w, r, redirect+"?flash=Speed+dial+added.", http.StatusSeeOther)
}

// meSpeedDialDelete removes one of the SESSION user's own speed dials. Scoped by
// user_id in the store query, so a user can never delete another user's entry.
func (s *Server) meSpeedDialDelete(w http.ResponseWriter, r *http.Request) {
	u := s.currentUser(r)
	if u == nil {
		http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "bad speed dial id", http.StatusBadRequest)
		return
	}
	redirect := "/admin/me"
	if err := s.store.DeleteSpeedDialForUser(r.Context(), u.ID, id); err != nil {
		s.flashErr(w, r, redirect, err)
		return
	}
	tid := uuid.Nil
	if u.TenantID != nil {
		tid = *u.TenantID
	}
	s.auditNested(r, tid, "speed_dial.deleted", "speed_dial", &id, nil)
	http.Redirect(w, r, redirect+"?flash=Speed+dial+removed.", http.StatusSeeOther)
}

// meSpeedDialCall rings the SESSION user's own extension and, on answer, dials
// the speed dial's stored number — reusing the existing click-to-dial originate
// path (no parallel mechanism). The speed dial is resolved strictly within the
// session user's own list, so a user can only call their own favorites.
func (s *Server) meSpeedDialCall(w http.ResponseWriter, r *http.Request) {
	u := s.currentUser(r)
	if u == nil {
		http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "bad speed dial id", http.StatusBadRequest)
		return
	}
	redirect := "/admin/me"
	// Find the dial within the user's OWN list (user-scoped lookup).
	dials, _ := s.store.ListSpeedDialsForUser(r.Context(), u.ID)
	var target *store.SpeedDial
	for i := range dials {
		if dials[i].ID == id {
			target = &dials[i]
			break
		}
	}
	if target == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	// Re-sanitize defensively before originating.
	to, valid := sanitizeDialTarget(target.Number)
	if !valid {
		s.flashErr(w, r, redirect, errors.New("this speed dial has an invalid number"))
		return
	}
	from, err := s.resolveClickToDialFrom(r, u, "")
	if err != nil {
		s.flashErr(w, r, redirect, err)
		return
	}
	if err := s.originateClickToDial(r, from, to); err != nil {
		s.flashErr(w, r, redirect, err)
		return
	}
	s.auditNested(r, from.TenantID, "speed_dial.call", "speed_dial", &target.ID, map[string]any{"to": to})
	flash := "Calling " + target.Label + " — your phone (" + from.Extension + ") will ring."
	http.Redirect(w, r, redirect+"?flash="+url.QueryEscape(flash), http.StatusSeeOther)
}

// meExtension renders the self-service page for one owned extension.
func (s *Server) meExtension(w http.ResponseWriter, r *http.Request) {
	ext, _, ok := s.ownedExtension(w, r)
	if !ok {
		return
	}
	vmBox, _ := s.store.GetVoicemailBoxByExtensionID(r.Context(), ext.ID)
	var vmMsgs []store.VoicemailMessage
	var vmTranscripts map[uuid.UUID]string
	if vmBox != nil {
		vmMsgs, _ = s.store.ListVoicemailMessagesForBox(r.Context(), vmBox.ID)
		vmTranscripts, _ = s.store.ListVoicemailTranscriptsForBox(r.Context(), vmBox.ID)
	}
	recent, _ := s.store.ListCDRsFilteredForTenant(r.Context(), ext.TenantID, store.CDRFilter{
		Search: ext.Extension,
		Limit:  50,
	})
	s.renderLayout(w, r, "My extension", "me_extension", map[string]any{
		"SelfService":   true,
		"Extension":     ext,
		"VoicemailBox":  vmBox,
		"VoicemailMsgs": vmMsgs,
		"VMTranscripts": vmTranscripts,
		"RecentCalls":   recent,
	})
}

// meFeaturesUpdate lets the owner change DND, call forwarding, and voicemail
// on/off (not recording or SIP identity).
func (s *Server) meFeaturesUpdate(w http.ResponseWriter, r *http.Request) {
	ext, _, ok := s.ownedExtension(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	redirect := "/admin/me/extensions/" + ext.ID.String()
	dnd := r.FormValue("dnd") == "true"
	vm := r.FormValue("voicemail_enabled") == "true"
	cfi := strings.TrimSpace(r.FormValue("cf_immediate"))
	cfb := strings.TrimSpace(r.FormValue("cf_busy"))
	cfn := strings.TrimSpace(r.FormValue("cf_no_answer"))
	in := store.UpdateExtensionFeaturesInput{
		DoNotDisturb:     &dnd,
		VoicemailEnabled: &vm,
		CFImmediate:      &cfi,
		CFBusy:           &cfb,
		CFNoAnswer:       &cfn,
		// RecordingEnabled intentionally left nil — owners can't toggle recording.
	}
	if _, err := s.store.UpdateExtensionFeatures(r.Context(), ext.ID, in); err != nil {
		s.flashErr(w, r, redirect, err)
		return
	}
	// VM-to-email opt-in (portal-only; not exposed on /v1). Best-effort: only
	// applies when the extension has a voicemail box.
	if err := s.applyVoicemailEmailNotify(r, ext, redirect, w); err != nil {
		return // applyVoicemailEmailNotify already wrote the flash/redirect
	}
	s.auditNested(r, ext.TenantID, "extension.features.self_update", "extension", &ext.ID, map[string]any{
		"dnd": dnd, "voicemail": vm,
	})
	http.Redirect(w, r, redirect+"?flash=Settings+saved.", http.StatusSeeOther)
}

// meVoicemailAudio streams an owned voicemail message.
func (s *Server) meVoicemailAudio(w http.ResponseWriter, r *http.Request) {
	ext, _, ok := s.ownedExtension(w, r)
	if !ok {
		return
	}
	msg, ok := s.ownedVoicemailMessage(w, r, ext)
	if !ok {
		return
	}
	path, ok := s.safeVoicemailPath(msg.AudioPath)
	if !ok {
		http.Error(w, "recording unavailable", http.StatusNotFound)
		return
	}
	f, err := os.Open(path)
	if err != nil {
		http.Error(w, "recording not readable", http.StatusNotFound)
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		http.Error(w, "recording not readable", http.StatusNotFound)
		return
	}
	_ = s.store.MarkVoicemailMessagePlayed(r.Context(), ext.TenantID, msg.ID)
	w.Header().Set("Content-Type", contentTypeForAudioName(path))
	w.Header().Set("Content-Disposition", "inline; filename=\""+filepath.Base(path)+"\"")
	http.ServeContent(w, r, filepath.Base(path), info.ModTime(), f)
}

// meVoicemailDelete deletes an owned voicemail message.
func (s *Server) meVoicemailDelete(w http.ResponseWriter, r *http.Request) {
	ext, _, ok := s.ownedExtension(w, r)
	if !ok {
		return
	}
	msg, ok := s.ownedVoicemailMessage(w, r, ext)
	if !ok {
		return
	}
	redirect := "/admin/me/extensions/" + ext.ID.String()
	if err := s.store.DeleteVoicemailMessageForTenant(r.Context(), ext.TenantID, msg.ID); err != nil {
		s.flashErr(w, r, redirect, err)
		return
	}
	s.auditNested(r, ext.TenantID, "voicemail.message.self_deleted", "voicemail_message", &msg.ID, nil)
	http.Redirect(w, r, redirect+"?flash=Message+deleted.", http.StatusSeeOther)
}

// applyVoicemailEmailNotify reads the vm-email toggle + address from the posted
// form and persists them to the extension's voicemail box. Shared by the admin
// and owner feature-update handlers. Portal-only (no /v1).
//
// Returns a non-nil error ONLY when it has already written an error response
// (so the caller must just return). A missing voicemail box is treated as a
// silent no-op — the toggle is only meaningful once a box exists. An invalid
// address (non-empty but no '@') is rejected with a flash.
func (s *Server) applyVoicemailEmailNotify(r *http.Request, ext *store.Extension, redirect string, w http.ResponseWriter) error {
	enabled := r.FormValue("vm_email_enabled") == "true"
	addr := strings.TrimSpace(r.FormValue("vm_email_address"))
	if enabled && addr != "" && !strings.Contains(addr, "@") {
		err := errBadEmail
		s.flashErr(w, r, redirect, err)
		return err
	}
	if err := s.store.UpdateVoicemailEmailNotify(r.Context(), ext.ID, enabled, addr); err != nil {
		if err == store.ErrVoicemailBoxNotFound {
			return nil // no box yet; nothing to set
		}
		s.flashErr(w, r, redirect, err)
		return err
	}
	return nil
}

// ownedVoicemailMessage resolves {msgID} and verifies it belongs to the owned
// extension's voicemail box (not merely the same tenant).
func (s *Server) ownedVoicemailMessage(w http.ResponseWriter, r *http.Request, ext *store.Extension) (*store.VoicemailMessage, bool) {
	msgID, err := uuid.Parse(chi.URLParam(r, "msgID"))
	if err != nil {
		http.Error(w, "bad message id", http.StatusBadRequest)
		return nil, false
	}
	box, err := s.store.GetVoicemailBoxByExtensionID(r.Context(), ext.ID)
	if err != nil || box == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return nil, false
	}
	msg, err := s.store.GetVoicemailMessageForTenant(r.Context(), ext.TenantID, msgID)
	if err != nil || msg.BoxID != box.ID {
		http.Error(w, "forbidden", http.StatusForbidden)
		return nil, false
	}
	return msg, true
}
