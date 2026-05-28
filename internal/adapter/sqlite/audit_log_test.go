package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/sulthonzh/subscription-reconciler/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAuditLogInsert(t *testing.T) {
	t.Parallel()
	db := setupTestDB(t)
	repo := NewAuditLogRepo(db)
	ctx := context.Background()

	entry := domain.AuditEntry{
		UserID:        "u1",
		TriggerID:     "evt_001",
		Source:        domain.SourceStore,
		PreviousState: `{"active":false}`,
		NextState:     `{"active":true}`,
		CreatedAt:     time.Now().UTC().Truncate(time.Microsecond),
	}

	require.NoError(t, repo.Insert(ctx, entry))

	entries, err := repo.GetByUser(ctx, "u1")
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, "u1", entries[0].UserID)
	assert.Equal(t, "evt_001", entries[0].TriggerID)
	assert.Equal(t, domain.SourceStore, entries[0].Source)
	assert.Equal(t, `{"active":false}`, entries[0].PreviousState)
	assert.Equal(t, `{"active":true}`, entries[0].NextState)
}

func TestAuditLogGetByUser_OrderedByCreatedAt(t *testing.T) {
	t.Parallel()
	db := setupTestDB(t)
	repo := NewAuditLogRepo(db)
	ctx := context.Background()

	base := time.Now().UTC().Truncate(time.Microsecond)

	e1 := domain.AuditEntry{
		UserID:        "u1",
		TriggerID:     "evt_001",
		Source:        domain.SourceStore,
		PreviousState: `{"active":false}`,
		NextState:     `{"active":true}`,
		CreatedAt:     base,
	}
	e2 := domain.AuditEntry{
		UserID:        "u1",
		TriggerID:     "evt_002",
		Source:        domain.SourceStore,
		PreviousState: `{"active":true}`,
		NextState:     `{"active":true}`,
		CreatedAt:     base.Add(1 * time.Hour),
	}

	require.NoError(t, repo.Insert(ctx, e1))
	require.NoError(t, repo.Insert(ctx, e2))

	entries, err := repo.GetByUser(ctx, "u1")
	require.NoError(t, err)
	require.Len(t, entries, 2)
	assert.Equal(t, "evt_001", entries[0].TriggerID)
	assert.Equal(t, "evt_002", entries[1].TriggerID)
}

func TestAuditLogGetByUser_Empty(t *testing.T) {
	t.Parallel()
	db := setupTestDB(t)
	repo := NewAuditLogRepo(db)
	ctx := context.Background()

	entries, err := repo.GetByUser(ctx, "nonexistent")
	require.NoError(t, err)
	assert.Empty(t, entries)
}

func TestAuditLogInsert_EmptyTriggerID(t *testing.T) {
	t.Parallel()
	db := setupTestDB(t)
	repo := NewAuditLogRepo(db)
	ctx := context.Background()

	entry := domain.AuditEntry{
		UserID:        "u1",
		TriggerID:     "",
		Source:        domain.SourceCarrier,
		PreviousState: `{"active":true}`,
		NextState:     `{"active":false}`,
		CreatedAt:     time.Now().UTC().Truncate(time.Microsecond),
	}

	require.NoError(t, repo.Insert(ctx, entry))

	entries, err := repo.GetByUser(ctx, "u1")
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, "", entries[0].TriggerID)
	assert.Equal(t, domain.SourceCarrier, entries[0].Source)
}
