package portal

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/tendpos/sip-platform/control-plane/internal/audit"
)

// safePathUnder cleans p and confirms it sits under root (path-traversal guard).
func safePathUnder(root, p string) (string, bool) {
	if root == "" || p == "" {
		return "", false
	}
	root = filepath.Clean(root)
	clean := filepath.Clean(p)
	if clean != root && !strings.HasPrefix(clean, root+string(os.PathSeparator)) {
		return "", false
	}
	return clean, true
}

// cdrRecordingAudio streams a call recording for a tenant's CDR.
func (s *Server) cdrRecordingAudio(w http.ResponseWriter, r *http.Request) {
	tid, ok := s.parseTenantParam(w, r)
	if !ok {
		return
	}
	cdrID, err := uuid.Parse(chi.URLParam(r, "cdrID"))
	if err != nil {
		http.Error(w, "bad cdr id", 400)
		return
	}
	cdr, err := s.store.GetCDRForTenant(r.Context(), tid, cdrID)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	path, ok := safePathUnder(s.recordingRoot, cdr.RecordingPath)
	if !ok {
		http.Error(w, "recording unavailable", http.StatusNotFound)
		return
	}
	f, err := os.Open(path)
	if err != nil {
		http.Error(w, "recording not readable (recordings volume mounted?)", http.StatusNotFound)
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		http.Error(w, "recording not readable", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", contentTypeForAudioName(path))
	w.Header().Set("Content-Disposition", "inline; filename=\""+filepath.Base(path)+"\"")
	http.ServeContent(w, r, filepath.Base(path), info.ModTime(), f)
}

// cdrRecordingDelete removes the recording file and clears recording_path.
func (s *Server) cdrRecordingDelete(w http.ResponseWriter, r *http.Request) {
	tid, ok := s.parseTenantParam(w, r)
	if !ok {
		return
	}
	cdrID, err := uuid.Parse(chi.URLParam(r, "cdrID"))
	if err != nil {
		http.Error(w, "bad cdr id", 400)
		return
	}
	redirect := "/admin/tenants/" + tid.String() + "/cdrs"
	cdr, err := s.store.GetCDRForTenant(r.Context(), tid, cdrID)
	if err != nil {
		s.flashErr(w, r, redirect, err)
		return
	}
	// Best-effort file delete (under the root only), then clear the DB pointer.
	if path, ok := safePathUnder(s.recordingRoot, cdr.RecordingPath); ok {
		_ = os.Remove(path)
	}
	if err := s.store.ClearCDRRecording(r.Context(), tid, cdrID); err != nil {
		s.flashErr(w, r, redirect, err)
		return
	}
	ip, ua := audit.FromRequest(r)
	var actorTok *uuid.UUID
	if tok := tokenFromCtx(r.Context()); tok != nil {
		actorTok = &tok.ID
	}
	s.audit.Log(r.Context(), audit.Event{
		TenantID: &tid, ActorTokenID: actorTok,
		Event: "cdr.recording.deleted", TargetType: "cdr", TargetID: &cdrID,
		IPAddress: ip, UserAgent: ua,
	})
	http.Redirect(w, r, redirect+"?flash=Recording+deleted.", http.StatusSeeOther)
}
