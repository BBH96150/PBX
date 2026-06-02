package portal

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/tendpos/sip-platform/control-plane/internal/audit"
	"github.com/tendpos/sip-platform/control-plane/internal/e164"
	"github.com/tendpos/sip-platform/control-plane/internal/store"
)

// auditNested logs an add/remove on a sub-entity (best-effort actor capture).
func (s *Server) auditNested(r *http.Request, tid uuid.UUID, event, targetType string, targetID *uuid.UUID, payload map[string]any) {
	ip, ua := audit.FromRequest(r)
	var actorTok *uuid.UUID
	if tok := tokenFromCtx(r.Context()); tok != nil {
		actorTok = &tok.ID
	}
	s.audit.Log(r.Context(), audit.Event{
		TenantID: &tid, ActorTokenID: actorTok,
		Event: event, TargetType: targetType, TargetID: targetID,
		IPAddress: ip, UserAgent: ua, Payload: payload,
	})
}

func atoiOr(s string, def int) int {
	if n, err := strconv.Atoi(strings.TrimSpace(s)); err == nil {
		return n
	}
	return def
}

// ================= Ring group members =================

func (s *Server) ringGroupDetail(w http.ResponseWriter, r *http.Request) {
	tid, ok := s.parseTenantParam(w, r)
	if !ok {
		return
	}
	rgID, err := uuid.Parse(chi.URLParam(r, "rgID"))
	if err != nil {
		http.Error(w, "bad ring group id", 400)
		return
	}
	tenant, err := s.store.GetTenant(r.Context(), tid)
	if err != nil {
		s.errPage(w, r, err)
		return
	}
	rg, err := s.store.GetRingGroupForTenant(r.Context(), tid, rgID)
	if err != nil {
		s.errPage(w, r, err)
		return
	}
	members, _ := s.store.ListRingGroupMembersDetailed(r.Context(), rgID)
	exts := mustExtensions(r.Context(), s.store, tid)

	s.renderLayout(w, r, tenant.Name+" · "+rg.Name, "ring_group", map[string]any{
		"Tenant":     tenant,
		"RingGroup":  rg,
		"Members":    members,
		"Extensions": exts,
	})
}

func (s *Server) ringGroupMemberAdd(w http.ResponseWriter, r *http.Request) {
	tid, ok := s.parseTenantParam(w, r)
	if !ok {
		return
	}
	rgID, err := uuid.Parse(chi.URLParam(r, "rgID"))
	if err != nil {
		http.Error(w, "bad ring group id", 400)
		return
	}
	redirect := "/admin/tenants/" + tid.String() + "/ring-groups/" + rgID.String()
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", 400)
		return
	}
	extID, err := uuid.Parse(r.FormValue("extension_id"))
	if err != nil {
		s.flashErr(w, r, redirect, errors.New("pick an extension to add"))
		return
	}
	m, err := s.store.AddRingGroupMember(r.Context(), store.AddRingGroupMemberInput{
		RingGroupID:  rgID,
		ExtensionID:  extID,
		Priority:     atoiOr(r.FormValue("priority"), 100),
		RingDelaySec: atoiOr(r.FormValue("ring_delay_sec"), 0),
	})
	if err != nil {
		s.flashErr(w, r, redirect, friendlyDupErr(err, "that extension is already a member"))
		return
	}
	s.auditNested(r, tid, "ring_group.member.added", "ring_group_member", &m.ID,
		map[string]any{"ring_group_id": rgID.String(), "extension_id": extID.String()})
	http.Redirect(w, r, redirect+"?flash=Member+added.", http.StatusSeeOther)
}

func (s *Server) ringGroupMemberRemove(w http.ResponseWriter, r *http.Request) {
	tid, ok := s.parseTenantParam(w, r)
	if !ok {
		return
	}
	rgID := chi.URLParam(r, "rgID")
	memberID, err := uuid.Parse(chi.URLParam(r, "memberID"))
	if err != nil {
		http.Error(w, "bad member id", 400)
		return
	}
	redirect := "/admin/tenants/" + tid.String() + "/ring-groups/" + rgID
	if err := s.store.DeleteRingGroupMemberForTenant(r.Context(), tid, memberID); err != nil {
		s.flashErr(w, r, redirect, err)
		return
	}
	s.auditNested(r, tid, "ring_group.member.removed", "ring_group_member", &memberID, nil)
	http.Redirect(w, r, redirect+"?flash=Member+removed.", http.StatusSeeOther)
}

func (s *Server) ringGroupDelete(w http.ResponseWriter, r *http.Request) {
	tid, ok := s.parseTenantParam(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "rgID"))
	if err != nil {
		http.Error(w, "bad ring group id", 400)
		return
	}
	tenantHome := "/admin/tenants/" + tid.String()
	if err := s.store.DeleteRingGroupForTenant(r.Context(), tid, id); err != nil {
		s.flashErr(w, r, tenantHome+"/ring-groups/"+id.String(), err)
		return
	}
	s.auditNested(r, tid, "ring_group.deleted", "ring_group", &id, nil)
	http.Redirect(w, r, tenantHome+"?flash=Ring+group+deleted.", http.StatusSeeOther)
}

func (s *Server) ringGroupToggle(w http.ResponseWriter, r *http.Request) {
	tid, ok := s.parseTenantParam(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "rgID"))
	if err != nil {
		http.Error(w, "bad ring group id", 400)
		return
	}
	_ = r.ParseForm()
	redirect := "/admin/tenants/" + tid.String() + "/ring-groups/" + id.String()
	if err := s.store.SetRingGroupEnabledForTenant(r.Context(), tid, id, r.FormValue("enabled") == "true"); err != nil {
		s.flashErr(w, r, redirect, err)
		return
	}
	http.Redirect(w, r, redirect+"?flash=Saved.", http.StatusSeeOther)
}

// ================= IVR options =================

func (s *Server) ivrDetail(w http.ResponseWriter, r *http.Request) {
	tid, ok := s.parseTenantParam(w, r)
	if !ok {
		return
	}
	ivrID, err := uuid.Parse(chi.URLParam(r, "ivrID"))
	if err != nil {
		http.Error(w, "bad ivr id", 400)
		return
	}
	tenant, err := s.store.GetTenant(r.Context(), tid)
	if err != nil {
		s.errPage(w, r, err)
		return
	}
	ivr, err := s.store.GetIVRForTenant(r.Context(), tid, ivrID)
	if err != nil {
		s.errPage(w, r, err)
		return
	}
	options, _ := s.store.ListIVROptions(r.Context(), ivrID)
	destGroups, destLabels := s.didDestinations(r.Context(), tid)

	// Resolve each option's target to a label for display.
	rowTarget := map[uuid.UUID]string{}
	for _, o := range options {
		switch o.ActionKind {
		case "hangup":
			rowTarget[o.ID] = "hang up"
		case "dial_e164":
			rowTarget[o.ID] = "dial " + o.ActionData
		default:
			if o.ActionID != nil {
				if lbl, ok := destLabels[o.ActionKind+":"+o.ActionID.String()]; ok {
					rowTarget[o.ID] = lbl
				}
			}
		}
	}

	s.renderLayout(w, r, tenant.Name+" · "+ivr.Name, "ivr", map[string]any{
		"Tenant":     tenant,
		"IVR":        ivr,
		"Options":    options,
		"DestGroups": destGroups,
		"RowTarget":  rowTarget,
	})
}

func (s *Server) ivrOptionAdd(w http.ResponseWriter, r *http.Request) {
	tid, ok := s.parseTenantParam(w, r)
	if !ok {
		return
	}
	ivrID, err := uuid.Parse(chi.URLParam(r, "ivrID"))
	if err != nil {
		http.Error(w, "bad ivr id", 400)
		return
	}
	redirect := "/admin/tenants/" + tid.String() + "/ivrs/" + ivrID.String()
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", 400)
		return
	}
	digit := strings.TrimSpace(r.FormValue("digit"))
	if digit == "" {
		s.flashErr(w, r, redirect, errors.New("enter the digit(s) the caller presses"))
		return
	}

	in := store.AddIVROptionInput{
		IVRID: ivrID,
		Digit: digit,
		Label: strings.TrimSpace(r.FormValue("label")),
	}

	action := r.FormValue("action")
	switch action {
	case "hangup":
		in.ActionKind = "hangup"
	case "dial_e164":
		raw := strings.TrimSpace(r.FormValue("action_data"))
		norm, nerr := e164.Normalize(raw, "US")
		if nerr != nil {
			s.flashErr(w, r, redirect, errors.New("dial target must be E.164 like +14155551234"))
			return
		}
		if !strings.HasPrefix(norm, "+") {
			norm = "+" + norm
		}
		in.ActionKind = "dial_e164"
		in.ActionData = norm
	default:
		// "kind:uuid" pointing at a tenant entity — validate against the picker.
		kind, idStr, found := strings.Cut(action, ":")
		if !found {
			s.flashErr(w, r, redirect, errors.New("pick what this digit does"))
			return
		}
		id, perr := uuid.Parse(idStr)
		if perr != nil {
			s.flashErr(w, r, redirect, errors.New("pick what this digit does"))
			return
		}
		_, destLabels := s.didDestinations(r.Context(), tid)
		if _, ok := destLabels[action]; !ok {
			s.flashErr(w, r, redirect, errors.New("that target is no longer available"))
			return
		}
		in.ActionKind = kind
		in.ActionID = &id
	}

	o, err := s.store.AddIVROption(r.Context(), in)
	if err != nil {
		s.flashErr(w, r, redirect, friendlyDupErr(err, "that digit is already mapped"))
		return
	}
	s.auditNested(r, tid, "ivr.option.added", "ivr_option", &o.ID,
		map[string]any{"ivr_id": ivrID.String(), "digit": digit, "action_kind": in.ActionKind})
	http.Redirect(w, r, redirect+"?flash=Option+added.", http.StatusSeeOther)
}

func (s *Server) ivrOptionRemove(w http.ResponseWriter, r *http.Request) {
	tid, ok := s.parseTenantParam(w, r)
	if !ok {
		return
	}
	ivrID := chi.URLParam(r, "ivrID")
	optID, err := uuid.Parse(chi.URLParam(r, "optID"))
	if err != nil {
		http.Error(w, "bad option id", 400)
		return
	}
	redirect := "/admin/tenants/" + tid.String() + "/ivrs/" + ivrID
	if err := s.store.DeleteIVROptionForTenant(r.Context(), tid, optID); err != nil {
		s.flashErr(w, r, redirect, err)
		return
	}
	s.auditNested(r, tid, "ivr.option.removed", "ivr_option", &optID, nil)
	http.Redirect(w, r, redirect+"?flash=Option+removed.", http.StatusSeeOther)
}

func (s *Server) ivrDelete(w http.ResponseWriter, r *http.Request) {
	tid, ok := s.parseTenantParam(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "ivrID"))
	if err != nil {
		http.Error(w, "bad ivr id", 400)
		return
	}
	tenantHome := "/admin/tenants/" + tid.String()
	if err := s.store.DeleteIVRForTenant(r.Context(), tid, id); err != nil {
		s.flashErr(w, r, tenantHome+"/ivrs/"+id.String(), err)
		return
	}
	s.auditNested(r, tid, "ivr.deleted", "ivr", &id, nil)
	http.Redirect(w, r, tenantHome+"?flash=IVR+deleted.", http.StatusSeeOther)
}

func (s *Server) ivrToggle(w http.ResponseWriter, r *http.Request) {
	tid, ok := s.parseTenantParam(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "ivrID"))
	if err != nil {
		http.Error(w, "bad ivr id", 400)
		return
	}
	_ = r.ParseForm()
	redirect := "/admin/tenants/" + tid.String() + "/ivrs/" + id.String()
	if err := s.store.SetIVREnabledForTenant(r.Context(), tid, id, r.FormValue("enabled") == "true"); err != nil {
		s.flashErr(w, r, redirect, err)
		return
	}
	http.Redirect(w, r, redirect+"?flash=Saved.", http.StatusSeeOther)
}

// ================= Queue agents =================

func (s *Server) queueDetail(w http.ResponseWriter, r *http.Request) {
	tid, ok := s.parseTenantParam(w, r)
	if !ok {
		return
	}
	queueID, err := uuid.Parse(chi.URLParam(r, "queueID"))
	if err != nil {
		http.Error(w, "bad queue id", 400)
		return
	}
	tenant, err := s.store.GetTenant(r.Context(), tid)
	if err != nil {
		s.errPage(w, r, err)
		return
	}
	q, err := s.store.GetQueueForTenant(r.Context(), tid, queueID)
	if err != nil {
		s.errPage(w, r, err)
		return
	}
	agents, _ := s.store.ListQueueAgentsDetailed(r.Context(), queueID)
	exts := mustExtensions(r.Context(), s.store, tid)

	s.renderLayout(w, r, tenant.Name+" · "+q.Name, "queue", map[string]any{
		"Tenant":     tenant,
		"Queue":      q,
		"Agents":     agents,
		"Extensions": exts,
	})
}

func (s *Server) queueAgentAdd(w http.ResponseWriter, r *http.Request) {
	tid, ok := s.parseTenantParam(w, r)
	if !ok {
		return
	}
	queueID, err := uuid.Parse(chi.URLParam(r, "queueID"))
	if err != nil {
		http.Error(w, "bad queue id", 400)
		return
	}
	redirect := "/admin/tenants/" + tid.String() + "/queues/" + queueID.String()
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", 400)
		return
	}
	extID, err := uuid.Parse(r.FormValue("extension_id"))
	if err != nil {
		s.flashErr(w, r, redirect, errors.New("pick an extension to add as an agent"))
		return
	}
	a, err := s.store.AddQueueAgent(r.Context(), store.AddQueueAgentInput{
		QueueID:      queueID,
		ExtensionID:  extID,
		TierLevel:    atoiOr(r.FormValue("tier_level"), 1),
		TierPosition: atoiOr(r.FormValue("tier_position"), 1),
		WrapUpTime:   atoiOr(r.FormValue("wrap_up_time"), 10),
	})
	if err != nil {
		s.flashErr(w, r, redirect, friendlyDupErr(err, "that extension is already an agent"))
		return
	}
	s.auditNested(r, tid, "queue.agent.added", "queue_agent", &a.ID,
		map[string]any{"queue_id": queueID.String(), "extension_id": extID.String()})
	http.Redirect(w, r, redirect+"?flash=Agent+added.", http.StatusSeeOther)
}

func (s *Server) queueAgentRemove(w http.ResponseWriter, r *http.Request) {
	tid, ok := s.parseTenantParam(w, r)
	if !ok {
		return
	}
	queueID := chi.URLParam(r, "queueID")
	agentID, err := uuid.Parse(chi.URLParam(r, "agentID"))
	if err != nil {
		http.Error(w, "bad agent id", 400)
		return
	}
	redirect := "/admin/tenants/" + tid.String() + "/queues/" + queueID
	if err := s.store.DeleteQueueAgentForTenant(r.Context(), tid, agentID); err != nil {
		s.flashErr(w, r, redirect, err)
		return
	}
	s.auditNested(r, tid, "queue.agent.removed", "queue_agent", &agentID, nil)
	http.Redirect(w, r, redirect+"?flash=Agent+removed.", http.StatusSeeOther)
}

func (s *Server) queueDelete(w http.ResponseWriter, r *http.Request) {
	tid, ok := s.parseTenantParam(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "queueID"))
	if err != nil {
		http.Error(w, "bad queue id", 400)
		return
	}
	tenantHome := "/admin/tenants/" + tid.String()
	if err := s.store.DeleteQueueForTenant(r.Context(), tid, id); err != nil {
		s.flashErr(w, r, tenantHome+"/queues/"+id.String(), err)
		return
	}
	s.auditNested(r, tid, "queue.deleted", "queue", &id, nil)
	http.Redirect(w, r, tenantHome+"?flash=Queue+deleted.", http.StatusSeeOther)
}

func (s *Server) queueToggle(w http.ResponseWriter, r *http.Request) {
	tid, ok := s.parseTenantParam(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "queueID"))
	if err != nil {
		http.Error(w, "bad queue id", 400)
		return
	}
	_ = r.ParseForm()
	redirect := "/admin/tenants/" + tid.String() + "/queues/" + id.String()
	if err := s.store.SetQueueEnabledForTenant(r.Context(), tid, id, r.FormValue("enabled") == "true"); err != nil {
		s.flashErr(w, r, redirect, err)
		return
	}
	http.Redirect(w, r, redirect+"?flash=Saved.", http.StatusSeeOther)
}

// friendlyDupErr maps a unique-violation to a human message; otherwise returns
// the original error.
func friendlyDupErr(err error, dupMsg string) error {
	if err != nil && strings.Contains(err.Error(), "duplicate key") {
		return errors.New(dupMsg)
	}
	return err
}
