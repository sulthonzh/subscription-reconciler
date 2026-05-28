package domain

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestScheduleNotification(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name             string
		expiresAt        time.Time
		wantScheduledFor time.Time
	}{
		{
			name:             "future expiry schedules 24h before",
			expiresAt:        time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC),
			wantScheduledFor: time.Date(2026, 6, 9, 0, 0, 0, 0, time.UTC),
		},
		{
			name:             "less than 24h clamps to now",
			expiresAt:        time.Date(2026, 5, 20, 18, 0, 0, 0, time.UTC), // 6h from now
			wantScheduledFor: now,
		},
		{
			name:             "already passed clamps to now",
			expiresAt:        time.Date(2026, 5, 19, 0, 0, 0, 0, time.UTC), // yesterday
			wantScheduledFor: now,
		},
		{
			name:             "exactly 24h away schedules at now",
			expiresAt:        now.Add(24 * time.Hour),
			wantScheduledFor: now,
		},
		{
			name:             "more than 24h away schedules properly",
			expiresAt:        now.Add(48 * time.Hour),
			wantScheduledFor: now.Add(24 * time.Hour),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := ScheduleNotification("u_42", tt.expiresAt, now)

			assert.Equal(t, "u_42", result.UserID)
			assert.Equal(t, NotificationPremiumExpiresSoon, result.Type)
			assert.Equal(t, tt.wantScheduledFor, result.ScheduledFor)
			assert.Equal(t, now, result.CreatedAt)
			assert.Nil(t, result.SentAt)
		})
	}
}
