package portal

import (
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/tendpos/sip-platform/control-plane/internal/store"
)

// Recorded paging ("voice blast"): a device records a short clip in the browser
// and the server plays it to every member of a paging group. No WebRTC media is
// needed — FreeSWITCH originates a call to each member that auto-answers and
// plays the file. Works on any device with a microphone + this web app.

const (
	blastSubdir      = "blasts"           // under recordingRoot
	blastMaxBytes    = 12 << 20           // 12 MiB cap on an uploaded clip
	blastRetention   = time.Hour          // delete blast files older than this
	blastContentType = "application/json" // response
)

// broadcastConsole renders the standalone PWA broadcast console (its own HTML
// document, no admin chrome — meant to run full-screen / installed).
func (s *Server) broadcastConsole(w http.ResponseWriter, r *http.Request) {
	user := s.currentUser(r)
	if user == nil {
		http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
		return
	}
	tok := tokenFromCtx(r.Context())

	type groupVM struct {
		ID      string `json:"id"`
		Name    string `json:"name"`
		Code    string `json:"code"`
		Members int    `json:"members"`
		Enabled bool   `json:"enabled"`
	}
	var groups []groupVM
	var tenantName string
	ready := s.broadcastReady()

	if tok != nil && tok.TenantID != nil {
		if t, err := s.store.GetTenant(r.Context(), *tok.TenantID); err == nil {
			tenantName = t.Name
		}
		gs, _ := s.store.ListPagingGroupsForTenant(r.Context(), *tok.TenantID)
		for _, g := range gs {
			groups = append(groups, groupVM{
				ID: g.ID.String(), Name: g.Name, Code: g.Extension,
				Members: g.MemberCount, Enabled: g.Enabled,
			})
		}
	}

	groupsJSON, _ := json.Marshal(groups)

	// Live push-to-talk extras: the user registers a WebRTC client (their own
	// extension's webphone creds) and INVITEs a group's page code. Needs the WS
	// URL + the user's owned extensions to pick an identity.
	type extVM struct {
		ID          string `json:"id"`
		Extension   string `json:"extension"`
		DisplayName string `json:"display_name"`
	}
	var exts []extVM
	owned, _ := s.store.FindOwnedExtensions(r.Context(), user.ID)
	for _, e := range owned {
		exts = append(exts, extVM{ID: e.ID.String(), Extension: e.Extension, DisplayName: e.DisplayName})
	}
	extsJSON, _ := json.Marshal(exts)

	data := map[string]any{
		"UserName":   user.DisplayName,
		"UserEmail":  user.Email,
		"TenantName": tenantName,
		"HasTenant":  tok != nil && tok.TenantID != nil,
		"Ready":      ready,
		"GroupsJSON": template.JS(groupsJSON), //nolint:gosec // json.Marshal escapes <>&
		"WSURL":      deriveWebSocketURL(s.portalBaseURL),
		"ExtsJSON":   template.JS(extsJSON), //nolint:gosec // json.Marshal escapes <>&
		"LiveReady":  s.portalBaseURL != "" && len(exts) > 0,
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.tmpls.ExecuteTemplate(w, "broadcast_page", data); err != nil {
		s.errPage(w, r, err)
	}
}

// broadcastReady reports whether the recorded-page path is wired (FS routing
// target + a recordings dir to write into).
func (s *Server) broadcastReady() bool {
	return s.originator != nil && s.sipRoutingTarget != "" && s.recordingRoot != ""
}

// broadcastSend accepts a recorded clip + target group, writes it to the shared
// recordings volume, and originates an auto-answer playback to each member.
//   POST /broadcast/send   multipart: group_id, audio (audio/wav)
func (s *Server) broadcastSend(w http.ResponseWriter, r *http.Request) {
	user := s.currentUser(r)
	if user == nil {
		writeJSONErr(w, http.StatusUnauthorized, "not signed in")
		return
	}
	if !s.broadcastReady() {
		writeJSONErr(w, http.StatusServiceUnavailable, "broadcast not configured on this server")
		return
	}
	tok := tokenFromCtx(r.Context())
	if tok == nil || tok.TenantID == nil {
		writeJSONErr(w, http.StatusForbidden, "broadcast requires a tenant context")
		return
	}
	tid := *tok.TenantID

	if err := r.ParseMultipartForm(blastMaxBytes + (1 << 20)); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "bad upload")
		return
	}
	gid, err := uuid.Parse(strings.TrimSpace(r.FormValue("group_id")))
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "bad group id")
		return
	}
	// Tenant-scope the group.
	group, err := s.store.GetPagingGroupForTenant(r.Context(), tid, gid)
	if err != nil {
		writeJSONErr(w, http.StatusNotFound, "no such paging group")
		return
	}

	file, _, err := r.FormFile("audio")
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "missing audio")
		return
	}
	defer file.Close()

	members, err := s.store.ListPagingMembersDetailed(r.Context(), gid)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, "member lookup failed")
		return
	}
	if len(members) == 0 {
		writeJSONErr(w, http.StatusConflict, "this group has no members to page")
		return
	}

	// Persist the clip into the shared recordings volume at a path FS reads at
	// the identical mount point.
	dir := filepath.Join(s.recordingRoot, blastSubdir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		writeJSONErr(w, http.StatusInternalServerError, "cannot prepare storage")
		return
	}
	s.cleanupOldBlasts(dir)

	fsPath := filepath.Join(dir, gid.String()+"-"+uuid.NewString()+".wav")
	dst, err := os.Create(fsPath)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, "cannot save clip")
		return
	}
	n, err := io.Copy(dst, io.LimitReader(file, blastMaxBytes))
	cerr := dst.Close()
	if err != nil || cerr != nil || n == 0 {
		_ = os.Remove(fsPath)
		writeJSONErr(w, http.StatusInternalServerError, "clip write failed")
		return
	}

	paged := s.blastToMembers(r, members, fsPath)

	s.auditNested(r, tid, "paging.broadcast", "paging_group", &gid, map[string]any{
		"group": group.Name, "members": len(members), "paged": paged, "bytes": n,
	})
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true, "group": group.Name, "members": len(members), "paged": paged,
	})
}

// blastToMembers originates an auto-answer playback of fsPath to each member.
// Returns the count dispatched. Best-effort per member.
func (s *Server) blastToMembers(r *http.Request, members []store.PagingGroupMember, fsPath string) int {
	app := "&playback(" + fsPath + ")"
	paged := 0
	for _, m := range members {
		if m.SIPUsername == "" || m.SIPDomain == "" {
			continue
		}
		dial := fmt.Sprintf(
			"{sip_auto_answer=true,ignore_early_media=true,origination_caller_id_name=Paging,origination_caller_id_number=PAGE,playback_terminators=none}sofia/internal/sip:%s@%s;fs_path=sip:%s;lr",
			m.SIPUsername, m.SIPDomain, s.sipRoutingTarget,
		)
		if _, err := s.originator.Originate(r.Context(), dial, app); err != nil {
			continue
		}
		paged++
	}
	return paged
}

// cleanupOldBlasts removes blast WAVs older than the retention window. Cheap,
// best-effort; runs on each new broadcast so files don't accumulate.
func (s *Server) cleanupOldBlasts(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-blastRetention)
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".wav") {
			continue
		}
		info, err := e.Info()
		if err == nil && info.ModTime().Before(cutoff) {
			_ = os.Remove(filepath.Join(dir, e.Name()))
		}
	}
}

func writeJSONErr(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", blastContentType)
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", blastContentType)
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
