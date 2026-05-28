package sqlite

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"github.com/sulthonzh/subscription-reconciler/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	migrationSQL, err := os.ReadFile("../../../migrations/001_create_tables.up.sql")
	require.NoError(t, err)
	_, err = db.Exec(string(migrationSQL))
	require.NoError(t, err)

	return db
}

func makeEntitlement(userID string, source domain.Source, active bool) domain.Entitlement {
	now := time.Now().UTC().Truncate(time.Microsecond)
	return domain.Entitlement{
		UserID:        userID,
		Source:        source,
		Active:        active,
		Reason:        "INITIAL_PURCHASE",
		LastChangedAt: now,
		CreatedAt:     now,
	}
}

func TestUpsert_NewEntitlement(t *testing.T) {
	t.Parallel()
	db := setupTestDB(t)
	repo := NewEntitlementRepo(db)
	ctx := context.Background()

	ent := makeEntitlement("u1", domain.SourceStore, true)
	require.NoError(t, repo.Upsert(ctx, ent))

	got, err := repo.GetByUserAndSource(ctx, "u1", domain.SourceStore)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "u1", got.UserID)
	assert.Equal(t, domain.SourceStore, got.Source)
	assert.True(t, got.Active)
}

func TestUpsert_UpdateExisting(t *testing.T) {
	t.Parallel()
	db := setupTestDB(t)
	repo := NewEntitlementRepo(db)
	ctx := context.Background()

	ent := makeEntitlement("u1", domain.SourceStore, true)
	require.NoError(t, repo.Upsert(ctx, ent))

	ent.Active = false
	ent.Reason = "EXPIRATION"
	require.NoError(t, repo.Upsert(ctx, ent))

	got, err := repo.GetByUserAndSource(ctx, "u1", domain.SourceStore)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.False(t, got.Active)
	assert.Equal(t, "EXPIRATION", got.Reason)
}

func TestGetByUser_MultipleSources(t *testing.T) {
	t.Parallel()
	db := setupTestDB(t)
	repo := NewEntitlementRepo(db)
	ctx := context.Background()

	require.NoError(t, repo.Upsert(ctx, makeEntitlement("u1", domain.SourceStore, true)))
	require.NoError(t, repo.Upsert(ctx, makeEntitlement("u1", domain.SourceCarrier, true)))

	rows, err := repo.GetByUser(ctx, "u1")
	require.NoError(t, err)
	assert.Len(t, rows, 2)
}

func TestGetByUser_NotFound(t *testing.T) {
	t.Parallel()
	db := setupTestDB(t)
	repo := NewEntitlementRepo(db)
	ctx := context.Background()

	rows, err := repo.GetByUser(ctx, "nonexistent")
	require.NoError(t, err)
	assert.Empty(t, rows)
}

func TestGetByUserAndSource_Found(t *testing.T) {
	t.Parallel()
	db := setupTestDB(t)
	repo := NewEntitlementRepo(db)
	ctx := context.Background()

	require.NoError(t, repo.Upsert(ctx, makeEntitlement("u1", domain.SourceStore, true)))

	got, err := repo.GetByUserAndSource(ctx, "u1", domain.SourceStore)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "u1", got.UserID)
}

func TestGetByUserAndSource_NotFound(t *testing.T) {
	t.Parallel()
	db := setupTestDB(t)
	repo := NewEntitlementRepo(db)
	ctx := context.Background()

	got, err := repo.GetByUserAndSource(ctx, "u1", domain.SourceStore)
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestGetActiveBySource(t *testing.T) {
	t.Parallel()
	db := setupTestDB(t)
	repo := NewEntitlementRepo(db)
	ctx := context.Background()

	require.NoError(t, repo.Upsert(ctx, makeEntitlement("u1", domain.SourceStore, true)))
	require.NoError(t, repo.Upsert(ctx, makeEntitlement("u2", domain.SourceStore, false)))
	require.NoError(t, repo.Upsert(ctx, makeEntitlement("u3", domain.SourceCarrier, true)))

	rows, err := repo.GetActiveBySource(ctx, domain.SourceStore)
	require.NoError(t, err)
	assert.Len(t, rows, 1)
	assert.Equal(t, "u1", rows[0].UserID)
}

func TestUpdateActive(t *testing.T) {
	t.Parallel()
	db := setupTestDB(t)
	repo := NewEntitlementRepo(db)
	ctx := context.Background()

	require.NoError(t, repo.Upsert(ctx, makeEntitlement("u1", domain.SourceStore, true)))

	require.NoError(t, repo.UpdateActive(ctx, "u1", domain.SourceStore, false, "CANCELLATION"))

	got, err := repo.GetByUserAndSource(ctx, "u1", domain.SourceStore)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.False(t, got.Active)
	assert.Equal(t, "CANCELLATION", got.Reason)
}

func TestExpireOverdue(t *testing.T) {
	t.Parallel()
	db := setupTestDB(t)
	repo := NewEntitlementRepo(db)
	ctx := context.Background()

	past := time.Now().UTC().Add(-24 * time.Hour)
	ent1 := makeEntitlement("u1", domain.SourceStore, true)
	ent1.ExpiresAt = &past
	require.NoError(t, repo.Upsert(ctx, ent1))

	future := time.Now().UTC().Add(24 * time.Hour)
	ent2 := makeEntitlement("u2", domain.SourceStore, true)
	ent2.ExpiresAt = &future
	require.NoError(t, repo.Upsert(ctx, ent2))

	count, err := repo.ExpireOverdue(ctx, time.Now().UTC())
	require.NoError(t, err)
	assert.Equal(t, 1, count)

	got1, _ := repo.GetByUserAndSource(ctx, "u1", domain.SourceStore)
	assert.False(t, got1.Active)
	assert.Equal(t, "EXPIRED", got1.Reason)

	got2, _ := repo.GetByUserAndSource(ctx, "u2", domain.SourceStore)
	assert.True(t, got2.Active)
}

func TestUpsert_WithExpiresAt(t *testing.T) {
	t.Parallel()
	db := setupTestDB(t)
	repo := NewEntitlementRepo(db)
	ctx := context.Background()

	expires := time.Now().UTC().Add(30*24*time.Hour).Truncate(time.Microsecond)
	ent := makeEntitlement("u1", domain.SourceStore, true)
	ent.ExpiresAt = &expires
	require.NoError(t, repo.Upsert(ctx, ent))

	got, err := repo.GetByUserAndSource(ctx, "u1", domain.SourceStore)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.NotNil(t, got.ExpiresAt)
	assert.WithinDuration(t, expires, *got.ExpiresAt, time.Second)
}

func TestUpsert_NullExpiresAt(t *testing.T) {
	t.Parallel()
	db := setupTestDB(t)
	repo := NewEntitlementRepo(db)
	ctx := context.Background()

	ent := makeEntitlement("u1", domain.SourceCarrier, true)
	require.NoError(t, repo.Upsert(ctx, ent))

	got, err := repo.GetByUserAndSource(ctx, "u1", domain.SourceCarrier)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Nil(t, got.ExpiresAt)
}

func TestGetActiveBySource_Empty(t *testing.T) {
	t.Parallel()
	db := setupTestDB(t)
	repo := NewEntitlementRepo(db)
	ctx := context.Background()

	rows, err := repo.GetActiveBySource(ctx, domain.SourceMarketplace)
	require.NoError(t, err)
	assert.Empty(t, rows)
}

func TestUpdateActive_Nonexistent(t *testing.T) {
	t.Parallel()
	db := setupTestDB(t)
	repo := NewEntitlementRepo(db)
	ctx := context.Background()

	err := repo.UpdateActive(ctx, "nonexistent", domain.SourceStore, false, "TEST")
	require.NoError(t, err)
}

func TestExpireOverdue_NoExpired(t *testing.T) {
	t.Parallel()
	db := setupTestDB(t)
	repo := NewEntitlementRepo(db)
	ctx := context.Background()

	future := time.Now().UTC().Add(24 * time.Hour)
	ent := makeEntitlement("u1", domain.SourceStore, true)
	ent.ExpiresAt = &future
	require.NoError(t, repo.Upsert(ctx, ent))

	count, err := repo.ExpireOverdue(ctx, time.Now().UTC())
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}
