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

	p := NewPoller(entRepo, pollRepo, carrier, nil, testLogger())
	p.PollAll(context.Background())

	assert.Equal(t, 1, carrier.called)
	assert.False(t, entRepo.entitlements["u_42:CARRIER"].Active,
		"inactive response should deactivate")
	assert.Equal(t, "CARRIER_INACTIVE", entRepo.entitlements["u_42:CARRIER"].Reason)
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

// Verify the Poller satisfies nothing extra - just a compile check
var _ = func() {
	var _ port.CarrierClient = (*mockCarrierClient)(nil)
}
