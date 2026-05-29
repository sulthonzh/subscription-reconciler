package httphandler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"

	"github.com/sulthonzh/subscription-reconciler/internal/domain"
	"github.com/sulthonzh/subscription-reconciler/internal/service"
)

type mockEntRepo struct {
	entitlements map[string]*domain.Entitlement
	upserted     []domain.Entitlement
	updated      []mockUpdateCall

	getByUserAndSourceErr error
	getByUserErr          error
	upsertErr             error
	updateActiveErr       error
}

type mockUpdateCall struct {
	userID string
	source domain.Source
	active bool
	reason string
}

func newEntRepo() *mockEntRepo {
	return &mockEntRepo{entitlements: make(map[string]*domain.Entitlement)}
}

func (m *mockEntRepo) key(userID string, source domain.Source) string {
	return userID + ":" + string(source)
}

func (m *mockEntRepo) GetByUserAndSource(_ context.Context, userID string, source domain.Source) (*domain.Entitlement, error) {
	if m.getByUserAndSourceErr != nil {
		return nil, m.getByUserAndSourceErr
	}
	if e, ok := m.entitlements[m.key(userID, source)]; ok {
		return e, nil
	}
	return nil, nil
}

func (m *mockEntRepo) GetByUser(_ context.Context, userID string) ([]domain.Entitlement, error) {
	if m.getByUserErr != nil {
		return nil, m.getByUserErr
	}
	var result []domain.Entitlement
	for _, e := range m.entitlements {
		if e.UserID == userID {
			result = append(result, *e)
		}
	}
	return result, nil
}

func (m *mockEntRepo) Upsert(_ context.Context, entitlement domain.Entitlement) error {
	if m.upsertErr != nil {
		return m.upsertErr
	}
	m.upserted = append(m.upserted, entitlement)
	k := m.key(entitlement.UserID, entitlement.Source)
	entitlementCopy := entitlement
	m.entitlements[k] = &entitlementCopy
	return nil
}

func (m *mockEntRepo) GetActiveBySource(_ context.Context, source domain.Source) ([]domain.Entitlement, error) {
	var result []domain.Entitlement
	for _, e := range m.entitlements {
		if e.Source == source && e.Active {
			result = append(result, *e)
		}
	}
	return result, nil
}

func (m *mockEntRepo) UpdateActive(_ context.Context, userID string, source domain.Source, active bool, reason string) error {
	if m.updateActiveErr != nil {
		return m.updateActiveErr
	}
	m.updated = append(m.updated, mockUpdateCall{userID, source, active, reason})
	k := m.key(userID, source)
	if e, ok := m.entitlements[k]; ok {
		e.Active = active
		e.Reason = reason
		e.LastChangedAt = time.Now()
	}
	return nil
}

func (m *mockEntRepo) ExpireOverdue(_ context.Context, now time.Time) (int, error) {
	count := 0
	for _, e := range m.entitlements {
		if e.Active && e.ExpiresAt != nil && e.ExpiresAt.Before(now) {
			e.Active = false
			e.Reason = "EXPIRED"
			count++
		}
	}
	return count, nil
}

func (m *mockEntRepo) GetExpiringBefore(_ context.Context, before time.Time) ([]domain.Entitlement, error) {
	var result []domain.Entitlement
	for _, e := range m.entitlements {
		if e.Active && e.ExpiresAt != nil && !e.ExpiresAt.After(before) {
			result = append(result, *e)
		}
	}
	return result, nil
}

type mockEventRepo struct {
	events    map[string]bool
	insertErr error
}

func newEventRepo() *mockEventRepo {
	return &mockEventRepo{events: make(map[string]bool)}
}

func (m *mockEventRepo) Insert(_ context.Context, event domain.StoreEvent) (bool, error) {
	if m.insertErr != nil {
		return false, m.insertErr
	}
	if m.events[event.EventID] {
		return false, nil
	}
	m.events[event.EventID] = true
	return true, nil
}

type mockNotifRepo struct {
	scheduled []domain.Notification
}

func newNotifRepo() *mockNotifRepo {
	return &mockNotifRepo{}
}

func (m *mockNotifRepo) Schedule(_ context.Context, notification domain.Notification) (bool, error) {
	m.scheduled = append(m.scheduled, notification)
	return true, nil
}

func (m *mockNotifRepo) FindDue(_ context.Context, now time.Time, limit int) ([]domain.Notification, error) {
	return nil, nil
}

func (m *mockNotifRepo) MarkSent(_ context.Context, id int64, now time.Time) error {
	return nil
}

type mockAuditRepo struct {
	entries    []domain.AuditEntry
	returnData []domain.AuditEntry
	insertErr  error
}

func (m *mockAuditRepo) Insert(_ context.Context, entry domain.AuditEntry) error {
	if m.insertErr != nil {
		return m.insertErr
	}
	m.entries = append(m.entries, entry)
	return nil
}

func (m *mockAuditRepo) GetByUser(_ context.Context, userID string) ([]domain.AuditEntry, error) {
	if m.returnData != nil {
		return m.returnData, nil
	}
	var result []domain.AuditEntry
	for _, e := range m.entries {
		if e.UserID == userID {
			result = append(result, e)
		}
	}
	return result, nil
}

type mockTxProvider struct{}

func (mockTxProvider) WithinTx(ctx context.Context, fn func(context.Context) error) error {
	return fn(ctx)
}

func setupHandler() (*Handler, *mockEntRepo, *mockEventRepo) {
	entRepo := newEntRepo()
	eventRepo := newEventRepo()
	notifRepo := newNotifRepo()
	auditRepo := &mockAuditRepo{}
	logger := testLogger()

	r := service.NewReconciler(entRepo, eventRepo, notifRepo, auditRepo, mockTxProvider{}, logger)
	h := New(r)
	return h, entRepo, eventRepo
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

func executeRequest(h *Handler, method, path string, body interface{}) *httptest.ResponseRecorder {
	var buf bytes.Buffer
	if body != nil {
		json.NewEncoder(&buf).Encode(body)
	}

	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")

	r := chi.NewRouter()
	r.Post("/webhooks/store", h.HandleStoreWebhook)
	r.Post("/webhooks/marketplace/revoke", h.HandleMarketplaceRevoke)
	r.Get("/users/{id}/entitlement", h.HandleGetEntitlement)
	r.Get("/users/{id}/timeline", h.HandleGetTimeline)

	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	return rr
}

func TestHandleStoreWebhook_Valid(t *testing.T) {
	h, entRepo, _ := setupHandler()

	body := map[string]interface{}{
		"eventId":     "evt_001",
		"userId":      "u_42",
		"type":        "INITIAL_PURCHASE",
		"eventTimeMs": time.Now().Add(-1 * time.Hour).UnixMilli(),
		"productId":   "premium_monthly",
	}

	rr := executeRequest(h, http.MethodPost, "/webhooks/store", body)

	assert.Equal(t, http.StatusOK, rr.Code)

	var resp map[string]string
	json.NewDecoder(rr.Body).Decode(&resp)
	assert.Equal(t, "processed", resp["status"])
	assert.Len(t, entRepo.upserted, 1)
}

func TestHandleStoreWebhook_Duplicate(t *testing.T) {
	h, _, _ := setupHandler()

	body := map[string]interface{}{
		"eventId":     "evt_001",
		"userId":      "u_42",
		"type":        "INITIAL_PURCHASE",
		"eventTimeMs": time.Now().Add(-1 * time.Hour).UnixMilli(),
		"productId":   "premium_monthly",
	}

	rr1 := executeRequest(h, http.MethodPost, "/webhooks/store", body)
	assert.Equal(t, http.StatusOK, rr1.Code)

	rr2 := executeRequest(h, http.MethodPost, "/webhooks/store", body)
	assert.Equal(t, http.StatusOK, rr2.Code)

	var resp map[string]string
	json.NewDecoder(rr2.Body).Decode(&resp)
	assert.Equal(t, "ignored", resp["status"])
}

func TestHandleStoreWebhook_InvalidBody(t *testing.T) {
	h, _, _ := setupHandler()

	rr := executeRequest(h, http.MethodPost, "/webhooks/store", nil)
	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestHandleStoreWebhook_InvalidEventType(t *testing.T) {
	h, _, _ := setupHandler()

	body := map[string]interface{}{
		"eventId":     "evt_001",
		"userId":      "u_42",
		"type":        "INVALID_TYPE",
		"eventTimeMs": time.Now().UnixMilli(),
		"productId":   "premium_monthly",
	}

	rr := executeRequest(h, http.MethodPost, "/webhooks/store", body)
	assert.Equal(t, http.StatusBadRequest, rr.Code)

	var resp map[string]string
	json.NewDecoder(rr.Body).Decode(&resp)
	assert.Equal(t, "invalid event type", resp["error"])
}

func TestHandleStoreWebhook_MissingFields(t *testing.T) {
	h, _, _ := setupHandler()

	body := map[string]interface{}{
		"eventId": "evt_001",
	}

	rr := executeRequest(h, http.MethodPost, "/webhooks/store", body)
	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestHandleMarketplaceRevoke_Valid(t *testing.T) {
	h, entRepo, _ := setupHandler()

	now := time.Now()
	entRepo.entitlements["u_42:MARKETPLACE"] = &domain.Entitlement{
		UserID: "u_42", Source: domain.SourceMarketplace,
		Active: true, Reason: "GRANTED", LastChangedAt: now, CreatedAt: now,
	}

	body := map[string]interface{}{
		"userIds": []string{"u_42", "u_99"},
	}

	rr := executeRequest(h, http.MethodPost, "/webhooks/marketplace/revoke", body)
	assert.Equal(t, http.StatusOK, rr.Code)

	var resp map[string]int
	json.NewDecoder(rr.Body).Decode(&resp)
	assert.Equal(t, 1, resp["revoked"])
	assert.Equal(t, 1, resp["skipped"])
}

func TestHandleMarketplaceRevoke_EmptyUserIDs(t *testing.T) {
	h, _, _ := setupHandler()

	body := map[string]interface{}{
		"userIds": []string{},
	}

	rr := executeRequest(h, http.MethodPost, "/webhooks/marketplace/revoke", body)
	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestHandleGetEntitlement_Found(t *testing.T) {
	h, entRepo, _ := setupHandler()

	now := time.Now()
	expiresAt := now.Add(30 * 24 * time.Hour)
	entRepo.entitlements["u_42:STORE"] = &domain.Entitlement{
		UserID: "u_42", Source: domain.SourceStore,
		Active: true, ExpiresAt: &expiresAt, Reason: "RENEWAL",
		LastChangedAt: now, CreatedAt: now,
	}

	rr := executeRequest(h, http.MethodGet, "/users/u_42/entitlement", nil)
	assert.Equal(t, http.StatusOK, rr.Code)

	var resp entitlementResponse
	json.NewDecoder(rr.Body).Decode(&resp)
	assert.True(t, resp.Active)
	assert.Equal(t, "STORE", resp.Source)
	assert.NotNil(t, resp.ExpiresAt)
	assert.NotNil(t, resp.Reason)
	assert.Equal(t, "RENEWAL", *resp.Reason)
}

func TestHandleGetEntitlement_NotFound(t *testing.T) {
	h, _, _ := setupHandler()

	rr := executeRequest(h, http.MethodGet, "/users/u_unknown/entitlement", nil)
	assert.Equal(t, http.StatusOK, rr.Code)

	var resp entitlementResponse
	json.NewDecoder(rr.Body).Decode(&resp)
	assert.False(t, resp.Active)
	assert.Equal(t, "NONE", resp.Source)
	assert.Nil(t, resp.ExpiresAt)
	assert.Nil(t, resp.LastChangedAt)
	assert.Nil(t, resp.Reason)
}

func TestRoutes_ReturnsRouter(t *testing.T) {
	h, _, _ := setupHandler()
	r := h.Routes()
	assert.NotNil(t, r)

	routes := r.Routes()
	assert.NotEmpty(t, routes, "Routes() should register at least one route")
}

func TestHandleStoreWebhook_UnknownProduct(t *testing.T) {
	entRepo := newEntRepo()
	eventRepo := newEventRepo()
	
	r := service.NewReconciler(entRepo, eventRepo, newNotifRepo(), &mockAuditRepo{}, mockTxProvider{}, testLogger())
	h := New(r)

	body := map[string]interface{}{
		"eventId":     "evt_unknown",
		"userId":      "u_42",
		"type":        "INITIAL_PURCHASE",
		"eventTimeMs": time.Now().Add(-1 * time.Hour).UnixMilli(),
		"productId":   "unknown_product",
	}

	rr := executeRequest(h, http.MethodPost, "/webhooks/store", body)
	assert.Equal(t, http.StatusBadRequest, rr.Code)

	var resp map[string]string
	json.NewDecoder(rr.Body).Decode(&resp)
	assert.Equal(t, "unknown product ID", resp["error"])
}

func TestHandleStoreWebhook_InternalError(t *testing.T) {
	entRepo := newEntRepo()
	eventRepo := newEventRepo()
	eventRepo.insertErr = fmt.Errorf("db down")

	r := service.NewReconciler(entRepo, eventRepo, newNotifRepo(), &mockAuditRepo{}, mockTxProvider{}, testLogger())
	h := New(r)

	body := map[string]interface{}{
		"eventId":     "evt_err",
		"userId":      "u_42",
		"type":        "INITIAL_PURCHASE",
		"eventTimeMs": time.Now().Add(-1 * time.Hour).UnixMilli(),
		"productId":   "premium_monthly",
	}

	rr := executeRequest(h, http.MethodPost, "/webhooks/store", body)
	assert.Equal(t, http.StatusInternalServerError, rr.Code)

	var resp map[string]string
	json.NewDecoder(rr.Body).Decode(&resp)
	assert.Equal(t, "internal error", resp["error"])
}

func TestHandleMarketplaceRevoke_InternalError(t *testing.T) {
	entRepo := newEntRepo()
	entRepo.getByUserAndSourceErr = fmt.Errorf("db down")

	r := service.NewReconciler(entRepo, newEventRepo(), newNotifRepo(), &mockAuditRepo{}, mockTxProvider{}, testLogger())
	h := New(r)

	body := map[string]interface{}{
		"userIds": []string{"u_42"},
	}

	rr := executeRequest(h, http.MethodPost, "/webhooks/marketplace/revoke", body)
	assert.Equal(t, http.StatusInternalServerError, rr.Code)

	var resp map[string]string
	json.NewDecoder(rr.Body).Decode(&resp)
	assert.Equal(t, "internal error", resp["error"])
}

func TestHandleMarketplaceRevoke_InvalidBody(t *testing.T) {
	h, _, _ := setupHandler()

	rr := executeRequest(h, http.MethodPost, "/webhooks/marketplace/revoke", nil)
	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestHandleGetEntitlement_InternalError(t *testing.T) {
	entRepo := newEntRepo()
	entRepo.getByUserErr = fmt.Errorf("db down")

	r := service.NewReconciler(entRepo, newEventRepo(), newNotifRepo(), &mockAuditRepo{}, mockTxProvider{}, testLogger())
	h := New(r)

	rr := executeRequest(h, http.MethodGet, "/users/u_42/entitlement", nil)
	assert.Equal(t, http.StatusInternalServerError, rr.Code)

	var resp map[string]string
	json.NewDecoder(rr.Body).Decode(&resp)
	assert.Equal(t, "internal error", resp["error"])
}

func TestHandleGetEntitlement_WithLastChangedAt(t *testing.T) {
	h, entRepo, _ := setupHandler()

	now := time.Now()
	entRepo.entitlements["u_42:STORE"] = &domain.Entitlement{
		UserID: "u_42", Source: domain.SourceStore,
		Active: true, Reason: "", LastChangedAt: now, CreatedAt: now,
	}

	rr := executeRequest(h, http.MethodGet, "/users/u_42/entitlement", nil)
	assert.Equal(t, http.StatusOK, rr.Code)

	var resp entitlementResponse
	json.NewDecoder(rr.Body).Decode(&resp)
	assert.True(t, resp.Active)
	assert.NotNil(t, resp.LastChangedAt)
	assert.Nil(t, resp.Reason, "empty reason should not be included")
}

func TestHandleGetTimeline_ReturnsEntries(t *testing.T) {
	entRepo := newEntRepo()
	eventRepo := newEventRepo()
	notifRepo := newNotifRepo()
	auditRepo := &mockAuditRepo{}
	now := time.Now()
	auditRepo.returnData = []domain.AuditEntry{
		{ID: 1, UserID: "u_42", TriggerID: "evt_001", Source: domain.SourceStore, PreviousState: "{}", NextState: `{"active":true}`, CreatedAt: now},
		{ID: 2, UserID: "u_42", TriggerID: "evt_002", Source: domain.SourceStore, PreviousState: `{"active":true}`, NextState: `{"active":false}`, CreatedAt: now.Add(1 * time.Hour)},
	}

	r := service.NewReconciler(entRepo, eventRepo, notifRepo, auditRepo, mockTxProvider{}, testLogger())
	h := New(r)

	rr := executeRequest(h, http.MethodGet, "/users/u_42/timeline", nil)
	assert.Equal(t, http.StatusOK, rr.Code)

	var resp []map[string]interface{}
	json.NewDecoder(rr.Body).Decode(&resp)
	assert.Len(t, resp, 2)
	assert.Equal(t, "evt_001", resp[0]["triggerId"])
	assert.Equal(t, "STORE", resp[0]["source"])
}

func TestHandleGetTimeline_Empty(t *testing.T) {
	h, _, _ := setupHandler()

	rr := executeRequest(h, http.MethodGet, "/users/u_unknown/timeline", nil)
	assert.Equal(t, http.StatusOK, rr.Code)

	var resp []interface{}
	json.NewDecoder(rr.Body).Decode(&resp)
	assert.Empty(t, resp)
}
