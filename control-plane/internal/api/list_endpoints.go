package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// listQueues returns a tenant's call queues.
func (s *Server) listQueues(w http.ResponseWriter, r *http.Request) {
	tid, err := uuid.Parse(chi.URLParam(r, "tenantID"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid tenant id")
		return
	}
	qs, err := s.store.ListQueuesForTenant(r.Context(), tid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, qs)
}

// listDevices returns a tenant's devices. The per-device provisioning token is
// stripped — a bulk list of ZTP tokens is more exposure than the single
// GET /v1/devices/{mac} lookup warrants.
func (s *Server) listDevices(w http.ResponseWriter, r *http.Request) {
	tid, err := uuid.Parse(chi.URLParam(r, "tenantID"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid tenant id")
		return
	}
	ds, err := s.store.ListDevicesForTenant(r.Context(), tid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	for i := range ds {
		ds[i].ProvisioningToken = ""
	}
	writeJSON(w, http.StatusOK, ds)
}

// listPagingGroups returns a tenant's paging / PTT groups (each with its
// member count). Member rosters are not expanded in the list.
func (s *Server) listPagingGroups(w http.ResponseWriter, r *http.Request) {
	tid, err := uuid.Parse(chi.URLParam(r, "tenantID"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid tenant id")
		return
	}
	gs, err := s.store.ListPagingGroupsForTenant(r.Context(), tid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, gs)
}

// listPagingGroupMembers returns the member extensions of one paging group —
// the roster a native PTT client renders for a channel. Tenant-scoped: the
// group must belong to the path tenant.
func (s *Server) listPagingGroupMembers(w http.ResponseWriter, r *http.Request) {
	tid, err := uuid.Parse(chi.URLParam(r, "tenantID"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid tenant id")
		return
	}
	gid, err := uuid.Parse(chi.URLParam(r, "groupID"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid group id")
		return
	}
	if _, err := s.store.GetPagingGroupForTenant(r.Context(), tid, gid); err != nil {
		writeErr(w, http.StatusNotFound, "no such paging group for tenant")
		return
	}
	members, err := s.store.ListPagingMembersDetailed(r.Context(), gid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, members)
}

// listConferenceRooms returns a tenant's meet-me conference bridges.
func (s *Server) listConferenceRooms(w http.ResponseWriter, r *http.Request) {
	tid, err := uuid.Parse(chi.URLParam(r, "tenantID"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid tenant id")
		return
	}
	rooms, err := s.store.ListConferenceRoomsForTenant(r.Context(), tid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, rooms)
}

// listExtensionPresence returns the live registration status (online/offline)
// of every active extension in a tenant, derived from Kamailio's usrloc
// location table.
func (s *Server) listExtensionPresence(w http.ResponseWriter, r *http.Request) {
	tid, err := uuid.Parse(chi.URLParam(r, "tenantID"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid tenant id")
		return
	}
	pres, err := s.store.ListExtensionPresenceForTenant(r.Context(), tid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, pres)
}

// listParkLots returns a tenant's call-park lots.
func (s *Server) listParkLots(w http.ResponseWriter, r *http.Request) {
	tid, err := uuid.Parse(chi.URLParam(r, "tenantID"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid tenant id")
		return
	}
	lots, err := s.store.ListParkLotsForTenant(r.Context(), tid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, lots)
}

// listE911Locations returns a tenant's E911 dispatchable locations.
func (s *Server) listE911Locations(w http.ResponseWriter, r *http.Request) {
	tid, err := uuid.Parse(chi.URLParam(r, "tenantID"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid tenant id")
		return
	}
	locs, err := s.store.ListE911LocationsForTenant(r.Context(), tid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, locs)
}

// listQueueCallbacks returns a tenant's queue callbacks ("keep your place in
// line" requests). An optional ?status= query filters to one status.
func (s *Server) listQueueCallbacks(w http.ResponseWriter, r *http.Request) {
	tid, err := uuid.Parse(chi.URLParam(r, "tenantID"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid tenant id")
		return
	}
	cbs, err := s.store.ListQueueCallbacksForTenant(r.Context(), tid, r.URL.Query().Get("status"))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, cbs)
}

// listVoicemailMessages returns an extension's voicemail messages (metadata
// only — the audio path is never serialized).
func (s *Server) listVoicemailMessages(w http.ResponseWriter, r *http.Request) {
	extID, err := uuid.Parse(chi.URLParam(r, "extensionID"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid extension id")
		return
	}
	box, err := s.store.GetVoicemailBoxByExtensionID(r.Context(), extID)
	if err != nil {
		writeErr(w, http.StatusNotFound, "no voicemail box for extension")
		return
	}
	msgs, err := s.store.ListVoicemailMessagesForBox(r.Context(), box.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, msgs)
}
