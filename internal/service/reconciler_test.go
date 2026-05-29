package service

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/assert"
	"github.com/sulthonzh/subscription-reconciler/internal/domain"
)

type mockEntitlementRepo struct {
	entitlements            map[string]*domain.Entitlement
	upserted               []domain.Entitlement
	updated                []updateCall
	expireOverdueCount     int
	getByUserAndSourceErr  error
	getByUserErr           error
	upsertErr              error
	getActiveBySourceErr   error
	updateActiveErr       error
	expireOverdueErr      error
	getExpiringBeforeErr   error
}

type updateCall struct {
	userID string
	source domain.Source
	active bool
	reason string
}

func newMockEntRepo() *mockEntitlementRepo {
	return &mockEntitlementRepo{entitlements: make(map[string]*domain.Entitlement)}
}

func (m *mockEntitlementRepo) key(userID string, source domain.Source) string {
	return userID + ":" + string(source)
}

func (m *mockEntitlementRepo) GetByUserAndSource(_ context.Context, userID string, source domain.Source) (*domain.Entitlement, error) {
	if m.getByUserAndSourceErr != nil {
		return nil, m.getByUserAndSourceErr
	}
	if e, ok := m.entitlements[m.key(userID, source)]; ok {
		return e, nil
	}
	return nil, nil
}

func (m *mockEntitlementRepo) GetByUser(_ context.Context, userID string) ([]domain.Entitlement, error) {
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

func (m *mockEntitlementRepo) Upsert(_ context.Context, entitlement domain.Entitlement) error {
	if m.upsertErr != nil {
		return m.upsertErr
	}
	m.upserted = append(m.upserted, entitlement)
	k := m.key(entitlement.UserID, entitlement.Source)
	entitlementCopy := entitlement
	m.entitlements[k] = &entitlementCopy
	return nil
}

func (m *mockEntitlementRepo) GetActiveBySource(_ context.Context, source domain.Source) ([]domain.Entitlement, error) {
	if m.getActiveBySourceErr != nil {
		return nil, m.getActiveBySourceErr
	}
	var result []domain.Entitlement
	for _, e := range m.entitlements {
		if e.Source == source && e.Active {
			result = append(result, *e)
		}
	}
	return result, nil
}

func (m *mockEntitlementRepo) UpdateActive(_ context.Context, userID string, source domain.Source, active bool, reason string) error {
	if m.updateActiveErr != nil {
		return m.updateActiveErr
	}
	m.updated = append(m.updated, updateCall{userID, source, active, reason})
	k := m.key(userID, source)
	if e, ok := m.entitlements[k]; ok {
		e.Active = active
		e.Reason = reason
		e.LastChangedAt = time.Now()
	}
	return nil
}

func (m *mockEntitlementRepo) ExpireOverdue(_ context.Context, now time.Time) (int, error) {
	if m.expireOverdueErr != nil {
		return 0, m.expireOverdueErr
	}
	count := 0
	for _, e := range m.entitlements {
		if e.Active && e.ExpiresAt != nil && e.ExpiresAt.Before(now) {
			e.Active = false
			e.Reason = "EXPIRED"
			count++
		}
	}
	m.expireOverdueCount = count
	return count, nil
}

func (m *mockEntitlementRepo) GetExpiringBefore(_ context.Context, before time.Time) ([]domain.Entitlement, error) {
	if m.getExpiringBeforeErr != nil {
		return nil, m.getExpiringBeforeErr
	}
	var result []domain.Entitlement
	for _, e := range m.entitlements {
		if e.Active && e.ExpiresAt != nil && !e.ExpiresAt.After(before) {
			result = append(result, *e)
		}
	}
	return result, nil
}

type mockStoreEventRepo struct {
	events    map[string]bool
	insertErr error
}

func newMockEventRepo() *mockStoreEventRepo {
	return &mockStoreEventRepo{events: make(map[string]bool)}
}

func (m *mockStoreEventRepo) Insert(_ context.Context, event domain.StoreEvent) (bool, error) {
	if m.insertErr != nil {
		return false, m.insertErr
	}
	if m.events[event.EventID] {
		return false, nil
	}
	m.events[event.EventID] = true
	return true, nil
}

type mockNotificationRepo struct {
	scheduled   []domain.Notification
	due         []domain.Notification
	marked      []int64
	scheduleErr error
	findDueErr  error
	markSentErr error
	newScheduleReturnFalse bool
	firstScheduleFail     bool
}

func newMockNotifRepo() *mockNotificationRepo {
	return &mockNotificationRepo{}
}

func (m *mockNotificationRepo) Schedule(_ context.Context, notification domain.Notification) (bool, error) {
	if m.scheduleErr != nil {
		return false, m.scheduleErr
	}
	if m.newScheduleReturnFalse {
		m.scheduled = append(m.scheduled, notification)
		return false, nil // Simulate duplicate (already scheduled)
	}
	if m.firstScheduleFail {
		m.firstScheduleFail = false // Reset for subsequent calls
		m.scheduled = append(m.scheduled, notification)
		return false, fmt.Errorf("first fail") // Return false but store for counting
	}
	m.scheduled = append(m.scheduled, notification)
	return true, nil
}

func (m *mockNotificationRepo) FindDue(_ context.Context, now time.Time, limit int) ([]domain.Notification, error) {
	if m.findDueErr != nil {
		return nil, m.findDueErr
	}
	return m.due, nil
}

func (m *mockNotificationRepo) MarkSent(_ context.Context, id int64, now time.Time) error {
	if m.markSentErr != nil {
		return m.markSentErr
	}
	m.marked = append(m.marked, id)
	return nil
}

type mockAuditLogRepo struct {
	entries     []domain.AuditEntry
	insertErr   error
	getByUserErr error
}

func newMockAuditRepo() *mockAuditLogRepo {
	return &mockAuditLogRepo{}
}

func (m *mockAuditLogRepo) Insert(_ context.Context, entry domain.AuditEntry) error {
	if m.insertErr != nil {
		return m.insertErr
	}
	m.entries = append(m.entries, entry)
	return nil
}

func (m *mockAuditLogRepo) GetByUser(_ context.Context, userID string) ([]domain.AuditEntry, error) {
	if m.getByUserErr != nil {
		return nil, m.getByUserErr
	}
	var result []domain.AuditEntry
	for _, e := range m.entries {
		if e.UserID == userID {
			result = append(result, e)
		}
	}
	return result, nil
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

type mockTxProvider struct{}

func (mockTxProvider) WithinTx(ctx context.Context, fn func(context.Context) error) error {
	return fn(ctx)
}

func baseEvent() domain.StoreEvent {
	return domain.StoreEvent{
		EventID:     "evt_001",
		UserID:      "u_42",
		Type:        domain.EventInitialPurchase,
		EventTimeMs: time.Now().Add(-1 * time.Hour).UnixMilli(),
		ProductID:   "premium_monthly",
	}
}

func TestGetTimeline_WithAuditEntries(t *testing.T) {
	auditRepo := newMockAuditRepo()
	now := time.Now()
	
	auditRepo.entries = []domain.AuditEntry{
		{
			ID:         1,
			UserID:     "u_42",
			TriggerID:  "evt_001",
			Source:     domain.SourceStore,
			PreviousState: "{}",
			NextState:     `{"active":true,"source":"STORE","reason":"INITIAL_PURCHASE"}`,
			CreatedAt:   now,
		},
		{
			ID:         2,
			UserID:     "u_42",
			TriggerID:  "evt_002",
			Source:     domain.SourceStore,
			PreviousState: `{"active":true,"source":"STORE","reason":"INITIAL_PURCHASE"}`,
			NextState:     `{"active":false,"source":"STORE","reason":"EXPIRATION"}`,
			CreatedAt:   now.Add(1 * time.Hour),
		},
	}

	r := NewReconciler(nil, nil, nil, auditRepo, mockTxProvider{}, testLogger())

	timeline, err := r.GetTimeline(context.Background(), "u_42")
	require.NoError(t, err)
	assert.Len(t, timeline, 2)
	assert.Equal(t, "evt_001", timeline[0].TriggerID)
	assert.Equal(t, "evt_002", timeline[1].TriggerID)
}

func TestGetTimeline_NoEntries(t *testing.T) {
	auditRepo := newMockAuditRepo()

	r := NewReconciler(nil, nil, nil, auditRepo, mockTxProvider{}, testLogger())

	timeline, err := r.GetTimeline(context.Background(), "u_42")
	require.NoError(t, err)
	assert.Empty(t, timeline)
}

func TestGetTimeline_GetByUserError(t *testing.T) {
	auditRepo := newMockAuditRepo()
	auditRepo.getByUserErr = fmt.Errorf("db down")

	r := NewReconciler(nil, nil, nil, auditRepo, mockTxProvider{}, testLogger())

	_, err := r.GetTimeline(context.Background(), "u_42")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "db down")
}
