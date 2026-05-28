package domain

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetExpiryTime(t *testing.T) {
	t.Parallel()

	eventTimeMs := int64(1716700000000) // 2024-05-26 04:26:40 UTC
	eventTime := time.UnixMilli(eventTimeMs)

	tests := []struct {
		name       string
		productID  string
		wantOffset time.Duration
		wantErr    bool
	}{
		{
			name:       "monthly product gives 30-day offset",
			productID:  "premium_monthly",
			wantOffset: 30 * 24 * time.Hour,
			wantErr:    false,
		},
		{
			name:       "yearly product gives 365-day offset",
			productID:  "premium_yearly",
			wantOffset: 365 * 24 * time.Hour,
			wantErr:    false,
		},
		{
			name:      "unknown product returns error",
			productID: "premium_lifetime",
			wantErr:   true,
		},
		{
			name:      "empty product returns error",
			productID: "",
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result, err := GetExpiryTime(eventTimeMs, tt.productID)

			if tt.wantErr {
				require.Error(t, err)
				assert.ErrorIs(t, err, ErrProductNotFound)
				assert.True(t, result.IsZero())
				return
			}

			require.NoError(t, err)
			expected := eventTime.Add(tt.wantOffset)
			assert.Equal(t, expected, result)
		})
	}
}
