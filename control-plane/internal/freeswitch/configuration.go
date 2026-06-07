package freeswitch

import (
	"log/slog"
	"net/http"
	"text/template"

	"github.com/tendpos/sip-platform/control-plane/internal/store"
)

// ConfigurationHandler responds to FreeSWITCH mod_xml_curl `configuration`
// section lookups. Each FS module that wants dynamic config queries here.
//
// Currently served:
//   ivr.conf        — mod_ivr menus (Wave 3)
//   callcenter.conf — mod_callcenter queues/agents/tiers (Wave 4)
type ConfigurationHandler struct {
	store          *store.Store
	kamailioTarget string // host:port — needed for agent contact URIs
}

func NewConfigurationHandler(s *store.Store, kamailioTarget string) *ConfigurationHandler {
	return &ConfigurationHandler{store: s, kamailioTarget: kamailioTarget}
}

func (h *ConfigurationHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		writeNotFound(w)
		return
	}
	if r.FormValue("section") != "configuration" {
		writeNotFound(w)
		return
	}

	name := firstNonEmpty(r.FormValue("key_value"), r.FormValue("name"))
	slog.Info("configuration lookup", "name", name)

	switch name {
	case "ivr.conf":
		h.handleIVRConf(w, r)
	case "callcenter.conf":
		h.handleCallcenterConf(w, r)
	default:
		// Many modules query for configs that don't exist yet — return
		// not-found so FS falls through to its bundled defaults.
		writeNotFound(w)
	}
}

func (h *ConfigurationHandler) handleIVRConf(w http.ResponseWriter, r *http.Request) {
	menus, err := h.store.ListEnabledIVRMenus(r.Context())
	if err != nil {
		slog.Error("list ivr menus", "err", err)
		writeNotFound(w)
		return
	}
	w.Header().Set("Content-Type", "text/xml; charset=utf-8")
	if err := ivrConfTmpl.Execute(w, menus); err != nil {
		slog.Error("ivr.conf render", "err", err)
	}
}

func (h *ConfigurationHandler) handleCallcenterConf(w http.ResponseWriter, r *http.Request) {
	cfg, err := h.store.ListCallcenterConfig(r.Context(), h.kamailioTarget)
	if err != nil {
		slog.Error("list callcenter config", "err", err)
		writeNotFound(w)
		return
	}
	w.Header().Set("Content-Type", "text/xml; charset=utf-8")
	if err := callcenterConfTmpl.Execute(w, cfg); err != nil {
		slog.Error("callcenter.conf render", "err", err)
	}
}

// NOTE: conference.conf is intentionally NOT served here. The paging conference
// profile is static and identical for every tenant, and mod_conference fetches
// it at FS boot — before the control-plane is reachable. Serving it via xml_curl
// made the boot-time fetch fail and the module abort loading. It now lives in a
// static file: freeswitch/conf/autoload_configs/conference.conf.xml.

var ivrConfTmpl = template.Must(template.New("ivrconf").Parse(`<?xml version="1.0"?>
<document type="freeswitch/xml">
  <section name="configuration">
    <configuration name="ivr.conf" description="IVR menus">
      <menus>
        {{range .}}
        <menu name="{{.IVR.ID}}"
              greet-long="{{.IVR.GreetingLong}}"
              greet-short="{{.IVR.GreetingShort}}"
              invalid-sound="{{.IVR.InvalidSound}}"
              exit-sound="{{.IVR.ExitSound}}"
              timeout="{{.IVR.TimeoutMS}}"
              inter-digit-timeout="{{.IVR.InterDigitTimeoutMS}}"
              max-failures="{{.IVR.MaxFailures}}"
              max-timeouts="{{.IVR.MaxTimeouts}}"
              digit-len="{{.IVR.DigitLen}}">
          {{range .Entries}}<entry action="{{.Action}}" digits="{{.Digit}}"{{if .Param}} param="{{.Param}}"{{end}}/>
          {{end}}
        </menu>
        {{end}}
      </menus>
    </configuration>
  </section>
</document>
`))

var callcenterConfTmpl = template.Must(template.New("ccconf").Parse(`<?xml version="1.0"?>
<document type="freeswitch/xml">
  <section name="configuration">
    <configuration name="callcenter.conf" description="Call Center">
      <settings>
        <param name="odbc-dsn" value=""/>
        <param name="dbname" value="callcenter"/>
      </settings>
      <queues>
        {{range .Queues}}
        <queue name="{{.Name}}">
          <param name="strategy" value="{{.Strategy}}"/>
          <param name="moh-sound" value="{{.MOHSound}}"/>
          {{if .RecordTemplate}}<param name="record-template" value="{{.RecordTemplate}}"/>{{end}}
          <param name="time-base-score" value="{{.TimeBaseScore}}"/>
          <param name="max-wait-time" value="{{.MaxWaitTime}}"/>
          <param name="max-wait-time-with-no-agent" value="{{.MaxWaitNoAgent}}"/>
          <param name="max-wait-time-with-no-agent-time-reached" value="{{.MaxWaitNoAgentTimeReached}}"/>
          <param name="tier-rules-apply" value="{{.TierRulesApply}}"/>
          <param name="tier-rule-wait-second" value="{{.TierRuleWaitSecond}}"/>
          <param name="tier-rule-no-agent-no-wait" value="{{.TierRuleNoAgentNoWait}}"/>
          <param name="discard-abandoned-after" value="{{.DiscardAbandonedAfter}}"/>
          <param name="abandoned-resume-allowed" value="{{.AbandonedResumeAllowed}}"/>
          {{if .AnnounceSound}}<param name="announce-sound" value="{{.AnnounceSound}}"/>{{end}}
        </queue>
        {{end}}
      </queues>
      <agents>
        {{range .Agents}}
        <agent name="{{.Name}}"
               type="{{.Type}}"
               contact="{{.Contact}}"
               status="Available"
               max-no-answer="{{.MaxNoAnswer}}"
               wrap-up-time="{{.WrapUpTime}}"
               reject-delay-time="{{.RejectDelayTime}}"
               busy-delay-time="{{.BusyDelayTime}}"
               no-answer-delay-time="{{.NoAnswerDelayTime}}"/>
        {{end}}
      </agents>
      <tiers>
        {{range .Tiers}}
        <tier agent="{{.Agent}}" queue="{{.Queue}}" level="{{.Level}}" position="{{.Position}}"/>
        {{end}}
      </tiers>
    </configuration>
  </section>
</document>
`))
