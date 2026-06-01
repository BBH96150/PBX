package portal

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/tendpos/sip-platform/control-plane/internal/audit"
	"github.com/tendpos/sip-platform/control-plane/internal/store"
)

// extensionFromMsgRoute resolves {extensionID}, authorizes the caller against
// the owning tenant, and returns the extension. Shared by the message
// audio/delete handlers.
func (s *Server) extensionFromMsgRoute(w http.ResponseWriter, r *http.Request) (*store.Extension, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "extensionID"))
	if err != nil {
		http.Error(w, "bad extension id", 400)
		return nil, false
	}
	ext, err := s.lookupExtensionByID(r.Context(), id)
	if err != nil {
		s.errPage(w, r, err)
		return nil, false
	}
	if !s.canAccessTenant(r.Context(), ext.TenantID) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return nil, false
	}
	return ext, true
}

// voicemailMessageAudio streams a recording to the browser's <audio> element.
// It enforces tenant ownership and that the stored path resolves under the
// configured storage root before opening any file.
func (s *Server) voicemailMessageAudio(w http.ResponseWriter, r *http.Request) {
	ext, ok := s.extensionFromMsgRoute(w, r)
	if !ok {
		return
	}
	msgID, err := uuid.Parse(chi.URLParam(r, "msgID"))
	if err != nil {
		http.Error(w, "bad message id", 400)
		return
	}
	msg, err := s.store.GetVoicemailMessageForTenant(r.Context(), ext.TenantID, msgID)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	path, ok := s.safeVoicemailPath(msg.AudioPath)
	if !ok {
		http.Error(w, "recording unavailable", http.StatusNotFound)
		return
	}
	f, err := os.Open(path)
	if err != nil {
		// Most likely the storage volume isn't mounted into this container.
		http.Error(w, "recording not readable (storage volume mounted?)", http.StatusNotFound)
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		http.Error(w, "recording not readable", http.StatusNotFound)
		return
	}

	// First play marks it heard; best-effort, don't fail the stream on error.
	_ = s.store.MarkVoicemailMessagePlayed(r.Context(), ext.TenantID, msgID)

	w.Header().Set("Content-Type", contentTypeForAudioName(path))
	w.Header().Set("Content-Disposition", "inline; filename=\""+filepath.Base(path)+"\"")
	// http.ServeContent handles Range requests so the <audio> scrubber works.
	http.ServeContent(w, r, filepath.Base(path), info.ModTime(), f)
}

func (s *Server) voicemailMessageDelete(w http.ResponseWriter, r *http.Request) {
	ext, ok := s.extensionFromMsgRoute(w, r)
	if !ok {
		return
	}
	msgID, err := uuid.Parse(chi.URLParam(r, "msgID"))
	if err != nil {
		http.Error(w, "bad message id", 400)
		return
	}
	redirect := "/admin/extensions/" + ext.ID.String()
	if err := s.store.DeleteVoicemailMessageForTenant(r.Context(), ext.TenantID, msgID); err != nil {
		s.flashErr(w, r, redirect, err)
		return
	}
	ip, ua := audit.FromRequest(r)
	var actorTok *uuid.UUID
	if tok := tokenFromCtx(r.Context()); tok != nil {
		actorTok = &tok.ID
	}
	s.audit.Log(r.Context(), audit.Event{
		TenantID: &ext.TenantID, ActorTokenID: actorTok,
		Event: "voicemail.message.deleted", TargetType: "voicemail_message", TargetID: &msgID,
		IPAddress: ip, UserAgent: ua,
	})
	http.Redirect(w, r, redirect+"?flash=Message+deleted.", http.StatusSeeOther)
}

// safeVoicemailPath cleans the stored path and confirms it sits under the
// configured storage root, defeating any "../" traversal that could reach
// arbitrary files if the DB value were ever tampered with. Returns false when
// streaming is disabled (no root configured) or the path escapes the root.
func (s *Server) safeVoicemailPath(audioPath string) (string, bool) {
	if s.vmStorageRoot == "" || audioPath == "" {
		return "", false
	}
	root := filepath.Clean(s.vmStorageRoot)
	clean := filepath.Clean(audioPath)
	if clean != root && !strings.HasPrefix(clean, root+string(os.PathSeparator)) {
		return "", false
	}
	return clean, true
}

func contentTypeForAudioName(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".wav":
		return "audio/wav"
	case ".mp3":
		return "audio/mpeg"
	case ".ogg":
		return "audio/ogg"
	default:
		return "application/octet-stream"
	}
}
