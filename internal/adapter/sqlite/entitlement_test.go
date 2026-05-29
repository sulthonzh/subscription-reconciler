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
		UserID:          userID,
		Source:          source,
		Active:          active,
		Reason:          "INITIAL_PURCHASE",
		LastChangedAt:   now,
		LastEventTimeMs: now.UnixMilli(),
		CreatedAt:       now,
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

func TestUpsert_TimestampsPreserved(t *testing.T) {
	t.Parallel()
	db := setupTestDB(t)
	repo := NewEntitlementRepo(db)
	ctx := context.Background()

	ent := makeEntitlement("u1", domain.SourceStore, true)
	require.NoError(t, repo.Upsert(ctx, ent))

	got, err := repo.GetByUserAndSource(ctx, "u1", domain.SourceStore)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.False(t, got.LastChangedAt.IsZero(), "LastChangedAt should not be zero after DB round-trip")
	assert.False(t, got.CreatedAt.IsZero(), "CreatedAt should not be zero after DB round-trip")
	assert.WithinDuration(t, ent.LastChangedAt, got.LastChangedAt, time.Second)
	assert.WithinDuration(t, ent.CreatedAt, got.CreatedAt, time.Second)
}

func TestGetByUser_TimestampsPreserved(t *testing.T) {
	t.Parallel()
	db := setupTestDB(t)
	repo := NewEntitlementRepo(db)
	ctx := context.Background()

	ent := makeEntitlement("u1", domain.SourceStore, true)
	require.NoError(t, repo.Upsert(ctx, ent))

	rows, err := repo.GetByUser(ctx, "u1")
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.False(t, rows[0].LastChangedAt.IsZero(), "LastChangedAt should not be zero in multi-row scan")
	assert.False(t, rows[0].CreatedAt.IsZero(), "CreatedAt should not be zero in multi-row scan")
}

func TestGetActiveBySource_TimestampsPreserved(t *testing.T) {
	t.Parallel()
	db := setupTestDB(t)
	repo := NewEntitlementRepo(db)
	ctx := context.Background()

	ent := makeEntitlement("u1", domain.SourceStore, true)
	require.NoError(t, repo.Upsert(ctx, ent))

	rows, err := repo.GetActiveBySource(ctx, domain.SourceStore)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.False(t, rows[0].LastChangedAt.IsZero(), "LastChangedAt should not be zero in GetActiveBySource")
	assert.False(t, rows[0].CreatedAt.IsZero(), "CreatedAt should not be zero in GetActiveBySource")
}

func TestGetExpiringBefore_ExpiringBeforeThreshold(t *testing.T) {
	t.Parallel()
	db := setupTestDB(t)
	repo := NewEntitlementRepo(db)
	ctx := context.Background()

	threshold := time.Now().UTC().Add(24 * time.Hour)
	
	earlyExpiry := threshold.Add(-2 * time.Hour)
	ent1 := makeEntitlementWithExpires("u1", domain.SourceStore, true, earlyExpiry)
	require.NoError(t, repo.Upsert(ctx, ent1))

	lateExpiry := threshold.Add(2 * time.Hour)
	ent2 := makeEntitlementWithExpires("u2", domain.SourceStore, true, lateExpiry)
	require.NoError(t, repo.Upsert(ctx, ent2))

	ent3 := makeEntitlementWithExpires("u3", domain.SourceStore, false, threshold.Add(-1 * time.Hour))
	require.NoError(t, repo.Upsert(ctx, ent3))

	ent4 := makeEntitlement("u4", domain.SourceCarrier, true)
	require.NoError(t, repo.Upsert(ctx, ent4))

	expiring, err := repo.GetExpiringBefore(ctx, threshold)
	require.NoError(t, err)
	require.Len(t, expiring, 1)
	assert.Equal(t, "u1", expiring[0].UserID)
	assert.True(t, expiring[0].Active)
	assert.WithinDuration(t, earlyExpiry, *expiring[0].ExpiresAt, time.Second)
}

func TestGetExpiringBefore_ExpiringAfterThreshold(t *testing.T) {
	t.Parallel()
	db := setupTestDB(t)
	repo := NewEntitlementRepo(db)
	ctx := context.Background()

	threshold := time.Now().UTC().Add(24 * time.Hour)
	
	futureExpiry := threshold.Add(2 * time.Hour)
	ent := makeEntitlementWithExpires("u1", domain.SourceStore, true, futureExpiry)
	require.NoError(t, repo.Upsert(ctx, ent))

	expiring, err := repo.GetExpiringBefore(ctx, threshold)
	require.NoError(t, err)
	assert.Empty(t, expiring)
}

func TestGetExpiringBefore_InactiveEntitlement(t *testing.T) {
	t.Parallel()
	db := setupTestDB(t)
	repo := NewEntitlementRepo(db)
	ctx := context.Background()

	threshold := time.Now().UTC().Add(24 * time.Hour)
	
	ent := makeEntitlementWithExpires("u1", domain.SourceStore, false, threshold.Add(-2 * time.Hour))
	require.NoError(t, repo.Upsert(ctx, ent))

	expiring, err := repo.GetExpiringBefore(ctx, threshold)
	require.NoError(t, err)
	assert.Empty(t, expiring)
}

func TestGetExpiringBefore_NoEntitlements(t *testing.T) {
	t.Parallel()
	db := setupTestDB(t)
	repo := NewEntitlementRepo(db)
	ctx := context.Background()

	threshold := time.Now().UTC().Add(24 * time.Hour)

	expiring, err := repo.GetExpiringBefore(ctx, threshold)
	require.NoError(t, err)
	assert.Empty(t, expiring)
}

func TestGetExpiringBefore_MultipleExpiring(t *testing.T) {
	t.Parallel()
	db := setupTestDB(t)
	repo := NewEntitlementRepo(db)
	ctx := context.Background()

	threshold := time.Now().UTC().Add(24 * time.Hour)
	
	ent1 := makeEntitlementWithExpires("u1", domain.SourceStore, true, threshold.Add(-2 * time.Hour))
	ent2 := makeEntitlementWithExpires("u2", domain.SourceMarketplace, true, threshold.Add(-1 * time.Hour))
	ent3 := makeEntitlementWithExpires("u3", domain.SourceCarrier, true, threshold.Add(-30 * time.Minute))
	
	require.NoError(t, repo.Upsert(ctx, ent1))
	require.NoError(t, repo.Upsert(ctx, ent2))
	require.NoError(t, repo.Upsert(ctx, ent3))

	expiring, err := repo.GetExpiringBefore(ctx, threshold)
	require.NoError(t, err)
	require.Len(t, expiring, 3)
	
	for _, ent := range expiring {
		assert.True(t, ent.Active)
		assert.NotNil(t, ent.ExpiresAt)
		assert.Less(t, ent.ExpiresAt.Unix(), threshold.Unix())
	}
}

func TestGetByUserAndSource_ClosedDB(t *testing.T) {
	t.Parallel()
	db := setupTestDB(t)
	repo := NewEntitlementRepo(db)
	ctx := context.Background()

	require.NoError(t, db.Close())

	_, err := repo.GetByUserAndSource(ctx, "u1", domain.SourceStore)
	require.Error(t, err)
}

func TestGetByUser_ClosedDB(t *testing.T) {
	t.Parallel()
	db := setupTestDB(t)
	repo := NewEntitlementRepo(db)
	ctx := context.Background()

	require.NoError(t, db.Close())

	_, err := repo.GetByUser(ctx, "u1")
	require.Error(t, err)
}

func TestGetActiveBySource_ClosedDB(t *testing.T) {
	t.Parallel()
	db := setupTestDB(t)
	repo := NewEntitlementRepo(db)
	ctx := context.Background()

	require.NoError(t, db.Close())

	_, err := repo.GetActiveBySource(ctx, domain.SourceStore)
	require.Error(t, err)
}

func TestExpireOverdue_ClosedDB(t *testing.T) {
	t.Parallel()
	db := setupTestDB(t)
	repo := NewEntitlementRepo(db)
	ctx := context.Background()

	require.NoError(t, db.Close())

	_, err := repo.ExpireOverdue(ctx, time.Now().UTC())
	require.Error(t, err)
}

func TestGetExpiringBefore_ClosedDB(t *testing.T) {
	t.Parallel()
	db := setupTestDB(t)
	repo := NewEntitlementRepo(db)
	ctx := context.Background()

	require.NoError(t, db.Close())

	_, err := repo.GetExpiringBefore(ctx, time.Now().UTC())
	require.Error(t, err)
}

func TestScanEntitlements_ScanError(t *testing.T) {
	t.Parallel()
	db := setupTestDB(t)
	repo := NewEntitlementRepo(db)
	ctx := context.Background()

	_, err := db.ExecContext(ctx, `INSERT INTO entitlements (user_id, source, active, expires_at, reason, last_changed_at, last_event_time_ms, created_at) VALUES ('u1', 'STORE', 1, NULL, 'TEST', '2026-01-01T00:00:00Z', 'notanumber', '2026-01-01T00:00:00Z')`)
	require.NoError(t, err)

	_, err = repo.GetByUser(ctx, "u1")
	require.Error(t, err)
}

func TestScanEntitlements_ScanError_GetActiveBySource(t *testing.T) {
	t.Parallel()
	db := setupTestDB(t)
	repo := NewEntitlementRepo(db)
	ctx := context.Background()

	_, err := db.ExecContext(ctx, `INSERT INTO entitlements (user_id, source, active, expires_at, reason, last_changed_at, last_event_time_ms, created_at) VALUES ('u1', 'STORE', 1, NULL, 'TEST', '2026-01-01T00:00:00Z', 'notanumber', '2026-01-01T00:00:00Z')`)
	require.NoError(t, err)

	_, err = repo.GetActiveBySource(ctx, domain.SourceStore)
	require.Error(t, err)
}

func TestScanEntitlements_ScanError_GetExpiringBefore(t *testing.T) {
	t.Parallel()
	db := setupTestDB(t)
	repo := NewEntitlementRepo(db)
	ctx := context.Background()

	_, err := db.ExecContext(ctx, `INSERT INTO entitlements (user_id, source, active, expires_at, reason, last_changed_at, last_event_time_ms, created_at) VALUES ('u1', 'STORE', 1, '2026-01-01T00:00:00Z', 'TEST', '2026-01-01T00:00:00Z', 'notanumber', '2026-01-01T00:00:00Z')`)
	require.NoError(t, err)

	_, err = repo.GetExpiringBefore(ctx, time.Now().UTC().Add(24*time.Hour))
	require.Error(t, err)
}

func TestUpsert_ClosedDB(t *testing.T) {
	t.Parallel()
	db := setupTestDB(t)
	repo := NewEntitlementRepo(db)
	ctx := context.Background()

	require.NoError(t, db.Close())

	err := repo.Upsert(ctx, makeEntitlement("u1", domain.SourceStore, true))
	require.Error(t, err)
}

func TestGetExpiringBefore_ExpiredEntitlement(t *testing.T) {
	t.Parallel()
	db := setupTestDB(t)
	repo := NewEntitlementRepo(db)
	ctx := context.Background()

	// Use a fixed time to avoid race conditions
	fixedTime := time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)
	
	pastExpiry := fixedTime.Add(-2 * time.Hour)
	ent := makeEntitlementWithExpires("u1", domain.SourceStore, true, pastExpiry)
	require.NoError(t, repo.Upsert(ctx, ent))

	expiring, err := repo.GetExpiringBefore(ctx, fixedTime)
	require.NoError(t, err)
	require.Len(t, expiring, 1)
	assert.Equal(t, "u1", expiring[0].UserID)
	assert.True(t, expiring[0].Active)
	assert.WithinDuration(t, pastExpiry, *expiring[0].ExpiresAt, time.Second)
}
