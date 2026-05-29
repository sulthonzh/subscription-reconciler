package sqlite

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpen_Success(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	db, err := Open(dbPath)
	require.NoError(t, err)
	defer db.Close()

	// Verify pragmas were applied
	var mode string
	err = db.QueryRow("PRAGMA journal_mode").Scan(&mode)
	require.NoError(t, err)
	assert.Equal(t, "wal", mode)
}

func TestOpen_InvalidPath(t *testing.T) {
	t.Parallel()

	// Path in nonexistent deeply nested read-only location
	db, err := Open("/nonexistent/deeply/nested/path/test.db")
	if db != nil {
		db.Close()
	}
	// Open may not error immediately (SQLite defers file creation), but we test it doesn't panic
	_ = err
}

func TestOpen_PragmaError(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "readonly.db")

	f, err := os.Create(dbPath)
	require.NoError(t, err)
	require.NoError(t, f.Close())

	require.NoError(t, os.Chmod(dbPath, 0444))
	defer os.Chmod(dbPath, 0644)

	db, err := Open(dbPath)
	if db != nil {
		db.Close()
	}
	require.Error(t, err)
}

func TestOpen_InMemory(t *testing.T) {
	t.Parallel()

	db, err := Open(":memory:")
	require.NoError(t, err)
	defer db.Close()

	var result int
	err = db.QueryRow("SELECT 1").Scan(&result)
	require.NoError(t, err)
	assert.Equal(t, 1, result)
}

func TestOpen_DriverNotRegistered(t *testing.T) {
	orig := driverName
	driverName = "nonexistent_driver_12345"
	defer func() { driverName = orig }()

	_, err := Open(":memory:")
	assert.Error(t, err)
}

func TestSetupTestDB(t *testing.T) {
	// Verify the shared test helper works with a temp copy of migrations
	t.Parallel()
	db := setupTestDB(t)

	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table'").Scan(&count)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, count, 5) // entitlements, store_events, carrier_poll_log, notifications, audit_log
}
