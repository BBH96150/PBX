package portal

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/google/uuid"

	"github.com/tendpos/sip-platform/control-plane/internal/store"
)

// Click-to-dial ("Call" button): a CRM-style action that rings the caller's OWN
// desk phone / softphone first, and when they answer, dials the target number
// and connects them. There is NO new data model, migration, or /v1 API route
// here — it is implemented entirely by originating a call to the caller's own
// extension (mirroring broadcast.go / supervisor.go) and running a `&transfer`
// application on the answered leg so the caller re-enters the dialplan dialing
// the target. Existing routing then handles internal-vs-outbound for us.

// errClickToDialNoExt is shown when the logged-in user has no owned extension to
// originate the caller leg to.
var errClickToDialNoExt = errors.New("assign yourself an extension to use click-to-dial")

// buildClickToDialActions returns the FreeSWITCH application string to run on the
// caller's answered leg so they re-enter the XML dialplan dialing toNumber. The
// caller is assumed already sanitized via sanitizeDialTarget; this builder is
// pure (no I/O) so it can be unit-tested.
func buildClickToDialActions(toNumber string) string {
	return "&transfer(" + toNumber + " XML default)"
}

// sanitizeDialTarget keeps only dialable characters — digits, a single leading
// '+', and '*'/'#'. Anything else (letters, spaces, SIP/script metacharacters)
// makes the whole target invalid so it can never be injected into the dial
// string or the transfer application. Returns ("", false) on reject.
func sanitizeDialTarget(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", false
	}
	var b strings.Builder
	for i, r := range raw {
		switch {
		case r >= '0' && r <= '9', r == '*', r == '#':
			b.WriteRune(r)
		case r == '+' && i == 0:
			b.WriteRune(r)
		default:
			return "", false
		}
	}
	out := b.String()
	if out == "" || out == "+" {
		return "", false
	}
	return out, true
}

// clickToDial handles POST /admin/tenants/{tenantID}/click-to-dial.
// Form: to (number to dial) + optional from_extension_id (which owned extension
// to ring as the caller leg). Self-service-ish: the from-extension MUST be one
// the logged-in user owns.
func (s *Server) clickToDial(w http.ResponseWriter, r *http.Request) {
	tid, ok := s.parseTenantParam(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}

	// Redirect back to wherever the Call button was clicked, falling back to the
	// tenant's contacts page.
	back := r.Referer()
	if back == "" {
		back = "/admin/tenants/" + tid.String() + "/contacts"
	}

	user := s.currentUser(r)
	if user == nil {
		http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
		return
	}

	if s.originator == nil || s.sipRoutingTarget == "" {
		s.flashErr(w, r, back, errors.New("click-to-dial not configured"))
		return
	}

	to, valid := sanitizeDialTarget(r.FormValue("to"))
	if !valid {
		s.flashErr(w, r, back, errors.New("enter a valid number to call"))
		return
	}

	// Resolve the from-extension: the requested one if the user OWNS it, else the
	// user's first owned extension. None owned → error.
	from, err := s.resolveClickToDialFrom(r, user, strings.TrimSpace(r.FormValue("from_extension_id")))
	if err != nil {
		s.flashErr(w, r, back, err)
		return
	}

	if err := s.originateClickToDial(r, from, to); err != nil {
		s.flashErr(w, r, back, err)
		return
	}

	fromID := from.ID
	s.auditNested(r, tid, "call.click_to_dial", "extension", &fromID, map[string]any{"to": to})

	flash := fmt.Sprintf("Calling %s — your phone (%s) will ring.", to, from.Extension)
	sep := "?"
	if strings.Contains(back, "?") {
		sep = "&"
	}
	http.Redirect(w, r, back+sep+"flash="+url.QueryEscape(flash), http.StatusSeeOther)
}

// resolveClickToDialFrom picks the owned extension to ring as the caller leg:
// the requested wantID if the user owns it, else the user's first owned
// extension. Returns errClickToDialNoExt when the user owns no extension.
// Shared by the admin click-to-dial handler and the self-service speed-dial
// CALL button so both ring the caller's OWN phone the same way.
func (s *Server) resolveClickToDialFrom(r *http.Request, user *store.User, wantID string) (store.Extension, error) {
	owned, _ := s.store.FindOwnedExtensions(r.Context(), user.ID)
	if len(owned) == 0 {
		return store.Extension{}, errClickToDialNoExt
	}
	from := owned[0]
	if wantID != "" {
		if id, err := uuid.Parse(wantID); err == nil {
			for _, e := range owned {
				if e.ID == id {
					from = e
					break
				}
			}
		}
	}
	return from, nil
}

// originateClickToDial rings the caller's own extension via Kamailio (mirrors
// broadcast.go); on answer, &transfer re-enters the dialplan dialing the
// already-sanitized target. Pure originate path shared by click-to-dial and the
// speed-dial CALL button — there is NO parallel originate mechanism.
func (s *Server) originateClickToDial(r *http.Request, from store.Extension, to string) error {
	if s.originator == nil || s.sipRoutingTarget == "" {
		return errors.New("click-to-dial not configured")
	}
	// Resolve the from-extension's SIP domain string by matching its
	// sip_domain_id against the tenant's SIP domains (mirrors supervisor.go).
	fromDomain := ""
	for _, d := range mustSIPDomains(r.Context(), s.store, from.TenantID) {
		if d.ID == from.SIPDomainID {
			fromDomain = d.Domain
			break
		}
	}
	if fromDomain == "" {
		return errClickToDialNoExt
	}
	dial := fmt.Sprintf(
		"{origination_caller_id_number=%s,ignore_early_media=true}sofia/internal/sip:%s@%s;fs_path=sip:%s;lr",
		from.Extension, from.SIPUsername, fromDomain, s.sipRoutingTarget,
	)
	if _, err := s.originator.Originate(r.Context(), dial, buildClickToDialActions(to)); err != nil {
		return fmt.Errorf("could not place call: %w", err)
	}
	return nil
}
