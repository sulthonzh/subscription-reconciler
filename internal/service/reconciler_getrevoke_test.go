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

func TestRevokeMarketplace_OnlyMarketplace(t *testing.T) {
	entRepo := newMockEntRepo()
	now := time.Now()
	entRepo.entitlements["u_42:MARKETPLACE"] = &domain.Entitlement{
		UserID: "u_42", Source: domain.SourceMarketplace,
		Active: true, Reason: "GRANTED", LastChangedAt: now, CreatedAt: now,
	}
	entRepo.entitlements["u_42:STORE"] = &domain.Entitlement{
		UserID: "u_42", Source: domain.SourceStore,
		Active: true, Reason: "RENEWAL", LastChangedAt: now, CreatedAt: now,
	}

	r := NewReconciler(entRepo, newMockEventRepo(), newMockNotifRepo(), newMockAuditRepo(), testLogger())

	revoked, skipped, err := r.RevokeMarketplace(context.Background(), []string{"u_42"})
	require.NoError(t, err)
	assert.Equal(t, 1, revoked)
	assert.Equal(t, 0, skipped)

	assert.Len(t, entRepo.updated, 1)
	assert.Equal(t, "u_42", entRepo.updated[0].userID)
	assert.Equal(t, domain.SourceMarketplace, entRepo.updated[0].source)
	assert.False(t, entRepo.updated[0].active)

	assert.False(t, entRepo.entitlements["u_42:MARKETPLACE"].Active)
	assert.True(t, entRepo.entitlements["u_42:STORE"].Active)
}

func TestRevokeMarketplace_PartialMatch(t *testing.T) {
	entRepo := newMockEntRepo()
	now := time.Now()
	entRepo.entitlements["u_42:MARKETPLACE"] = &domain.Entitlement{
		UserID: "u_42", Source: domain.SourceMarketplace,
		Active: true, Reason: "GRANTED", LastChangedAt: now, CreatedAt: now,
	}

	r := NewReconciler(entRepo, newMockEventRepo(), newMockNotifRepo(), nil, testLogger())

	revoked, skipped, err := r.RevokeMarketplace(context.Background(), []string{"u_42", "u_99"})
	require.NoError(t, err)
	assert.Equal(t, 1, revoked)
	assert.Equal(t, 1, skipped)
}

func TestRevokeMarketplace_AlreadyInactive(t *testing.T) {
	entRepo := newMockEntRepo()
	now := time.Now()
	entRepo.entitlements["u_42:MARKETPLACE"] = &domain.Entitlement{
		UserID: "u_42", Source: domain.SourceMarketplace,
		Active: false, Reason: "EXPIRED", LastChangedAt: now, CreatedAt: now,
	}

	r := NewReconciler(entRepo, newMockEventRepo(), newMockNotifRepo(), nil, testLogger())

	revoked, skipped, err := r.RevokeMarketplace(context.Background(), []string{"u_42"})
	require.NoError(t, err)
	assert.Equal(t, 0, revoked)
	assert.Equal(t, 1, skipped)
}

func TestGetEntitlement_MultipleSources(t *testing.T) {
	entRepo := newMockEntRepo()
	now := time.Now()
	entRepo.entitlements["u_42:CARRIER"] = &domain.Entitlement{
		UserID: "u_42", Source: domain.SourceCarrier,
		Active: true, Reason: "ACTIVE", LastChangedAt: now, CreatedAt: now,
	}
	entRepo.entitlements["u_42:STORE"] = &domain.Entitlement{
		UserID: "u_42", Source: domain.SourceStore,
		Active: true, Reason: "RENEWAL", LastChangedAt: now, CreatedAt: now,
	}

	r := NewReconciler(entRepo, newMockEventRepo(), newMockNotifRepo(), nil, testLogger())

	ent, err := r.GetEntitlement(context.Background(), "u_42")
	require.NoError(t, err)
	assert.True(t, ent.Active)
	assert.Equal(t, domain.SourceStore, ent.Source, "STORE should win over CARRIER")
}

func TestGetEntitlement_NotFound(t *testing.T) {
	entRepo := newMockEntRepo()
	r := NewReconciler(entRepo, newMockEventRepo(), newMockNotifRepo(), nil, testLogger())

	ent, err := r.GetEntitlement(context.Background(), "u_unknown")
	require.NoError(t, err)
	assert.False(t, ent.Active)
	assert.Equal(t, domain.SourceNone, ent.Source)
}

func TestGetEntitlement_NoneActive(t *testing.T) {
	entRepo := newMockEntRepo()
	now := time.Now()
	entRepo.entitlements["u_42:STORE"] = &domain.Entitlement{
		UserID: "u_42", Source: domain.SourceStore,
		Active: false, Reason: "EXPIRATION", LastChangedAt: now, CreatedAt: now,
	}

	r := NewReconciler(entRepo, newMockEventRepo(), newMockNotifRepo(), nil, testLogger())

	ent, err := r.GetEntitlement(context.Background(), "u_42")
	require.NoError(t, err)
	assert.False(t, ent.Active)
	assert.Equal(t, domain.SourceNone, ent.Source)
}

func TestExpireOverdue(t *testing.T) {
	entRepo := newMockEntRepo()
	now := time.Now()
	pastExpiry := now.Add(-1 * time.Hour)
	entRepo.entitlements["u_42:STORE"] = &domain.Entitlement{
		UserID: "u_42", Source: domain.SourceStore,
		Active: true, ExpiresAt: &pastExpiry, LastChangedAt: now, CreatedAt: now,
	}
	entRepo.entitlements["u_43:STORE"] = &domain.Entitlement{
		UserID: "u_43", Source: domain.SourceStore,
		Active: true, ExpiresAt: nil, LastChangedAt: now, CreatedAt: now,
	}

	r := NewReconciler(entRepo, newMockEventRepo(), newMockNotifRepo(), nil, testLogger())

	count, err := r.ExpireOverdue(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, count)
	assert.False(t, entRepo.entitlements["u_42:STORE"].Active)
	assert.True(t, entRepo.entitlements["u_43:STORE"].Active)
}

func TestRevokeMarketplace_GetError(t *testing.T) {
	entRepo := newMockEntRepo()
	entRepo.getByUserAndSourceErr = fmt.Errorf("db down")

	r := NewReconciler(entRepo, newMockEventRepo(), newMockNotifRepo(), nil, testLogger())

	_, _, err := r.RevokeMarketplace(context.Background(), []string{"u_42"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "get entitlement for u_42")
}

func TestRevokeMarketplace_UpdateActiveError(t *testing.T) {
	entRepo := newMockEntRepo()
	now := time.Now()
	entRepo.entitlements["u_42:MARKETPLACE"] = &domain.Entitlement{
		UserID: "u_42", Source: domain.SourceMarketplace,
		Active: true, Reason: "GRANTED", LastChangedAt: now, CreatedAt: now,
	}
	entRepo.updateActiveErr = fmt.Errorf("db down")

	r := NewReconciler(entRepo, newMockEventRepo(), newMockNotifRepo(), nil, testLogger())

	_, _, err := r.RevokeMarketplace(context.Background(), []string{"u_42"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "revoke marketplace for u_42")
}

func TestRevokeMarketplace_AuditInsertError(t *testing.T) {
	entRepo := newMockEntRepo()
	now := time.Now()
	entRepo.entitlements["u_42:MARKETPLACE"] = &domain.Entitlement{
		UserID: "u_42", Source: domain.SourceMarketplace,
		Active: true, Reason: "GRANTED", LastChangedAt: now, CreatedAt: now,
	}
	auditRepo := newMockAuditRepo()
	auditRepo.insertErr = fmt.Errorf("audit down")

	r := NewReconciler(entRepo, newMockEventRepo(), newMockNotifRepo(), auditRepo, testLogger())

	revoked, skipped, err := r.RevokeMarketplace(context.Background(), []string{"u_42"})
	require.NoError(t, err, "audit error should not fail revoke")
	assert.Equal(t, 1, revoked)
	assert.Equal(t, 0, skipped)
}

func TestGetEntitlement_GetByUserError(t *testing.T) {
	entRepo := newMockEntRepo()
	entRepo.getByUserErr = fmt.Errorf("db down")

	r := NewReconciler(entRepo, newMockEventRepo(), newMockNotifRepo(), nil, testLogger())

	_, err := r.GetEntitlement(context.Background(), "u_42")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "get entitlements")
}

func TestExpireOverdue_Error(t *testing.T) {
	entRepo := newMockEntRepo()
	entRepo.expireOverdueErr = fmt.Errorf("db down")

	r := NewReconciler(entRepo, newMockEventRepo(), newMockNotifRepo(), nil, testLogger())

	_, err := r.ExpireOverdue(context.Background())
	require.Error(t, err)
}
