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

func TestExpireOverdue_SingleFile(t *testing.T) {
	entRepo := newMockEntRepo()
	
	// Add some overdue entitlements
	now := time.Now()
	past := now.Add(-2 * time.Hour)
	future := now.Add(2 * time.Hour)
	
	entRepo.entitlements["u_1:STORE"] = &domain.Entitlement{
		UserID:          "u_1",
		Source:          domain.SourceStore,
		Active:          true,
		ExpiresAt:       &past,
		Reason:          "INITIAL_PURCHASE",
		LastChangedAt:   now.Add(-1 * time.Hour),
		LastEventTimeMs: now.Add(-1 * time.Hour).UnixMilli(),
		CreatedAt:       now.Add(-24 * time.Hour),
	}
	
	entRepo.entitlements["u_2:STORE"] = &domain.Entitlement{
		UserID:          "u_2",
		Source:          domain.SourceStore,
		Active:          true,
		ExpiresAt:       &past,
		Reason:          "RENEWAL",
		LastChangedAt:   now.Add(-2 * time.Hour),
		LastEventTimeMs: now.Add(-2 * time.Hour).UnixMilli(),
		CreatedAt:       now.Add(-48 * time.Hour),
	}
	
	entRepo.entitlements["u_3:STORE"] = &domain.Entitlement{
		UserID:          "u_3",
		Source:          domain.SourceStore,
		Active:          true,
		ExpiresAt:       &past,
		Reason:          "CANCELLATION",
		LastChangedAt:   now.Add(-3 * time.Hour),
		LastEventTimeMs: now.Add(-3 * time.Hour).UnixMilli(),
		CreatedAt:       now.Add(-72 * time.Hour),
	}
	
	// Add a non-overdue entitlement that should not be expired
	entRepo.entitlements["u_4:STORE"] = &domain.Entitlement{
		UserID:          "u_4",
		Source:          domain.SourceStore,
		Active:          true,
		ExpiresAt:       &future,
		Reason:          "RENEWAL",
		LastChangedAt:   now.Add(-1 * time.Hour),
		LastEventTimeMs: now.Add(-1 * time.Hour).UnixMilli(),
		CreatedAt:       now.Add(-24 * time.Hour),
	}
	
	r := NewReconciler(entRepo, newMockEventRepo(), newMockNotifRepo(), newMockAuditRepo(), mockTxProvider{}, testLogger())
	
	count, err := r.ExpireOverdue(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 3, count, "Should expire 3 overdue entitlements")
	
	// Verify the expired entitlements are marked as inactive
	assert.Equal(t, "EXPIRED", entRepo.entitlements["u_1:STORE"].Reason)
	assert.Equal(t, "EXPIRED", entRepo.entitlements["u_2:STORE"].Reason)
	assert.Equal(t, "EXPIRED", entRepo.entitlements["u_3:STORE"].Reason)
	assert.False(t, entRepo.entitlements["u_1:STORE"].Active)
	assert.False(t, entRepo.entitlements["u_2:STORE"].Active)
	assert.False(t, entRepo.entitlements["u_3:STORE"].Active)
	
	// Verify the non-overdue entitlement remains unchanged
	assert.True(t, entRepo.entitlements["u_4:STORE"].Active)
	assert.Equal(t, "RENEWAL", entRepo.entitlements["u_4:STORE"].Reason)
}

func TestExpireOverdue_SingleFile_Error(t *testing.T) {
	entRepo := newMockEntRepo()
	entRepo.expireOverdueErr = fmt.Errorf("database error")
	
	r := NewReconciler(entRepo, newMockEventRepo(), newMockNotifRepo(), newMockAuditRepo(), mockTxProvider{}, testLogger())
	
	count, err := r.ExpireOverdue(context.Background())
	require.Error(t, err)
	assert.Equal(t, 0, count, "Should return 0 count on error")
}