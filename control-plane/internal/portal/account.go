package portal

import (
	"net/http"
	"strings"
)

func (s *Server) accountPage(w http.ResponseWriter, r *http.Request) {
	u := s.currentUser(r)
	if u == nil {
		http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
		return
	}
	s.renderLayout(w, r, "Account", "account", map[string]any{
		"SessionUser": u,
		"Flash":       r.URL.Query().Get("flash"),
		"FlashErr":    r.URL.Query().Get("err"),
	})
}

func (s *Server) accountProfilePost(w http.ResponseWriter, r *http.Request) {
	u := s.currentUser(r)
	if u == nil {
		http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	name := strings.TrimSpace(r.FormValue("display_name"))
	if name == "" {
		http.Redirect(w, r, "/admin/security/account?err=Display+name+can't+be+empty", http.StatusSeeOther)
		return
	}
	if err := s.store.UpdateUserDisplayName(r.Context(), u.ID, name); err != nil {
		s.flashErr(w, r, "/admin/security/account", err)
		return
	}
	http.Redirect(w, r, "/admin/security/account?flash=Profile+updated.", http.StatusSeeOther)
}

func (s *Server) accountPasswordPost(w http.ResponseWriter, r *http.Request) {
	u := s.currentUser(r)
	if u == nil {
		http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	cur := r.FormValue("current_password")
	npw := r.FormValue("new_password")
	confirm := r.FormValue("confirm_password")
	dest := "/admin/security/account"
	if len(npw) < 8 {
		http.Redirect(w, r, dest+"?err=New+password+must+be+at+least+8+characters", http.StatusSeeOther)
		return
	}
	if npw != confirm {
		http.Redirect(w, r, dest+"?err=New+passwords+don't+match", http.StatusSeeOther)
		return
	}
	// Verify the current password before allowing the change.
	if _, err := s.store.VerifyUserPassword(r.Context(), u.Email, cur); err != nil {
		http.Redirect(w, r, dest+"?err=Current+password+is+incorrect", http.StatusSeeOther)
		return
	}
	if err := s.store.SetUserPassword(r.Context(), u.ID, npw); err != nil {
		s.flashErr(w, r, dest, err)
		return
	}
	http.Redirect(w, r, dest+"?flash=Password+changed.", http.StatusSeeOther)
}
