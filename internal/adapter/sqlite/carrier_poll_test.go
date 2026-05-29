package sqlite

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCarrierPollInsert(t *testing.T) {
	t.Parallel()
	db := setupTestDB(t)
	repo := NewCarrierPollLogRepo(db)
	ctx := context.Background()

	require.NoError(t, repo.Insert(ctx, "u1", "active"))

	var status string
	err := db.QueryRowContext(ctx,
		"SELECT status FROM carrier_poll_log WHERE user_id = ? ORDER BY id DESC LIMIT 1",
		"u1",
	).Scan(&status)
	require.NoError(t, err)
	assert.Equal(t, "active", status)
}

func TestAcquireLock_NoExistingLock(t *testing.T) {
	t.Parallel()
	db := setupTestDB(t)
	repo := NewCarrierPollLogRepo(db)
	ctx := context.Background()

	lockedUntil := time.Now().UTC().Add(2 * time.Minute)
	acquired, err := repo.AcquireLock(ctx, "u1", lockedUntil)
	require.NoError(t, err)
	assert.True(t, acquired)
}

func TestAcquireLock_ExistingActiveLock(t *testing.T) {
	t.Parallel()
	db := setupTestDB(t)
	repo := NewCarrierPollLogRepo(db)
	ctx := context.Background()

	lockedUntil := time.Now().UTC().Add(2 * time.Minute)
	acquired, err := repo.AcquireLock(ctx, "u1", lockedUntil)
	require.NoError(t, err)
	require.True(t, acquired)

	acquired, err = repo.AcquireLock(ctx, "u1", lockedUntil)
	require.NoError(t, err)
	assert.False(t, acquired)
}

func TestAcquireLock_ExpiredLock(t *testing.T) {
	t.Parallel()
	db := setupTestDB(t)
	ctx := context.Background()

	// Insert an expired lock directly
	_, err := db.ExecContext(ctx,
		"INSERT INTO carrier_poll_log (user_id, status, polled_at, locked_until) VALUES (?, 'LOCKED', ?, ?)",
		"u1",
		time.Now().UTC().Add(-5*time.Minute).Format(time.RFC3339Nano),
		time.Now().UTC().Add(-1*time.Minute).Format(time.RFC3339Nano),
	)
	require.NoError(t, err)

	repo := NewCarrierPollLogRepo(db)
	lockedUntil := time.Now().UTC().Add(2 * time.Minute)
	acquired, err := repo.AcquireLock(ctx, "u1", lockedUntil)
	require.NoError(t, err)
	assert.True(t, acquired)
}

func TestReleaseLock(t *testing.T) {
	t.Parallel()
	db := setupTestDB(t)
	repo := NewCarrierPollLogRepo(db)
	ctx := context.Background()

	lockedUntil := time.Now().UTC().Add(2 * time.Minute)
	acquired, err := repo.AcquireLock(ctx, "u1", lockedUntil)
	require.NoError(t, err)
	require.True(t, acquired)

	require.NoError(t, repo.ReleaseLock(ctx, "u1"))

	var lockedUntilVal sql.NullString
	err = db.QueryRowContext(ctx,
		"SELECT locked_until FROM carrier_poll_log WHERE user_id = ? ORDER BY id DESC LIMIT 1",
		"u1",
	).Scan(&lockedUntilVal)
	require.NoError(t, err)
	assert.False(t, lockedUntilVal.Valid)
}

func TestAcquireLock_QueryError(t *testing.T) {
	t.Parallel()
	db := setupTestDB(t)
	repo := NewCarrierPollLogRepo(db)
	ctx := context.Background()

	require.NoError(t, db.Close())

	_, err := repo.AcquireLock(ctx, "u1", time.Now().UTC().Add(2*time.Minute))
	require.Error(t, err)
}

func TestAcquireLock_InsertError(t *testing.T) {
	t.Parallel()
	db := setupTestDB(t)
	repo := NewCarrierPollLogRepo(db)
	ctx := context.Background()

	_, err := db.ExecContext(ctx, "PRAGMA query_only = true")
	require.NoError(t, err)

	_, err = repo.AcquireLock(ctx, "u1", time.Now().UTC().Add(2*time.Minute))
	require.Error(t, err)
}

func TestAcquireLock_DifferentUsers(t *testing.T) {
	t.Parallel()
	db := setupTestDB(t)
	repo := NewCarrierPollLogRepo(db)
	ctx := context.Background()

	lockedUntil := time.Now().UTC().Add(2 * time.Minute)

	acquired, err := repo.AcquireLock(ctx, "u1", lockedUntil)
	require.NoError(t, err)
	assert.True(t, acquired)

	acquired, err = repo.AcquireLock(ctx, "u2", lockedUntil)
	require.NoError(t, err)
	assert.True(t, acquired)
}
