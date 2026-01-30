package natsauth

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"opspilot-backend/internal/auth"
	"opspilot-backend/internal/models"
)

type ResolveConflictRequest struct {
	Resolution string `json:"resolution"`
}

// GET /api/v1/agents/conflicts
func (h *Handler) ListAgentConflicts(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.UserIDFromContext(r.Context())
	if !ok {
		respondError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	user, err := h.storage.GetUser(r.Context(), userID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to load user")
		return
	}
	if user == nil || user.OrgID == "" {
		respondError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	conflicts, err := h.storage.GetUnresolvedConflicts(r.Context(), user.OrgID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to load conflicts")
		return
	}

	respondJSON(w, http.StatusOK, conflicts)
}

// POST /api/v1/agents/conflicts/{id}/resolve
func (h *Handler) ResolveConflict(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.UserIDFromContext(r.Context())
	if !ok {
		respondError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	user, err := h.storage.GetUser(r.Context(), userID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to load user")
		return
	}
	if user == nil || user.OrgID == "" {
		respondError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	conflictID := chi.URLParam(r, "id")
	if conflictID == "" {
		respondError(w, http.StatusBadRequest, "missing conflict id")
		return
	}

	conflict, err := h.storage.GetConflict(r.Context(), conflictID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to load conflict")
		return
	}
	if conflict == nil {
		respondError(w, http.StatusNotFound, "conflict not found")
		return
	}

	agent, err := h.storage.GetAgentByAgentID(conflict.AgentID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to load agent")
		return
	}
	if agent == nil || agent.OrgID != user.OrgID {
		respondError(w, http.StatusNotFound, "conflict not found")
		return
	}

	var req ResolveConflictRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request")
		return
	}

	resolution, ok := mapResolution(req.Resolution)
	if !ok {
		respondError(w, http.StatusBadRequest, "invalid resolution")
		return
	}

	if err := h.storage.ResolveConflict(r.Context(), conflictID, resolution, &userID); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to resolve conflict")
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"status": "resolved"})
}

// GET /api/v1/events/stream (SSE)
func (h *Handler) EventStream(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.UserIDFromContext(r.Context())
	if !ok {
		respondError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	user, err := h.storage.GetUser(r.Context(), userID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to load user")
		return
	}
	if user == nil || user.OrgID == "" {
		respondError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		respondError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	sub := conflictHub.Subscribe(user.OrgID)
	defer conflictHub.Unsubscribe(user.OrgID, sub)

	ticker := time.NewTicker(25 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case conflict := <-sub:
			payload, _ := json.Marshal(conflict)
			w.Write([]byte("event: conflict\n"))
			w.Write([]byte("data: "))
			w.Write(payload)
			w.Write([]byte("\n\n"))
			flusher.Flush()
		case <-ticker.C:
			w.Write([]byte("event: ping\n"))
			w.Write([]byte("data: {}\n\n"))
			flusher.Flush()
		}
	}
}

func mapResolution(value string) (string, bool) {
	switch value {
	case "keep_existing":
		return "existing_wins", true
	case "keep_new":
		return "new_wins", true
	case "revoke_both":
		return "both_disconnected", true
	default:
		return "", false
	}
}

// Conflict hub for SSE fanout.
type conflictEventHub struct {
	mu    sync.RWMutex
	subs  map[string]map[chan models.AgentConflict]struct{}
	queue chan conflictEvent
}

type conflictEvent struct {
	orgID    string
	conflict models.AgentConflict
}

func newConflictEventHub() *conflictEventHub {
	hub := &conflictEventHub{
		subs:  make(map[string]map[chan models.AgentConflict]struct{}),
		queue: make(chan conflictEvent, 100),
	}
	go hub.run()
	return hub
}

func (h *conflictEventHub) run() {
	for ev := range h.queue {
		h.mu.RLock()
		subs := h.subs[ev.orgID]
		for ch := range subs {
			select {
			case ch <- ev.conflict:
			default:
			}
		}
		h.mu.RUnlock()
	}
}

func (h *conflictEventHub) Publish(orgID string, conflict models.AgentConflict) {
	h.queue <- conflictEvent{orgID: orgID, conflict: conflict}
}

func (h *conflictEventHub) Subscribe(orgID string) chan models.AgentConflict {
	ch := make(chan models.AgentConflict, 10)
	h.mu.Lock()
	if h.subs[orgID] == nil {
		h.subs[orgID] = make(map[chan models.AgentConflict]struct{})
	}
	h.subs[orgID][ch] = struct{}{}
	h.mu.Unlock()
	return ch
}

func (h *conflictEventHub) Unsubscribe(orgID string, ch chan models.AgentConflict) {
	h.mu.Lock()
	if h.subs[orgID] != nil {
		delete(h.subs[orgID], ch)
		if len(h.subs[orgID]) == 0 {
			delete(h.subs, orgID)
		}
	}
	h.mu.Unlock()
	close(ch)
}

var conflictHub = newConflictEventHub()

// PublishConflictEvent allows other services to push conflict events to SSE.
func PublishConflictEvent(orgID string, conflict models.AgentConflict) {
	conflictHub.Publish(orgID, conflict)
}
