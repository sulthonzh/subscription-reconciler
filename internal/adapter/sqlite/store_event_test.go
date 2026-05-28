package sqlite

import (
	"context"
	"testing"

	"github.com/sulthonzh/subscription-reconciler/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStoreEventInsert_New(t *testing.T) {
	t.Parallel()
	db := setupTestDB(t)
	repo := NewStoreEventRepo(db)
	ctx := context.Background()

	event := domain.StoreEvent{
		EventID:     "evt_001",
		UserID:      "u1",
		Type:        domain.EventInitialPurchase,
		EventTimeMs: 1716700000000,
		ProductID:   "premium_monthly",
	}

	inserted, err := repo.Insert(ctx, event)
	require.NoError(t, err)
	assert.True(t, inserted)
}

func TestStoreEventInsert_Duplicate(t *testing.T) {
	t.Parallel()
	db := setupTestDB(t)
	repo := NewStoreEventRepo(db)
	ctx := context.Background()

	event := domain.StoreEvent{
		EventID:     "evt_001",
		UserID:      "u1",
		Type:        domain.EventInitialPurchase,
		EventTimeMs: 1716700000000,
		ProductID:   "premium_monthly",
	}

	inserted, err := repo.Insert(ctx, event)
	require.NoError(t, err)
	assert.True(t, inserted)

	inserted, err = repo.Insert(ctx, event)
	require.NoError(t, err)
	assert.False(t, inserted)
}

func TestStoreEventInsert_DifferentUsers(t *testing.T) {
	t.Parallel()
	db := setupTestDB(t)
	repo := NewStoreEventRepo(db)
	ctx := context.Background()

	e1 := domain.StoreEvent{
		EventID:     "evt_001",
		UserID:      "u1",
		Type:        domain.EventInitialPurchase,
		EventTimeMs: 1716700000000,
		ProductID:   "premium_monthly",
	}
	e2 := domain.StoreEvent{
		EventID:     "evt_002",
		UserID:      "u2",
		Type:        domain.EventRenewal,
		EventTimeMs: 1716700000000,
		ProductID:   "premium_yearly",
	}

	inserted, err := repo.Insert(ctx, e1)
	require.NoError(t, err)
	assert.True(t, inserted)

	inserted, err = repo.Insert(ctx, e2)
	require.NoError(t, err)
	assert.True(t, inserted)
}
