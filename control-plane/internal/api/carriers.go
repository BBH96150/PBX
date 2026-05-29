package api

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/tendpos/sip-platform/control-plane/internal/store"
)

func (s *Server) listCarriers(w http.ResponseWriter, r *http.Request) {
	cs, err := s.store.ListCarriers(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, cs)
}

type createCarrierAccountReq struct {
	Name          string `json:"name"`
	SIPUsername   string `json:"sip_username"`
	SIPPassword   string `json:"sip_password"`
	AuthRealm     string `json:"auth_realm"`
	FSGatewayName string `json:"fs_gateway_name"`
	Register      *bool  `json:"register"`
	MainDIDE164   string `json:"main_did_e164"`
}

func (s *Server) createCarrierAccount(w http.ResponseWriter, r *http.Request) {
	kind := chi.URLParam(r, "kind")
	carrier, err := s.store.GetCarrierByKind(r.Context(), kind)
	if err != nil {
		writeErr(w, http.StatusNotFound, "carrier kind not found")
		return
	}
	var req createCarrierAccountReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.Name == "" || req.SIPUsername == "" || req.SIPPassword == "" || req.FSGatewayName == "" {
		writeErr(w, http.StatusBadRequest, "name, sip_username, sip_password, fs_gateway_name required")
		return
	}
	register := true
	if req.Register != nil {
		register = *req.Register
	}
	acct, err := s.store.CreateCarrierAccount(r.Context(), store.CreateCarrierAccountInput{
		CarrierID:     carrier.ID,
		Name:          req.Name,
		SIPUsername:   req.SIPUsername,
		SIPPassword:   req.SIPPassword,
		AuthRealm:     req.AuthRealm,
		FSGatewayName: req.FSGatewayName,
		Register:      register,
		MainDIDE164:   req.MainDIDE164,
	})
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, acct)
}
