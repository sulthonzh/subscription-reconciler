package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/sulthonzh/subscription-reconciler/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewTxProvider(t *testing.T) {
	t.Parallel()
	
	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	defer db.Close()
	
	txProvider := NewTxProvider(db)
	require.NotNil(t, txProvider)
	assert.Equal(t, db, txProvider.db)
}

func TestWithinTx_Success(t *testing.T) {
	t.Parallel()
	
	db := setupTestDB(t)
	txProvider := NewTxProvider(db)
	ctx := context.Background()
	
	// Insert data within transaction
	err := txProvider.WithinTx(ctx, func(ctx context.Context) error {
		repo := NewEntitlementRepo(db)
		
		ent := makeEntitlement("u1", domain.SourceStore, true)
		ent.ExpiresAt = pointer(time.Now().UTC().Add(24 * time.Hour))
		return repo.Upsert(ctx, ent)
	})
	
	require.NoError(t, err)
	
	// Verify data is committed and visible outside transaction
	repo := NewEntitlementRepo(db)
	got, err := repo.GetByUserAndSource(ctx, "u1", domain.SourceStore)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.True(t, got.Active)
	assert.Equal(t, "INITIAL_PURCHASE", got.Reason)
	if got.ExpiresAt != nil {
		assert.WithinDuration(t, time.Now().UTC().Add(24*time.Hour), *got.ExpiresAt, time.Second)
	}
}

func TestWithinTx_RollbackOnError(t *testing.T) {
	t.Parallel()
	
	db := setupTestDB(t)
	txProvider := NewTxProvider(db)
	ctx := context.Background()
	
	// Insert data then return error (should cause rollback)
	err := txProvider.WithinTx(ctx, func(ctx context.Context) error {
		repo := NewEntitlementRepo(db)
		
		ent := makeEntitlement("u1", domain.SourceStore, true)
		if err := repo.Upsert(ctx, ent); err != nil {
			return err
		}
		
		// Return an error to trigger rollback
		return fmt.Errorf("simulated error")
	})
	
	// Should return the error
	require.Error(t, err)
	
	// Verify data was rolled back (not visible outside transaction)
	repo := NewEntitlementRepo(db)
	got, err := repo.GetByUserAndSource(ctx, "u1", domain.SourceStore)
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestWithinTx_TransactionError(t *testing.T) {
	t.Parallel()
	
	db := setupTestDB(t)
	txProvider := NewTxProvider(db)
	ctx := context.Background()
	
	// Test begin transaction error by closing the DB before starting transaction
	err := db.Close()
	require.NoError(t, err)
	
	err = txProvider.WithinTx(ctx, func(ctx context.Context) error {
		return nil
	})
	
	require.Error(t, err)
	assert.Contains(t, err.Error(), "begin tx")
}

func TestGetDB_Fallback(t *testing.T) {
	t.Parallel()
	
	db := setupTestDB(t)
	ctx := context.Background()
	
	// Context without transaction should return fallback DB
	result := getDB(ctx, db)
	assert.Equal(t, db, result)
}

func TestGetDB_FromContext(t *testing.T) {
	t.Parallel()
	
	db := setupTestDB(t)
	ctx := context.Background()
	
	// Create a transaction and put it in context
	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer tx.Rollback()
	
	txCtx := context.WithValue(ctx, txKey{}, tx)
	
	// Context with transaction should return the transaction
	result := getDB(txCtx, db)
	assert.Equal(t, tx, result)
}

func TestGetDB_NilTransaction(t *testing.T) {
	t.Parallel()
	
	db := setupTestDB(t)
	ctx := context.Background()
	
	// Context with nil transaction should return fallback DB
	txCtx := context.WithValue(ctx, txKey{}, nil)
	
	result := getDB(txCtx, db)
	assert.Equal(t, db, result)
}

func TestGetDB_WrongType(t *testing.T) {
	t.Parallel()
	
	db := setupTestDB(t)
	ctx := context.Background()
	
	// Context with wrong type should return fallback DB
	txCtx := context.WithValue(ctx, txKey{}, "not-a-tx")
	
	result := getDB(txCtx, db)
	assert.Equal(t, db, result)
}

// Helper function to create a pointer to time.Time
func pointer(t time.Time) *time.Time {
	return &t
}

// Helper function to create entitlement with expires_at
func makeEntitlementWithExpires(userID string, source domain.Source, active bool, expiresAt time.Time) domain.Entitlement {
	now := time.Now().UTC().Truncate(time.Microsecond)
	return domain.Entitlement{
		UserID:          userID,
		Source:          source,
		Active:          active,
		ExpiresAt:       &expiresAt,
		Reason:          "INITIAL_PURCHASE",
		LastChangedAt:   now,
		LastEventTimeMs: now.UnixMilli(),
		CreatedAt:       now,
	}
}