package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sulthonzh/subscription-reconciler/internal/domain"
)

func TestProcessStoreEvent_NewEvent(t *testing.T) {
	entRepo := newMockEntRepo()
	eventRepo := newMockEventRepo()
	notifRepo := newMockNotifRepo()
	auditRepo := newMockAuditRepo()

	r := NewReconciler(entRepo, eventRepo, notifRepo, auditRepo, mockTxProvider{}, testLogger())

	processed, err := r.ProcessStoreEvent(context.Background(), baseEvent())
	require.NoError(t, err)
	assert.True(t, processed)

	assert.Len(t, entRepo.upserted, 1)
	assert.Equal(t, "u_42", entRepo.upserted[0].UserID)
	assert.Equal(t, domain.SourceStore, entRepo.upserted[0].Source)
	assert.True(t, entRepo.upserted[0].Active)
	assert.NotNil(t, entRepo.upserted[0].ExpiresAt)

	assert.Len(t, notifRepo.scheduled, 1)
	assert.Equal(t, "u_42", notifRepo.scheduled[0].UserID)

	assert.Len(t, auditRepo.entries, 1)
	assert.Equal(t, "evt_001", auditRepo.entries[0].TriggerID)
}

func TestProcessStoreEvent_DuplicateEvent(t *testing.T) {
	entRepo := newMockEntRepo()
	eventRepo := newMockEventRepo()
	notifRepo := newMockNotifRepo()
	auditRepo := newMockAuditRepo()

	r := NewReconciler(entRepo, eventRepo, notifRepo, auditRepo, mockTxProvider{}, testLogger())

	event := baseEvent()
	_, _ = r.ProcessStoreEvent(context.Background(), event)

	processed, err := r.ProcessStoreEvent(context.Background(), event)
	require.NoError(t, err)
	assert.False(t, processed)
	assert.Len(t, entRepo.upserted, 1)
	assert.Len(t, auditRepo.entries, 1)
}

func TestProcessStoreEvent_StaleEvent(t *testing.T) {
	entRepo := newMockEntRepo()
	notifRepo := newMockNotifRepo()
	auditRepo := newMockAuditRepo()

	now := time.Now()
	expiresAt := now.Add(30 * 24 * time.Hour)
	entRepo.entitlements["u_42:STORE"] = &domain.Entitlement{
		UserID:          "u_42",
		Source:          domain.SourceStore,
		Active:          true,
		ExpiresAt:       &expiresAt,
		Reason:          "RENEWAL",
		LastChangedAt:   now,
		LastEventTimeMs: now.UnixMilli(),
		CreatedAt:       now,
	}

	r := NewReconciler(entRepo, newMockEventRepo(), notifRepo, auditRepo, mockTxProvider{}, testLogger())

	event := domain.StoreEvent{
		EventID:     "evt_stale",
		UserID:      "u_42",
		Type:        domain.EventInitialPurchase,
		EventTimeMs: now.Add(-2 * time.Hour).UnixMilli(),
		ProductID:   "premium_monthly",
	}

	processed, err := r.ProcessStoreEvent(context.Background(), event)
	require.NoError(t, err)
	assert.False(t, processed)
	assert.Len(t, entRepo.upserted, 0)
}

func TestProcessStoreEvent_LateArrival(t *testing.T) {
	entRepo := newMockEntRepo()
	notifRepo := newMockNotifRepo()
	auditRepo := newMockAuditRepo()

	now := time.Now()
	pastTime := now.Add(-2 * time.Hour)
	expiresAt := pastTime.Add(30 * 24 * time.Hour)
	entRepo.entitlements["u_42:STORE"] = &domain.Entitlement{
		UserID:          "u_42",
		Source:          domain.SourceStore,
		Active:          true,
		ExpiresAt:       &expiresAt,
		Reason:          "INITIAL_PURCHASE",
		LastChangedAt:   pastTime,
		LastEventTimeMs: pastTime.UnixMilli(),
		CreatedAt:       pastTime,
	}

	r := NewReconciler(entRepo, newMockEventRepo(), notifRepo, auditRepo, mockTxProvider{}, testLogger())

	event := domain.StoreEvent{
		EventID:     "evt_late",
		UserID:      "u_42",
		Type:        domain.EventRenewal,
		EventTimeMs: now.Add(-1 * time.Hour).UnixMilli(),
		ProductID:   "premium_monthly",
	}

	processed, err := r.ProcessStoreEvent(context.Background(), event)
	require.NoError(t, err)
	assert.True(t, processed)
	assert.Len(t, entRepo.upserted, 1)
	assert.Equal(t, "RENEWAL", entRepo.upserted[0].Reason)
}

func TestProcessStoreEvent_Cancellation(t *testing.T) {
	entRepo := newMockEntRepo()
	notifRepo := newMockNotifRepo()
	auditRepo := newMockAuditRepo()

	now := time.Now()
	expiresAt := now.Add(30 * 24 * time.Hour)
	entRepo.entitlements["u_42:STORE"] = &domain.Entitlement{
		UserID:          "u_42",
		Source:          domain.SourceStore,
		Active:          true,
		ExpiresAt:       &expiresAt,
		Reason:          "INITIAL_PURCHASE",
		LastChangedAt:   now.Add(-1 * time.Hour),
		LastEventTimeMs: now.Add(-1 * time.Hour).UnixMilli(),
		CreatedAt:       now.Add(-24 * time.Hour),
	}

	r := NewReconciler(entRepo, newMockEventRepo(), notifRepo, auditRepo, mockTxProvider{}, testLogger())

	event := domain.StoreEvent{
		EventID:     "evt_cancel",
		UserID:      "u_42",
		Type:        domain.EventCancellation,
		EventTimeMs: now.UnixMilli(),
		ProductID:   "premium_monthly",
	}

	processed, err := r.ProcessStoreEvent(context.Background(), event)
	require.NoError(t, err)
	assert.True(t, processed)
	assert.True(t, entRepo.upserted[0].Active, "cancellation should keep active=true")
	assert.Equal(t, "CANCELLATION", entRepo.upserted[0].Reason)
}

func TestProcessStoreEvent_Expiration(t *testing.T) {
	entRepo := newMockEntRepo()
	notifRepo := newMockNotifRepo()
	auditRepo := newMockAuditRepo()

	now := time.Now()
	expiresAt := now.Add(-1 * time.Hour)
	entRepo.entitlements["u_42:STORE"] = &domain.Entitlement{
		UserID:          "u_42",
		Source:          domain.SourceStore,
		Active:          true,
		ExpiresAt:       &expiresAt,
		Reason:          "INITIAL_PURCHASE",
		LastChangedAt:   now.Add(-2 * time.Hour),
		LastEventTimeMs: now.Add(-2 * time.Hour).UnixMilli(),
		CreatedAt:       now.Add(-24 * time.Hour),
	}

	r := NewReconciler(entRepo, newMockEventRepo(), notifRepo, auditRepo, mockTxProvider{}, testLogger())

	event := domain.StoreEvent{
		EventID:     "evt_expire",
		UserID:      "u_42",
		Type:        domain.EventExpiration,
		EventTimeMs: now.UnixMilli(),
		ProductID:   "premium_monthly",
	}

	processed, err := r.ProcessStoreEvent(context.Background(), event)
	require.NoError(t, err)
	assert.True(t, processed)
	assert.False(t, entRepo.upserted[0].Active, "expiration should set active=false")
	assert.Equal(t, "EXPIRATION", entRepo.upserted[0].Reason)
}

func TestProcessStoreEvent_BillingIssue_NoNotification(t *testing.T) {
	entRepo := newMockEntRepo()
	notifRepo := newMockNotifRepo()
	auditRepo := newMockAuditRepo()

	now := time.Now()
	expiresAt := now.Add(30 * 24 * time.Hour)
	entRepo.entitlements["u_42:STORE"] = &domain.Entitlement{
		UserID:          "u_42",
		Source:          domain.SourceStore,
		Active:          true,
		ExpiresAt:       &expiresAt,
		Reason:          "RENEWAL",
		LastChangedAt:   now.Add(-1 * time.Hour),
		LastEventTimeMs: now.Add(-1 * time.Hour).UnixMilli(),
		CreatedAt:       now.Add(-24 * time.Hour),
	}

	r := NewReconciler(entRepo, newMockEventRepo(), notifRepo, auditRepo, mockTxProvider{}, testLogger())

	event := domain.StoreEvent{
		EventID:     "evt_billing",
		UserID:      "u_42",
		Type:        domain.EventBillingIssue,
		EventTimeMs: now.UnixMilli(),
		ProductID:   "premium_monthly",
	}

	processed, err := r.ProcessStoreEvent(context.Background(), event)
	require.NoError(t, err)
	assert.True(t, processed)
	assert.True(t, entRepo.upserted[0].Active)
	assert.Equal(t, "BILLING_ISSUE", entRepo.upserted[0].Reason)
	assert.Len(t, notifRepo.scheduled, 0, "billing issue should not schedule notification")
}

func TestProcessStoreEvent_NilAuditRepo(t *testing.T) {
	entRepo := newMockEntRepo()
	r := NewReconciler(entRepo, newMockEventRepo(), newMockNotifRepo(), nil, mockTxProvider{}, testLogger())

	processed, err := r.ProcessStoreEvent(context.Background(), baseEvent())
	require.NoError(t, err)
	assert.True(t, processed)
}

func TestProcessStoreEvent_InsertError(t *testing.T) {
	eventRepo := newMockEventRepo()
	eventRepo.insertErr = fmt.Errorf("db down")

	r := NewReconciler(newMockEntRepo(), eventRepo, newMockNotifRepo(), nil, mockTxProvider{}, testLogger())

	_, err := r.ProcessStoreEvent(context.Background(), baseEvent())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "insert store event")
}

func TestProcessStoreEvent_GetEntitlementError(t *testing.T) {
	entRepo := newMockEntRepo()
	entRepo.getByUserAndSourceErr = fmt.Errorf("db down")

	r := NewReconciler(entRepo, newMockEventRepo(), newMockNotifRepo(), nil, mockTxProvider{}, testLogger())

	_, err := r.ProcessStoreEvent(context.Background(), baseEvent())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "get entitlement")
}

func TestProcessStoreEvent_UpsertError(t *testing.T) {
	entRepo := newMockEntRepo()
	entRepo.upsertErr = fmt.Errorf("db down")

	r := NewReconciler(entRepo, newMockEventRepo(), newMockNotifRepo(), nil, mockTxProvider{}, testLogger())

	_, err := r.ProcessStoreEvent(context.Background(), baseEvent())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "upsert entitlement")
}

func TestProcessStoreEvent_ScheduleError(t *testing.T) {
	entRepo := newMockEntRepo()
	notifRepo := newMockNotifRepo()
	notifRepo.scheduleErr = fmt.Errorf("queue down")

	r := NewReconciler(entRepo, newMockEventRepo(), notifRepo, nil, mockTxProvider{}, testLogger())

	processed, err := r.ProcessStoreEvent(context.Background(), baseEvent())
	require.NoError(t, err)
	assert.True(t, processed, "schedule error should not fail the event")
}

func TestProcessStoreEvent_AuditInsertError(t *testing.T) {
	entRepo := newMockEntRepo()
	auditRepo := newMockAuditRepo()
	auditRepo.insertErr = fmt.Errorf("audit down")

	r := NewReconciler(entRepo, newMockEventRepo(), newMockNotifRepo(), auditRepo, mockTxProvider{}, testLogger())

	processed, err := r.ProcessStoreEvent(context.Background(), baseEvent())
	require.NoError(t, err)
	assert.True(t, processed, "audit error should not fail the event")
}

func TestProcessStoreEvent_OutOfOrderEvents(t *testing.T) {
	entRepo := newMockEntRepo()
	notifRepo := newMockNotifRepo()
	auditRepo := newMockAuditRepo()

	now := time.Now()
	r := NewReconciler(entRepo, newMockEventRepo(), notifRepo, auditRepo, mockTxProvider{}, testLogger())

	// Event 2 arrives first (newer) - RENEWAL
	renewalEvent := domain.StoreEvent{
		EventID:     "evt_renewal",
		UserID:      "u_42",
		Type:        domain.EventRenewal,
		EventTimeMs: now.UnixMilli(),
		ProductID:   "premium_monthly",
	}
	processed, err := r.ProcessStoreEvent(context.Background(), renewalEvent)
	require.NoError(t, err)
	assert.True(t, processed)
	assert.Equal(t, "RENEWAL", entRepo.upserted[0].Reason)

	// Event 1 arrives later (older) - INITIAL_PURCHASE
	purchaseEvent := domain.StoreEvent{
		EventID:     "evt_purchase",
		UserID:      "u_42",
		Type:        domain.EventInitialPurchase,
		EventTimeMs: now.Add(-2 * time.Hour).UnixMilli(),
		ProductID:   "premium_monthly",
	}
	processed, err = r.ProcessStoreEvent(context.Background(), purchaseEvent)
	require.NoError(t, err)
	assert.False(t, processed, "older event should be ignored as stale")

	// Verify entitlement still reflects RENEWAL, not INITIAL_PURCHASE
	assert.Len(t, entRepo.upserted, 1)
	assert.Equal(t, "RENEWAL", entRepo.upserted[0].Reason)
	assert.True(t, entRepo.upserted[0].Active)
}
