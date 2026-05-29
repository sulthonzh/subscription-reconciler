package integration

import (
	"database/sql"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"github.com/sulthonzh/subscription-reconciler/internal/adapter/httphandler"
	"github.com/sulthonzh/subscription-reconciler/internal/adapter/sqlite"
	"github.com/sulthonzh/subscription-reconciler/internal/service"
)

const schema = `
CREATE TABLE IF NOT EXISTS entitlements (
    user_id       TEXT NOT NULL,
    source        TEXT NOT NULL,
    active        BOOLEAN NOT NULL DEFAULT FALSE,
    expires_at    TEXT,
    reason        TEXT,
    last_changed_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    last_event_time_ms INTEGER NOT NULL DEFAULT 0,
    created_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    PRIMARY KEY (user_id, source)
);

CREATE TABLE IF NOT EXISTS store_events (
    event_id      TEXT PRIMARY KEY,
    user_id       TEXT    NOT NULL,
    type          TEXT    NOT NULL,
    event_time_ms INTEGER NOT NULL,
    product_id    TEXT    NOT NULL,
    processed_at  TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

CREATE TABLE IF NOT EXISTS carrier_poll_log (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id       TEXT    NOT NULL,
    polled_at     TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    status        TEXT    NOT NULL,
    locked_until  TEXT
);

CREATE TABLE IF NOT EXISTS notifications (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id       TEXT NOT NULL,
    type          TEXT NOT NULL DEFAULT 'PREMIUM_EXPIRES_SOON',
    scheduled_for TEXT NOT NULL,
    sent_at       TEXT,
    created_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    UNIQUE(user_id, type, scheduled_for)
);

CREATE TABLE IF NOT EXISTS audit_log (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id       TEXT NOT NULL,
    trigger_id    TEXT,
    source        TEXT NOT NULL,
    previous_state TEXT NOT NULL,
    next_state    TEXT NOT NULL,
    created_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
`

func setupIntegration(t *testing.T) (*httphandler.Handler, *sql.DB) {
	t.Helper()

	db, err := sql.Open("sqlite", "file::memory:?cache=shared")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	_, err = db.Exec(schema)
	require.NoError(t, err)

	entRepo := sqlite.NewEntitlementRepo(db)
	eventRepo := sqlite.NewStoreEventRepo(db)
	notifRepo := sqlite.NewNotificationRepo(db)
	auditRepo := sqlite.NewAuditLogRepo(db)
	txProvider := sqlite.NewTxProvider(db)

	logger := slog.Default()
	reconciler := service.NewReconciler(entRepo, eventRepo, notifRepo, auditRepo, txProvider, logger)
	handler := httphandler.New(reconciler)

	return handler, db
}

func newTestRouter(h *httphandler.Handler) *chi.Mux {
	r := chi.NewRouter()
	r.Mount("/", h.Routes())
	return r
}

func doRequest(t *testing.T, router *chi.Mux, method, path string, body interface{}) *http.Response {
	t.Helper()

	var reqBody io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		require.NoError(t, err)
		reqBody = io.NopCloser(newStringReader(string(b)))
	}

	req := httptest.NewRequest(method, path, reqBody)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w.Result()
}

type stringReader struct {
	s string
	i int
}

func newStringReader(s string) *stringReader {
	return &stringReader{s: s}
}

func (r *stringReader) Read(b []byte) (int, error) {
	if r.i >= len(r.s) {
		return 0, io.EOF
	}
	n := copy(b, r.s[r.i:])
	r.i += n
	return n, nil
}

func insertTestEntitlement(t *testing.T, db *sql.DB, userID, source string, active bool, expiresAt *string) {
	t.Helper()

	var expArg interface{}
	if expiresAt != nil {
		expArg = *expiresAt
	}

	_, err := db.Exec(
		`INSERT INTO entitlements (user_id, source, active, expires_at, reason, last_changed_at, last_event_time_ms, created_at)
		 VALUES (?, ?, ?, ?, ?, strftime('%Y-%m-%dT%H:%M:%fZ','now'), 0, strftime('%Y-%m-%dT%H:%M:%fZ','now'))`,
		userID, source, active, expArg, "",
	)
	require.NoError(t, err)
}

func TestIntegration_StoreWebhook_CreatesEntitlement(t *testing.T) {
	handler, _ := setupIntegration(t)
	router := newTestRouter(handler)

	webhook := map[string]interface{}{
		"eventId":     "evt_001",
		"userId":      "u_42",
		"type":        "INITIAL_PURCHASE",
		"eventTimeMs": 1716700000000,
		"productId":   "premium_monthly",
	}

	resp := doRequest(t, router, http.MethodPost, "/webhooks/store", webhook)
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var result map[string]string
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	assert.Equal(t, "processed", result["status"])

	// GET entitlement to verify
	resp2 := doRequest(t, router, http.MethodGet, "/users/u_42/entitlement", nil)
	defer func() { _ = resp2.Body.Close() }()

	assert.Equal(t, http.StatusOK, resp2.StatusCode)

	var ent map[string]interface{}
	require.NoError(t, json.NewDecoder(resp2.Body).Decode(&ent))
	assert.Equal(t, true, ent["active"])
	assert.Equal(t, "STORE", ent["source"])
	assert.NotNil(t, ent["expiresAt"])
}

func TestIntegration_StoreWebhook_DuplicateIgnored(t *testing.T) {
	handler, _ := setupIntegration(t)
	router := newTestRouter(handler)

	webhook := map[string]interface{}{
		"eventId":     "evt_002",
		"userId":      "u_43",
		"type":        "INITIAL_PURCHASE",
		"eventTimeMs": 1716700000000,
		"productId":   "premium_monthly",
	}

	// First request: processed
	resp1 := doRequest(t, router, http.MethodPost, "/webhooks/store", webhook)
	defer func() { _ = resp1.Body.Close() }()
	assert.Equal(t, http.StatusOK, resp1.StatusCode)

	var result1 map[string]string
	require.NoError(t, json.NewDecoder(resp1.Body).Decode(&result1))
	assert.Equal(t, "processed", result1["status"])

	// Second request with same eventId: ignored
	resp2 := doRequest(t, router, http.MethodPost, "/webhooks/store", webhook)
	defer func() { _ = resp2.Body.Close() }()
	assert.Equal(t, http.StatusOK, resp2.StatusCode)

	var result2 map[string]string
	require.NoError(t, json.NewDecoder(resp2.Body).Decode(&result2))
	assert.Equal(t, "ignored", result2["status"])
}

func TestIntegration_MarketplaceRevoke(t *testing.T) {
	handler, db := setupIntegration(t)
	router := newTestRouter(handler)

	// Create store entitlement via webhook
	storeWebhook := map[string]interface{}{
		"eventId":     "evt_010",
		"userId":      "u_50",
		"type":        "INITIAL_PURCHASE",
		"eventTimeMs": 1716700000000,
		"productId":   "premium_monthly",
	}
	resp := doRequest(t, router, http.MethodPost, "/webhooks/store", storeWebhook)
	_ = resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	// Manually insert a MARKETPLACE entitlement
	insertTestEntitlement(t, db, "u_50", "MARKETPLACE", true, nil)

	// POST marketplace revoke
	revokeReq := map[string]interface{}{
		"userIds": []string{"u_50"},
	}
	resp2 := doRequest(t, router, http.MethodPost, "/webhooks/marketplace/revoke", revokeReq)
	defer func() { _ = resp2.Body.Close() }()

	assert.Equal(t, http.StatusOK, resp2.StatusCode)

	var revokeResult map[string]interface{}
	require.NoError(t, json.NewDecoder(resp2.Body).Decode(&revokeResult))
	assert.Equal(t, float64(1), revokeResult["revoked"])
	assert.Equal(t, float64(0), revokeResult["skipped"])

	// Verify entitlement still shows STORE as active (STORE > MARKETPLACE priority)
	resp3 := doRequest(t, router, http.MethodGet, "/users/u_50/entitlement", nil)
	defer func() { _ = resp3.Body.Close() }()

	var ent map[string]interface{}
	require.NoError(t, json.NewDecoder(resp3.Body).Decode(&ent))
	assert.Equal(t, true, ent["active"])
	assert.Equal(t, "STORE", ent["source"])
}

func TestIntegration_GetTimeline(t *testing.T) {
	handler, _ := setupIntegration(t)
	router := newTestRouter(handler)

	webhook := map[string]interface{}{
		"eventId":     "evt_020",
		"userId":      "u_60",
		"type":        "INITIAL_PURCHASE",
		"eventTimeMs": 1716700000000,
		"productId":   "premium_monthly",
	}

	resp := doRequest(t, router, http.MethodPost, "/webhooks/store", webhook)
	_ = resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	// GET timeline
	resp2 := doRequest(t, router, http.MethodGet, "/users/u_60/timeline", nil)
	defer func() { _ = resp2.Body.Close() }()

	assert.Equal(t, http.StatusOK, resp2.StatusCode)

	var entries []map[string]interface{}
	require.NoError(t, json.NewDecoder(resp2.Body).Decode(&entries))
	require.Len(t, entries, 1)

	entry := entries[0]
	assert.Equal(t, "evt_020", entry["triggerId"])
	assert.Equal(t, "STORE", entry["source"])
}

func TestIntegration_StoreWebhook_Expiration(t *testing.T) {
	handler, _ := setupIntegration(t)
	router := newTestRouter(handler)

	// First: create entitlement via INITIAL_PURCHASE
	purchase := map[string]interface{}{
		"eventId":     "evt_030",
		"userId":      "u_70",
		"type":        "INITIAL_PURCHASE",
		"eventTimeMs": 1716700000000,
		"productId":   "premium_monthly",
	}
	resp := doRequest(t, router, http.MethodPost, "/webhooks/store", purchase)
	_ = resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	// Then: send EXPIRATION event
	expiration := map[string]interface{}{
		"eventId":     "evt_031",
		"userId":      "u_70",
		"type":        "EXPIRATION",
		"eventTimeMs": 1716700000001,
		"productId":   "premium_monthly",
	}
	resp2 := doRequest(t, router, http.MethodPost, "/webhooks/store", expiration)
	defer func() { _ = resp2.Body.Close() }()

	assert.Equal(t, http.StatusOK, resp2.StatusCode)

	var expResult map[string]string
	require.NoError(t, json.NewDecoder(resp2.Body).Decode(&expResult))
	assert.Equal(t, "processed", expResult["status"])

	// GET entitlement → should be inactive with EXPIRED reason
	resp3 := doRequest(t, router, http.MethodGet, "/users/u_70/entitlement", nil)
	defer func() { _ = resp3.Body.Close() }()

	var ent map[string]interface{}
	require.NoError(t, json.NewDecoder(resp3.Body).Decode(&ent))
	assert.Equal(t, false, ent["active"])
	reason, _ := ent["reason"].(string)
	assert.Equal(t, "EXPIRATION", reason)
}
