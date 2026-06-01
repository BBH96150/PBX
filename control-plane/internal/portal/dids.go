package portal

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/tendpos/sip-platform/control-plane/internal/audit"
	"github.com/tendpos/sip-platform/control-plane/internal/e164"
	"github.com/tendpos/sip-platform/control-plane/internal/store"
)

// didDestOption is one selectable inbound-call destination for a DID. The
// dialplan already routes all five kinds (see freeswitch/dialplan.go); the
// portal just needs to let an operator pick any of them.
type didDestOption struct {
	Kind  string // extension | ring_group | ivr | queue | voicemail
	ID    uuid.UUID
	Label string
}

// Key is the form value posted by the destination <select>: "kind:uuid".
func (o didDestOption) Key() string { return o.Kind + ":" + o.ID.String() }

// didDestGroup is one <optgroup> in the destination picker.
type didDestGroup struct {
	Heading string
	Options []didDestOption
}

// didDestinations gathers every routable destination for a tenant, grouped by
// kind for the picker, plus a "kind:uuid" → label map used both to render the
// routed-numbers table and to validate a submitted destination belongs to the
// tenant (and is still enabled).
func (s *Server) didDestinations(ctx context.Context, tid uuid.UUID) ([]didDestGroup, map[string]string) {
	var groups []didDestGroup
	labels := map[string]string{}

	add := func(heading string, opts []didDestOption) {
		if len(opts) == 0 {
			return
		}
		groups = append(groups, didDestGroup{Heading: heading, Options: opts})
		for _, o := range opts {
			labels[o.Key()] = o.Label
		}
	}

	withExt := func(name, ext string) string {
		if ext != "" {
			return name + " (" + ext + ")"
		}
		return name
	}

	var extOpts []didDestOption
	for _, e := range mustExtensions(ctx, s.store, tid) {
		extOpts = append(extOpts, didDestOption{
			Kind: "extension", ID: e.ID, Label: e.Extension + " — " + e.DisplayName,
		})
	}
	add("Extensions", extOpts)

	rgs, _ := s.store.ListRingGroupsForTenant(ctx, tid)
	var rgOpts []didDestOption
	for _, rg := range rgs {
		rgOpts = append(rgOpts, didDestOption{
			Kind: "ring_group", ID: rg.ID, Label: withExt(rg.Name, rg.Extension),
		})
	}
	add("Ring groups", rgOpts)

	ivrs, _ := listIVRs(ctx, s.store, tid)
	var ivrOpts []didDestOption
	for _, v := range ivrs {
		ivrOpts = append(ivrOpts, didDestOption{
			Kind: "ivr", ID: v.ID, Label: withExt(v.Name, v.Extension),
		})
	}
	add("IVR menus", ivrOpts)

	queues, _ := listQueues(ctx, s.store, tid)
	var qOpts []didDestOption
	for _, q := range queues {
		qOpts = append(qOpts, didDestOption{
			Kind: "queue", ID: q.ID, Label: withExt(q.Name, q.Extension),
		})
	}
	add("Call queues", qOpts)

	add("Voicemail boxes", listVoicemailDestinations(ctx, s.store, tid))

	return groups, labels
}

// listVoicemailDestinations returns each enabled voicemail box as a DID
// destination, labelled by its owning extension. A DID with
// destination_kind = 'voicemail' points at a voicemail_boxes row (see
// store.LookupDIDVoicemailTarget).
func listVoicemailDestinations(ctx context.Context, st *store.Store, tid uuid.UUID) []didDestOption {
	const q = `
		SELECT vb.id, e.extension, COALESCE(e.display_name,'')
		  FROM voicemail_boxes vb
		  JOIN extensions e ON e.id = vb.extension_id
		 WHERE vb.tenant_id = $1 AND vb.enabled = true AND e.status = 'active'
		 ORDER BY e.extension`
	rows, err := st.DB.Query(ctx, q, tid)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []didDestOption
	for rows.Next() {
		var id uuid.UUID
		var ext, name string
		if err := rows.Scan(&id, &ext, &name); err != nil {
			continue
		}
		label := "ext " + ext + " voicemail"
		if name != "" {
			label = name + " (" + ext + ") voicemail"
		}
		out = append(out, didDestOption{Kind: "voicemail", ID: id, Label: label})
	}
	return out
}

// didsList shows the per-tenant DID → destination mapping.
func (s *Server) didsList(w http.ResponseWriter, r *http.Request) {
	tid, ok := s.parseTenantParam(w, r)
	if !ok {
		return
	}
	tenant, err := s.store.GetTenant(r.Context(), tid)
	if err != nil {
		s.errPage(w, r, err)
		return
	}
	dids, _ := s.store.ListDIDsForTenant(r.Context(), tid)
	accounts, _ := s.store.ListCarrierAccountsForTenant(r.Context(), tid)
	destGroups, destLabels := s.didDestinations(r.Context(), tid)

	// Resolve each DID's destination to a human label for the table so we show
	// "Sales (601)" instead of a bare UUID. Disabled/removed destinations fall
	// back to the raw id in the template.
	rowDest := map[uuid.UUID]string{}
	for _, d := range dids {
		if lbl, ok := destLabels[d.DestinationKind+":"+d.DestinationID.String()]; ok {
			rowDest[d.ID] = lbl
		}
	}

	s.renderLayout(w, r, tenant.Name+" · Phone numbers", "dids", map[string]any{
		"Tenant":     tenant,
		"DIDs":       dids,
		"Accounts":   accounts,
		"DestGroups": destGroups,
		"RowDest":    rowDest,
		"HasDest":    len(destGroups) > 0,
	})
}

func (s *Server) didCreate(w http.ResponseWriter, r *http.Request) {
	tid, ok := s.parseTenantParam(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", 400)
		return
	}
	redirect := "/admin/tenants/" + tid.String() + "/dids"

	e164Raw := strings.TrimSpace(r.FormValue("e164"))
	normalized, err := e164.Normalize(e164Raw, "US")
	if err != nil {
		s.flashErr(w, r, redirect,
			errors.New("phone number must be E.164 format like +14155551234"))
		return
	}
	// Ensure it has a leading + (the DB check constraint requires it).
	if !strings.HasPrefix(normalized, "+") {
		normalized = "+" + normalized
	}

	// destination is "kind:uuid"; validate it against the tenant's current
	// destinations so an operator can't bind a DID to another tenant's entity
	// or a disabled one.
	destRaw := r.FormValue("destination")
	kind, idStr, found := strings.Cut(destRaw, ":")
	if !found {
		s.flashErr(w, r, redirect, errors.New("pick where inbound calls should go"))
		return
	}
	destID, err := uuid.Parse(idStr)
	if err != nil {
		s.flashErr(w, r, redirect, errors.New("pick where inbound calls should go"))
		return
	}
	_, destLabels := s.didDestinations(r.Context(), tid)
	if _, ok := destLabels[destRaw]; !ok {
		s.flashErr(w, r, redirect,
			errors.New("that destination is no longer available — pick another"))
		return
	}

	acctID, err := uuid.Parse(r.FormValue("carrier_account_id"))
	if err != nil {
		s.flashErr(w, r, redirect, errors.New("pick which trunk this number arrives on"))
		return
	}
	acct, err := s.store.GetCarrierAccountForTenant(r.Context(), tid, acctID)
	if err != nil {
		s.flashErr(w, r, redirect, err)
		return
	}

	did, err := s.store.CreateDID(r.Context(), store.CreateDIDInput{
		TenantID:         tid,
		CarrierID:        acct.CarrierID,
		CarrierAccountID: &acct.ID,
		E164:             normalized,
		DestinationKind:  kind,
		DestinationID:    destID,
		CNAM:             strings.TrimSpace(r.FormValue("cnam")),
	})
	if err != nil {
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
		Event: "did.created", TargetType: "did", TargetID: &did.ID,
		IPAddress: ip, UserAgent: ua,
		Payload: map[string]any{
			"e164": normalized, "destination_kind": kind, "destination_id": destID.String(),
		},
	})
	http.Redirect(w, r,
		redirect+"?flash="+normalized+"+routed.",
		http.StatusSeeOther)
}

func (s *Server) didDelete(w http.ResponseWriter, r *http.Request) {
	tid, ok := s.parseTenantParam(w, r)
	if !ok {
		return
	}
	did, err := uuid.Parse(chi.URLParam(r, "didID"))
	if err != nil {
		http.Error(w, "bad did id", 400)
		return
	}
	if err := s.store.DeleteDIDForTenant(r.Context(), tid, did); err != nil {
		s.flashErr(w, r, "/admin/tenants/"+tid.String()+"/dids", err)
		return
	}
	ip, ua := audit.FromRequest(r)
	var actorTok *uuid.UUID
	if tok := tokenFromCtx(r.Context()); tok != nil {
		actorTok = &tok.ID
	}
	s.audit.Log(r.Context(), audit.Event{
		TenantID: &tid, ActorTokenID: actorTok,
		Event: "did.deleted", TargetType: "did", TargetID: &did,
		IPAddress: ip, UserAgent: ua,
	})
	http.Redirect(w, r, "/admin/tenants/"+tid.String()+"/dids?flash=Number+removed.", http.StatusSeeOther)
}

func (s *Server) didToggleEnabled(w http.ResponseWriter, r *http.Request) {
	tid, ok := s.parseTenantParam(w, r)
	if !ok {
		return
	}
	did, err := uuid.Parse(chi.URLParam(r, "didID"))
	if err != nil {
		http.Error(w, "bad did id", 400)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", 400)
		return
	}
	enabled := r.FormValue("enabled") == "true"
	if err := s.store.SetDIDEnabledForTenant(r.Context(), tid, did, enabled); err != nil {
		s.flashErr(w, r, "/admin/tenants/"+tid.String()+"/dids", err)
		return
	}
	http.Redirect(w, r, "/admin/tenants/"+tid.String()+"/dids?flash=Saved.", http.StatusSeeOther)
}
