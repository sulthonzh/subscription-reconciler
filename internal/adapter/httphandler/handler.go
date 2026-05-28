package httphandler

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/sulthonzh/subscription-reconciler/internal/domain"
	"github.com/sulthonzh/subscription-reconciler/internal/service"
)

type Handler struct {
	reconciler *service.Reconciler
}

func New(reconciler *service.Reconciler) *Handler {
	return &Handler{reconciler: reconciler}
}

func (h *Handler) Routes() chi.Router {
	r := chi.NewRouter()

	r.Post("/webhooks/store", h.HandleStoreWebhook)
	r.Post("/webhooks/marketplace/revoke", h.HandleMarketplaceRevoke)
	r.Get("/users/{id}/entitlement", h.HandleGetEntitlement)
	r.Get("/users/{id}/timeline", h.HandleGetTimeline)

	return r
}

type storeWebhookRequest struct {
	EventID     string `json:"eventId"`
	UserID      string `json:"userId"`
	Type        string `json:"type"`
	EventTimeMs int64  `json:"eventTimeMs"`
	ProductID   string `json:"productId"`
}

func (h *Handler) HandleStoreWebhook(w http.ResponseWriter, r *http.Request) {
	var req storeWebhookRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
		return
	}

	if req.EventID == "" || req.UserID == "" || req.Type == "" || req.ProductID == "" || req.EventTimeMs == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "all fields are required"})
		return
	}

	eventType, ok := validEventType(req.Type)
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid event type"})
		return
	}

	event := domain.StoreEvent{
		EventID:     req.EventID,
		UserID:      req.UserID,
		Type:        eventType,
		EventTimeMs: req.EventTimeMs,
		ProductID:   req.ProductID,
	}

	processed, err := h.reconciler.ProcessStoreEvent(r.Context(), event)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}

	if processed {
		writeJSON(w, http.StatusOK, map[string]string{"status": "processed"})
	} else {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ignored"})
	}
}

type marketplaceRevokeRequest struct {
	UserIDs []string `json:"userIds"`
}

func (h *Handler) HandleMarketplaceRevoke(w http.ResponseWriter, r *http.Request) {
	var req marketplaceRevokeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
		return
	}

	if len(req.UserIDs) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "userIds must be non-empty"})
		return
	}

	revoked, skipped, err := h.reconciler.RevokeMarketplace(r.Context(), req.UserIDs)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]int{
		"revoked": revoked,
		"skipped": skipped,
	})
}

type entitlementResponse struct {
	Active        bool    `json:"active"`
	Source        string  `json:"source"`
	ExpiresAt     *string `json:"expiresAt"`
	LastChangedAt *string `json:"lastChangedAt"`
	Reason        *string `json:"reason"`
}

func (h *Handler) HandleGetEntitlement(w http.ResponseWriter, r *http.Request) {
	userID := chi.URLParam(r, "id")
	if userID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "user id required"})
		return
	}

	ent, err := h.reconciler.GetEntitlement(r.Context(), userID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}

	resp := entitlementResponse{
		Active: ent.Active,
		Source: string(ent.Source),
	}

	if ent.ExpiresAt != nil {
		s := ent.ExpiresAt.UTC().Format("2006-01-02T15:04:05Z")
		resp.ExpiresAt = &s
	}
	if !ent.LastChangedAt.IsZero() {
		s := ent.LastChangedAt.UTC().Format("2006-01-02T15:04:05Z")
		resp.LastChangedAt = &s
	}
	if ent.Reason != "" {
		resp.Reason = &ent.Reason
	}

	writeJSON(w, http.StatusOK, resp)
}

type timelineEntry struct {
	TriggerID     string `json:"triggerId"`
	Source        string `json:"source"`
	PreviousState string `json:"previousState"`
	NextState     string `json:"nextState"`
	CreatedAt     string `json:"createdAt"`
}

func (h *Handler) HandleGetTimeline(w http.ResponseWriter, r *http.Request) {
	userID := chi.URLParam(r, "id")
	if userID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "user id required"})
		return
	}

	entries, err := h.reconciler.GetTimeline(r.Context(), userID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}

	if entries == nil {
		entries = []domain.AuditEntry{}
	}

	resp := make([]timelineEntry, len(entries))
	for i, e := range entries {
		resp[i] = timelineEntry{
			TriggerID:     e.TriggerID,
			Source:        string(e.Source),
			PreviousState: e.PreviousState,
			NextState:     e.NextState,
			CreatedAt:     e.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
		}
	}

	writeJSON(w, http.StatusOK, resp)
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func validEventType(s string) (domain.EventType, bool) {
	switch domain.EventType(s) {
	case domain.EventInitialPurchase,
		domain.EventRenewal,
		domain.EventCancellation,
		domain.EventBillingIssue,
		domain.EventExpiration,
		domain.EventUnCancellation:
		return domain.EventType(s), true
	default:
		return "", false
	}
}
