package sqlite

import (
	"database/sql"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestScanTime_NullString(t *testing.T) {
	t.Parallel()

	result := scanTime(sql.NullString{Valid: false})
	assert.Nil(t, result)
}

func TestScanTime_EmptyString(t *testing.T) {
	t.Parallel()

	result := scanTime(sql.NullString{Valid: true, String: ""})
	assert.Nil(t, result)
}

func TestScanTime_ValidRFC3339(t *testing.T) {
	t.Parallel()

	ts := time.Date(2026, 5, 28, 12, 0, 0, 0, time.UTC)
	result := scanTime(sql.NullString{Valid: true, String: ts.Format(time.RFC3339Nano)})
	require.NotNil(t, result)
	assert.WithinDuration(t, ts, *result, time.Second)
}

func TestScanTime_InvalidFormat(t *testing.T) {
	t.Parallel()

	result := scanTime(sql.NullString{Valid: true, String: "not-a-date"})
	assert.Nil(t, result)
}

func TestFormatTime(t *testing.T) {
	t.Parallel()

	ts := time.Date(2026, 5, 28, 12, 30, 0, 0, time.UTC)
	formatted := formatTime(ts)
	parsed, err := time.Parse(time.RFC3339Nano, formatted)
	assert.NoError(t, err)
	assert.WithinDuration(t, ts, parsed, time.Microsecond)
}

func TestFormatTimeNow(t *testing.T) {
	t.Parallel()

	result := formatTimeNow()
	_, err := time.Parse(time.RFC3339Nano, result)
	assert.NoError(t, err)
}

func TestDerefTime_Nil(t *testing.T) {
	t.Parallel()

	result := derefTime(nil)
	assert.True(t, result.IsZero())
}

func TestDerefTime_NonNil(t *testing.T) {
	t.Parallel()

	ts := time.Date(2026, 5, 28, 12, 0, 0, 0, time.UTC)
	result := derefTime(&ts)
	assert.WithinDuration(t, ts, result, time.Microsecond)
}
