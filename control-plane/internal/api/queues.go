package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/tendpos/sip-platform/control-plane/internal/store"
)

// contextWithTimeout is a wrapper so unit tests can substitute. The
// request's context is wrong here — the API has returned 201 to the client
// and the background sync outlives the request.
func contextWithTimeout(d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), d)
}

type createQueueReq struct {
	Extension    string `json:"extension"`
	Name         string `json:"name"`
	Strategy     string `json:"strategy"`
	MOHSound     string `json:"moh_sound"`
	MaxWaitTime  int    `json:"max_wait_time"`
}

func (s *Server) createQueue(w http.ResponseWriter, r *http.Request) {
	tid, err := uuid.Parse(chi.URLParam(r, "tenantID"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid tenant id")
		return
	}
	var req createQueueReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.Name == "" {
		writeErr(w, http.StatusBadRequest, "name required")
		return
	}
	q, err := s.store.CreateQueue(r.Context(), store.CreateQueueInput{
		TenantID:    tid,
		Extension:   req.Extension,
		Name:        req.Name,
		Strategy:    req.Strategy,
		MOHSound:    req.MOHSound,
		MaxWaitTime: req.MaxWaitTime,
	})
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}

	// Wave 4.5: push the queue live to mod_callcenter. Best-effort —
	// queue is in our DB regardless; failures are recoverable with a
	// manual `reload mod_callcenter`.
	if s.queueESL != nil {
		go func() {
			ctx, cancel := contextWithTimeout(15 * time.Second)
			defer cancel()
			if err := s.queueESL.SyncQueueToFS(ctx, q.ID); err != nil {
				slog.Warn("ESL queue sync failed (admin can run `reload mod_callcenter`)",
					"queue_id", q.ID, "err", err)
			}
		}()
	}

	writeJSON(w, http.StatusCreated, q)
}

type addQueueAgentReq struct {
	ExtensionID  uuid.UUID `json:"extension_id"`
	TierLevel    int       `json:"tier_level"`
	TierPosition int       `json:"tier_position"`
	WrapUpTime   int       `json:"wrap_up_time"`
}

func (s *Server) addQueueAgent(w http.ResponseWriter, r *http.Request) {
	qID, err := uuid.Parse(chi.URLParam(r, "queueID"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid queue id")
		return
	}
	var req addQueueAgentReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.ExtensionID == uuid.Nil {
		writeErr(w, http.StatusBadRequest, "extension_id required")
		return
	}
	a, err := s.store.AddQueueAgent(r.Context(), store.AddQueueAgentInput{
		QueueID:      qID,
		ExtensionID:  req.ExtensionID,
		TierLevel:    req.TierLevel,
		TierPosition: req.TierPosition,
		WrapUpTime:   req.WrapUpTime,
	})
	if err != nil {
		switch {
		case errors.Is(err, store.ErrCrossTenant):
			writeErr(w, http.StatusBadRequest, err.Error())
		default:
			writeErr(w, http.StatusInternalServerError, err.Error())
		}
		return
	}

	// Wave 4.5: push the (new or updated) agent + tier live to mod_callcenter.
	if s.queueESL != nil {
		go func() {
			ctx, cancel := contextWithTimeout(15 * time.Second)
			defer cancel()
			if err := s.queueESL.SyncAgentForExtension(ctx, req.ExtensionID); err != nil {
				slog.Warn("ESL agent sync failed", "extension_id", req.ExtensionID, "err", err)
			}
		}()
	}

	writeJSON(w, http.StatusCreated, a)
}
