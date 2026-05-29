package freeswitch

import (
	"errors"
	"log/slog"
	"net/http"
	"text/template"

	"github.com/jackc/pgx/v5"

	"github.com/tendpos/sip-platform/control-plane/internal/store"
)

// DirectoryHandler responds to FreeSWITCH mod_xml_curl `directory` section
// requests. mod_voicemail uses this to look up per-user vm-password,
// vm-mailto, etc. We do NOT use the directory for SIP digest auth — Kamailio
// handles that — so the SIP `password` param is intentionally omitted.
type DirectoryHandler struct {
	store *store.Store
}

func NewDirectoryHandler(s *store.Store) *DirectoryHandler {
	return &DirectoryHandler{store: s}
}

func (h *DirectoryHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		writeNotFound(w)
		return
	}
	if r.FormValue("section") != "directory" {
		writeNotFound(w)
		return
	}

	domain := r.FormValue("domain")
	user := r.FormValue("user")
	purpose := r.FormValue("purpose")

	slog.Info("directory lookup", "domain", domain, "user", user, "purpose", purpose)

	if domain == "" || user == "" {
		writeNotFound(w)
		return
	}

	du, err := h.store.LookupDirectoryUser(r.Context(), domain, user)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeNotFound(w)
			return
		}
		slog.Error("directory lookup failed", "err", err)
		writeNotFound(w)
		return
	}

	w.Header().Set("Content-Type", "text/xml; charset=utf-8")
	data := directoryData{
		Domain:      du.Domain,
		User:        du.Extension.SIPUsername,
		DisplayName: du.Extension.DisplayName,
		Extension:   du.Extension.Extension,
	}
	if du.VoicemailBox != nil {
		data.HasVoicemail = true
		data.VMPassword = du.VoicemailBox.PIN
		data.VMMailto = du.VoicemailBox.Email
	}
	if err := directoryTmpl.Execute(w, data); err != nil {
		slog.Error("directory template render", "err", err)
	}
}

type directoryData struct {
	Domain       string
	User         string
	DisplayName  string
	Extension    string
	HasVoicemail bool
	VMPassword   string
	VMMailto     string
}

var directoryTmpl = template.Must(template.New("dir").Parse(`<?xml version="1.0"?>
<document type="freeswitch/xml">
  <section name="directory">
    <domain name="{{.Domain}}">
      <params>
        <param name="dial-string" value="{sip_invite_domain=${dialed_domain},presence_id=${dialed_user}@${dialed_domain}}${sofia_contact(${dialed_user}@${dialed_domain})}"/>
      </params>
      <user id="{{.User}}">
        <params>
          {{if .HasVoicemail}}<param name="vm-password" value="{{.VMPassword}}"/>
          <param name="vm-enabled" value="true"/>
          {{else}}<param name="vm-enabled" value="false"/>
          {{end}}
        </params>
        <variables>
          <variable name="user_context" value="default"/>
          <variable name="effective_caller_id_number" value="{{.Extension}}"/>
          {{if .DisplayName}}<variable name="effective_caller_id_name" value="{{.DisplayName}}"/>{{end}}
        </variables>
      </user>
    </domain>
  </section>
</document>
`))
