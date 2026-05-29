package service

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/sulthonzh/subscription-reconciler/internal/domain"
	"github.com/sulthonzh/subscription-reconciler/internal/port"
)

type mockCarrierClient struct {
	status string
	err    error
	called int
	mu     sync.Mutex
}

func (m *mockCarrierClient) CheckPlan(_ context.Context, _ string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.called++
	if m.err != nil {
		return "api_error", m.err
	}
	return m.status, nil
}

type mockPollLogRepo struct {
	inserts []pollInsert
	locks   map[string]bool
	mu      sync.Mutex

	acquireLockErr error
	releaseLockErr error
	insertErr      error
}

type pollInsert struct {
	userID string
	status string
}

func newMockPollLogRepo() *mockPollLogRepo {
	return &mockPollLogRepo{locks: make(map[string]bool)}
}

func (m *mockPollLogRepo) Insert(_ context.Context, userID string, status string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.insertErr != nil {
		return m.insertErr
	}
	m.inserts = append(m.inserts, pollInsert{userID, status})
	return nil
}

func (m *mockPollLogRepo) AcquireLock(_ context.Context, userID string, _ time.Time) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.acquireLockErr != nil {
		return false, m.acquireLockErr
	}
	if m.locks[userID] {
		return false, nil
	}
	m.locks[userID] = true
	return true, nil
}

func (m *mockPollLogRepo) ReleaseLock(_ context.Context, userID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.releaseLockErr != nil {
		return m.releaseLockErr
	}
	delete(m.locks, userID)
	return nil
}

func TestPollAll_ActiveUser(t *testing.T) {
	entRepo := newMockEntRepo()
	now := time.Now()
	entRepo.entitlements["u_42:CARRIER"] = &domain.Entitlement{
		UserID: "u_42", Source: domain.SourceCarrier,
		Active: true, Reason: "ACTIVE", LastChangedAt: now, CreatedAt: now,
	}

	carrier := &mockCarrierClient{status: "active"}
	pollRepo := newMockPollLogRepo()

	p := NewPoller(entRepo, pollRepo, carrier, nil, testLogger())
	p.PollAll(context.Background())

	assert.Equal(t, 1, carrier.called)
	assert.Len(t, pollRepo.inserts, 1)
	assert.Equal(t, "active", pollRepo.inserts[0].status)
	assert.True(t, entRepo.entitlements["u_42:CARRIER"].Active,
		"active response should not deactivate")
}

func TestPollAll_InactiveUser(t *testing.T) {
	entRepo := newMockEntRepo()
	now := time.Now()
	entRepo.entitlements["u_42:CARRIER"] = &domain.Entitlement{
		UserID: "u_42", Source: domain.SourceCarrier,
		Active: true, Reason: "ACTIVE", LastChangedAt: now, CreatedAt: now,
	}

	carrier := &mockCarrierClient{status: "inactive"}
	pollRepo := newMockPollLogRepo()
	auditRepo := newMockAuditRepo()

	p := NewPoller(entRepo, pollRepo, carrier, auditRepo, testLogger())
	p.PollAll(context.Background())

	assert.Equal(t, 1, carrier.called)
	assert.False(t, entRepo.entitlements["u_42:CARRIER"].Active,
		"inactive response should deactivate")
	assert.Equal(t, "CARRIER_INACTIVE", entRepo.entitlements["u_42:CARRIER"].Reason)
	assert.Len(t, auditRepo.entries, 1, "should write audit entry on deactivation")
	assert.Equal(t, "u_42", auditRepo.entries[0].UserID)
	assert.Equal(t, "carrier_poll", auditRepo.entries[0].TriggerID)
	assert.Equal(t, domain.SourceCarrier, auditRepo.entries[0].Source)
	assert.Contains(t, auditRepo.entries[0].PreviousState, "\"active\":true")
	assert.Contains(t, auditRepo.entries[0].NextState, "\"active\":false")
	assert.Contains(t, auditRepo.entries[0].NextState, "\"reason\":\"CARRIER_INACTIVE\"")
}

func TestPollAll_ApiError(t *testing.T) {
	entRepo := newMockEntRepo()
	now := time.Now()
	entRepo.entitlements["u_42:CARRIER"] = &domain.Entitlement{
		UserID: "u_42", Source: domain.SourceCarrier,
		Active: true, Reason: "ACTIVE", LastChangedAt: now, CreatedAt: now,
	}

	carrier := &mockCarrierClient{status: "api_error"}
	pollRepo := newMockPollLogRepo()

	p := NewPoller(entRepo, pollRepo, carrier, nil, testLogger())
	p.PollAll(context.Background())

	assert.Equal(t, 1, carrier.called)
	assert.True(t, entRepo.entitlements["u_42:CARRIER"].Active,
		"api_error should not change state")
	assert.Len(t, pollRepo.inserts, 1)
	assert.Equal(t, "api_error", pollRepo.inserts[0].status)
}

func TestPollAll_LockedUser(t *testing.T) {
	entRepo := newMockEntRepo()
	now := time.Now()
	entRepo.entitlements["u_42:CARRIER"] = &domain.Entitlement{
		UserID: "u_42", Source: domain.SourceCarrier,
		Active: true, Reason: "ACTIVE", LastChangedAt: now, CreatedAt: now,
	}

	carrier := &mockCarrierClient{status: "active"}
	pollRepo := newMockPollLogRepo()
	pollRepo.locks["u_42"] = true // pre-lock

	p := NewPoller(entRepo, pollRepo, carrier, nil, testLogger())
	p.PollAll(context.Background())

	assert.Equal(t, 0, carrier.called, "locked user should not be polled")
}

func TestPollAll_NoActiveUsers(t *testing.T) {
	entRepo := newMockEntRepo()
	carrier := &mockCarrierClient{status: "active"}
	pollRepo := newMockPollLogRepo()

	p := NewPoller(entRepo, pollRepo, carrier, nil, testLogger())
	p.PollAll(context.Background())

	assert.Equal(t, 0, carrier.called)
}

func TestPoller_Run_RespectsContext(t *testing.T) {
	entRepo := newMockEntRepo()
	carrier := &mockCarrierClient{status: "active"}
	pollRepo := newMockPollLogRepo()

	p := NewPoller(entRepo, pollRepo, carrier, nil, testLogger())

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	p.Run(ctx, 10*time.Millisecond)
}

func TestPollAll_GetActiveBySourceError(t *testing.T) {
	entRepo := newMockEntRepo()
	entRepo.getActiveBySourceErr = fmt.Errorf("db down")
	carrier := &mockCarrierClient{status: "active"}
	pollRepo := newMockPollLogRepo()

	p := NewPoller(entRepo, pollRepo, carrier, nil, testLogger())
	p.PollAll(context.Background())

	assert.Equal(t, 0, carrier.called)
}

func TestPollAll_CarrierCheckError(t *testing.T) {
	entRepo := newMockEntRepo()
	now := time.Now()
	entRepo.entitlements["u_42:CARRIER"] = &domain.Entitlement{
		UserID: "u_42", Source: domain.SourceCarrier,
		Active: true, Reason: "ACTIVE", LastChangedAt: now, CreatedAt: now,
	}

	carrier := &mockCarrierClient{status: "active", err: fmt.Errorf("network timeout")}
	pollRepo := newMockPollLogRepo()

	p := NewPoller(entRepo, pollRepo, carrier, nil, testLogger())
	p.PollAll(context.Background())

	assert.Equal(t, 1, carrier.called)
	assert.True(t, entRepo.entitlements["u_42:CARRIER"].Active,
		"carrier error should not change state")
	assert.Len(t, pollRepo.inserts, 1)
	assert.Equal(t, "api_error", pollRepo.inserts[0].status)
}

func TestPollAll_AcquireLockError(t *testing.T) {
	entRepo := newMockEntRepo()
	now := time.Now()
	entRepo.entitlements["u_42:CARRIER"] = &domain.Entitlement{
		UserID: "u_42", Source: domain.SourceCarrier,
		Active: true, Reason: "ACTIVE", LastChangedAt: now, CreatedAt: now,
	}

	carrier := &mockCarrierClient{status: "active"}
	pollRepo := newMockPollLogRepo()
	pollRepo.acquireLockErr = fmt.Errorf("lock fail")

	p := NewPoller(entRepo, pollRepo, carrier, nil, testLogger())
	p.PollAll(context.Background())

	assert.Equal(t, 0, carrier.called, "lock error should skip user")
}

func TestPollAll_InsertError(t *testing.T) {
	entRepo := newMockEntRepo()
	now := time.Now()
	entRepo.entitlements["u_42:CARRIER"] = &domain.Entitlement{
		UserID: "u_42", Source: domain.SourceCarrier,
		Active: true, Reason: "ACTIVE", LastChangedAt: now, CreatedAt: now,
	}

	carrier := &mockCarrierClient{status: "active"}
	pollRepo := newMockPollLogRepo()
	pollRepo.insertErr = fmt.Errorf("insert fail")

	p := NewPoller(entRepo, pollRepo, carrier, nil, testLogger())
	p.PollAll(context.Background())

	assert.Equal(t, 1, carrier.called)
}

func TestPollAll_ReleaseLockError(t *testing.T) {
	entRepo := newMockEntRepo()
	now := time.Now()
	entRepo.entitlements["u_42:CARRIER"] = &domain.Entitlement{
		UserID: "u_42", Source: domain.SourceCarrier,
		Active: true, Reason: "ACTIVE", LastChangedAt: now, CreatedAt: now,
	}

	carrier := &mockCarrierClient{status: "active"}
	pollRepo := newMockPollLogRepo()
	pollRepo.releaseLockErr = fmt.Errorf("release fail")

	p := NewPoller(entRepo, pollRepo, carrier, nil, testLogger())
	p.PollAll(context.Background())

	assert.Equal(t, 1, carrier.called)
}

func TestPollAll_UpdateActiveError(t *testing.T) {
	entRepo := newMockEntRepo()
	now := time.Now()
	entRepo.entitlements["u_42:CARRIER"] = &domain.Entitlement{
		UserID: "u_42", Source: domain.SourceCarrier,
		Active: true, Reason: "ACTIVE", LastChangedAt: now, CreatedAt: now,
	}
	entRepo.updateActiveErr = fmt.Errorf("update fail")

	carrier := &mockCarrierClient{status: "inactive"}
	pollRepo := newMockPollLogRepo()

	p := NewPoller(entRepo, pollRepo, carrier, nil, testLogger())
	p.PollAll(context.Background())

	assert.Equal(t, 1, carrier.called)
}

func TestPoller_Run_TickerFires(t *testing.T) {
	entRepo := newMockEntRepo()
	now := time.Now()
	entRepo.entitlements["u_42:CARRIER"] = &domain.Entitlement{
		UserID: "u_42", Source: domain.SourceCarrier,
		Active: true, Reason: "ACTIVE", LastChangedAt: now, CreatedAt: now,
	}

	carrier := &mockCarrierClient{status: "active"}
	pollRepo := newMockPollLogRepo()

	p := NewPoller(entRepo, pollRepo, carrier, nil, testLogger())

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	p.Run(ctx, 5*time.Millisecond)

	assert.True(t, carrier.called >= 1, "ticker should fire and poll user")
}

func TestPollAll_ConcurrentWorkers_NoDoubleProcessing(t *testing.T) {
	entRepo := newMockEntRepo()
	now := time.Now()

	// Set up 3 carrier users
	entRepo.entitlements["u_1:CARRIER"] = &domain.Entitlement{
		UserID: "u_1", Source: domain.SourceCarrier,
		Active: true, Reason: "ACTIVE", LastChangedAt: now, CreatedAt: now,
	}
	entRepo.entitlements["u_2:CARRIER"] = &domain.Entitlement{
		UserID: "u_2", Source: domain.SourceCarrier,
		Active: true, Reason: "ACTIVE", LastChangedAt: now, CreatedAt: now,
	}
	entRepo.entitlements["u_3:CARRIER"] = &domain.Entitlement{
		UserID: "u_3", Source: domain.SourceCarrier,
		Active: true, Reason: "ACTIVE", LastChangedAt: now, CreatedAt: now,
	}

	carrier := &mockCarrierClient{status: "active"}
	pollRepo := newMockPollLogRepo()
	pollRepo.releaseLockErr = fmt.Errorf("lock held")

	p := NewPoller(entRepo, pollRepo, carrier, nil, testLogger())

	// Run 3 concurrent PollAll calls
	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p.PollAll(context.Background())
		}()
	}
	wg.Wait()

	// Due to locking, each user should be polled at most once
	// pollRepo.inserts should have at most 3 entries (one per user)
	assert.LessOrEqual(t, len(pollRepo.inserts), 3, "concurrent workers should not double-process users")

	// Each user should appear at most once in inserts
	userCounts := make(map[string]int)
	for _, ins := range pollRepo.inserts {
		userCounts[ins.userID]++
	}
	for user, count := range userCounts {
		assert.Equal(t, 1, count, "user %s should be polled exactly once, got %d", user, count)
	}
}

func TestPollUser_CarrierError_ApiErrorPath(t *testing.T) {
	entRepo := newMockEntRepo()
	now := time.Now()
	entRepo.entitlements["u_42:CARRIER"] = &domain.Entitlement{
		UserID: "u_42", Source: domain.SourceCarrier,
		Active: true, Reason: "ACTIVE", LastChangedAt: now, CreatedAt: now,
	}

	carrier := &mockCarrierClient{status: "active", err: fmt.Errorf("network timeout")}
	pollRepo := newMockPollLogRepo()

	p := NewPoller(entRepo, pollRepo, carrier, nil, testLogger())
	p.pollUser(context.Background(), "u_42")

	assert.Equal(t, 1, carrier.called)
	assert.True(t, entRepo.entitlements["u_42:CARRIER"].Active,
		"carrier error should not change state")
	assert.Len(t, pollRepo.inserts, 1)
	assert.Equal(t, "api_error", pollRepo.inserts[0].status)
}

func TestPollUser_InactiveStatus(t *testing.T) {
	entRepo := newMockEntRepo()
	now := time.Now()
	entRepo.entitlements["u_42:CARRIER"] = &domain.Entitlement{
		UserID: "u_42", Source: domain.SourceCarrier,
		Active: true, Reason: "ACTIVE", LastChangedAt: now, CreatedAt: now,
	}

	carrier := &mockCarrierClient{status: "inactive"}
	pollRepo := newMockPollLogRepo()
	auditRepo := newMockAuditRepo()

	p := NewPoller(entRepo, pollRepo, carrier, auditRepo, testLogger())
	p.pollUser(context.Background(), "u_42")

	assert.Equal(t, 1, carrier.called)
	assert.False(t, entRepo.entitlements["u_42:CARRIER"].Active,
		"inactive response should deactivate")
	assert.Equal(t, "CARRIER_INACTIVE", entRepo.entitlements["u_42:CARRIER"].Reason)
	assert.Len(t, auditRepo.entries, 1, "should write audit entry on deactivation")
}

func TestPollUser_AcquireLockError(t *testing.T) {
	entRepo := newMockEntRepo()
	now := time.Now()
	entRepo.entitlements["u_42:CARRIER"] = &domain.Entitlement{
		UserID: "u_42", Source: domain.SourceCarrier,
		Active: true, Reason: "ACTIVE", LastChangedAt: now, CreatedAt: now,
	}

	carrier := &mockCarrierClient{status: "active"}
	pollRepo := newMockPollLogRepo()
	pollRepo.acquireLockErr = fmt.Errorf("lock acquisition failed")

	p := NewPoller(entRepo, pollRepo, carrier, nil, testLogger())
	p.pollUser(context.Background(), "u_42")

	assert.Equal(t, 0, carrier.called, "lock error should skip polling")
	assert.Len(t, pollRepo.inserts, 0, "should not log poll result when lock fails")
}

func TestPollUser_ReleaseLockError(t *testing.T) {
	entRepo := newMockEntRepo()
	now := time.Now()
	entRepo.entitlements["u_42:CARRIER"] = &domain.Entitlement{
		UserID: "u_42", Source: domain.SourceCarrier,
		Active: true, Reason: "ACTIVE", LastChangedAt: now, CreatedAt: now,
	}

	carrier := &mockCarrierClient{status: "active"}
	pollRepo := newMockPollLogRepo()
	pollRepo.releaseLockErr = fmt.Errorf("lock release failed")

	p := NewPoller(entRepo, pollRepo, carrier, nil, testLogger())
	p.pollUser(context.Background(), "u_42")

	assert.Equal(t, 1, carrier.called)
	assert.Len(t, pollRepo.inserts, 1, "should still log poll result")
}

func TestPollUser_GetByUserAndSourceError(t *testing.T) {
	entRepo := newMockEntRepo()
	entRepo.getByUserAndSourceErr = fmt.Errorf("db error")
	now := time.Now()
	entRepo.entitlements["u_42:CARRIER"] = &domain.Entitlement{
		UserID: "u_42", Source: domain.SourceCarrier,
		Active: true, Reason: "ACTIVE", LastChangedAt: now, CreatedAt: now,
	}

	carrier := &mockCarrierClient{status: "inactive"}
	pollRepo := newMockPollLogRepo()

	p := NewPoller(entRepo, pollRepo, carrier, nil, testLogger())
	p.pollUser(context.Background(), "u_42")

	assert.Equal(t, 1, carrier.called)
	assert.Len(t, pollRepo.inserts, 1)
	assert.Equal(t, "inactive", pollRepo.inserts[0].status)
}

func TestPollUser_ActiveStatus(t *testing.T) {
	entRepo := newMockEntRepo()
	now := time.Now()
	entRepo.entitlements["u_42:CARRIER"] = &domain.Entitlement{
		UserID: "u_42", Source: domain.SourceCarrier,
		Active: true, Reason: "ACTIVE", LastChangedAt: now, CreatedAt: now,
	}

	carrier := &mockCarrierClient{status: "active"}
	pollRepo := newMockPollLogRepo()

	p := NewPoller(entRepo, pollRepo, carrier, nil, testLogger())
	p.pollUser(context.Background(), "u_42")

	assert.Equal(t, 1, carrier.called)
	assert.Len(t, pollRepo.inserts, 1)
	assert.Equal(t, "active", pollRepo.inserts[0].status)
	assert.True(t, entRepo.entitlements["u_42:CARRIER"].Active, "should remain active")
}

func TestPollUser_InsertError(t *testing.T) {
	entRepo := newMockEntRepo()
	now := time.Now()
	entRepo.entitlements["u_42:CARRIER"] = &domain.Entitlement{
		UserID: "u_42", Source: domain.SourceCarrier,
		Active: true, Reason: "ACTIVE", LastChangedAt: now, CreatedAt: now,
	}

	carrier := &mockCarrierClient{status: "active"}
	pollRepo := newMockPollLogRepo()
	pollRepo.insertErr = fmt.Errorf("insert failed")

	p := NewPoller(entRepo, pollRepo, carrier, nil, testLogger())
	p.pollUser(context.Background(), "u_42")

	assert.Equal(t, 1, carrier.called, "should still call carrier even on insert error")
	assert.Len(t, pollRepo.inserts, 0, "should not insert when Insert returns error")
	assert.True(t, entRepo.entitlements["u_42:CARRIER"].Active, "should remain active")
}

func TestPollUser_UpdateActiveError(t *testing.T) {
	entRepo := newMockEntRepo()
	now := time.Now()
	entRepo.entitlements["u_42:CARRIER"] = &domain.Entitlement{
		UserID: "u_42", Source: domain.SourceCarrier,
		Active: true, Reason: "ACTIVE", LastChangedAt: now, CreatedAt: now,
	}
	entRepo.updateActiveErr = fmt.Errorf("update failed")

	carrier := &mockCarrierClient{status: "inactive"}
	pollRepo := newMockPollLogRepo()
	auditRepo := newMockAuditRepo()

	p := NewPoller(entRepo, pollRepo, carrier, auditRepo, testLogger())
	p.pollUser(context.Background(), "u_42")

	assert.Equal(t, 1, carrier.called)
	assert.Len(t, pollRepo.inserts, 1, "should still log poll result")
	assert.Equal(t, "inactive", pollRepo.inserts[0].status)
	assert.True(t, entRepo.entitlements["u_42:CARRIER"].Active, "should still be active due to update error")
	assert.Len(t, auditRepo.entries, 0, "should not write audit entry when update fails")
}

func TestPollUser_AuditInsertError(t *testing.T) {
	entRepo := newMockEntRepo()
	now := time.Now()
	entRepo.entitlements["u_42:CARRIER"] = &domain.Entitlement{
		UserID: "u_42", Source: domain.SourceCarrier,
		Active: true, Reason: "ACTIVE", LastChangedAt: now, CreatedAt: now,
	}

	carrier := &mockCarrierClient{status: "inactive"}
	pollRepo := newMockPollLogRepo()
	auditRepo := newMockAuditRepo()
	auditRepo.insertErr = fmt.Errorf("audit down")

	p := NewPoller(entRepo, pollRepo, carrier, auditRepo, testLogger())
	p.pollUser(context.Background(), "u_42")

	assert.Equal(t, 1, carrier.called)
	assert.Len(t, pollRepo.inserts, 1, "should still log poll result")
	assert.Equal(t, "inactive", pollRepo.inserts[0].status)
	assert.False(t, entRepo.entitlements["u_42:CARRIER"].Active, "should deactivate entitlement even if audit fails")
	assert.Equal(t, "CARRIER_INACTIVE", entRepo.entitlements["u_42:CARRIER"].Reason)
	assert.Len(t, auditRepo.entries, 0, "should not write audit entry when insert fails")
}

// Verify the Poller satisfies nothing extra - just a compile check
var _ = func() {
	var _ port.CarrierClient = (*mockCarrierClient)(nil)
}
