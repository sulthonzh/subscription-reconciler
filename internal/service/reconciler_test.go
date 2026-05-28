package service

import (
	"context"
	"log/slog"
	"os"
	"time"

	"github.com/sulthonzh/subscription-reconciler/internal/domain"
)

type mockEntitlementRepo struct {
	entitlements map[string]*domain.Entitlement
	upserted     []domain.Entitlement
	updated      []updateCall

	getByUserAndSourceErr error
	getByUserErr          error
	upsertErr             error
	getActiveBySourceErr  error
	updateActiveErr       error
	expireOverdueErr      error
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
	return count, nil
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
}

func newMockNotifRepo() *mockNotificationRepo {
	return &mockNotificationRepo{}
}

func (m *mockNotificationRepo) Schedule(_ context.Context, notification domain.Notification) (bool, error) {
	if m.scheduleErr != nil {
		return false, m.scheduleErr
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
	entries   []domain.AuditEntry
	insertErr error
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

func baseEvent() domain.StoreEvent {
	return domain.StoreEvent{
		EventID:     "evt_001",
		UserID:      "u_42",
		Type:        domain.EventInitialPurchase,
		EventTimeMs: time.Now().Add(-1 * time.Hour).UnixMilli(),
		ProductID:   "premium_monthly",
	}
}
