package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/sulthonzh/subscription-reconciler/internal/domain"
)

func TestNotificationSchedule_New(t *testing.T) {
	t.Parallel()
	db := setupTestDB(t)
	repo := NewNotificationRepo(db)
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Microsecond)
	n := domain.Notification{
		UserID:       "u1",
		Type:         domain.NotificationPremiumExpiresSoon,
		ScheduledFor: now.Add(24 * time.Hour),
		CreatedAt:    now,
	}

	inserted, err := repo.Schedule(ctx, n)
	require.NoError(t, err)
	assert.True(t, inserted)
}

func TestNotificationSchedule_Duplicate(t *testing.T) {
	t.Parallel()
	db := setupTestDB(t)
	repo := NewNotificationRepo(db)
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Microsecond)
	scheduled := now.Add(24 * time.Hour)
	n := domain.Notification{
		UserID:       "u1",
		Type:         domain.NotificationPremiumExpiresSoon,
		ScheduledFor: scheduled,
		CreatedAt:    now,
	}

	inserted, err := repo.Schedule(ctx, n)
	require.NoError(t, err)
	require.True(t, inserted)

	inserted, err = repo.Schedule(ctx, n)
	require.NoError(t, err)
	assert.False(t, inserted)
}

func TestNotificationFindDue(t *testing.T) {
	t.Parallel()
	db := setupTestDB(t)
	repo := NewNotificationRepo(db)
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Microsecond)
	past := now.Add(-1 * time.Hour)
	future := now.Add(24 * time.Hour)

	// Due notification
	n1 := domain.Notification{
		UserID:       "u1",
		Type:         domain.NotificationPremiumExpiresSoon,
		ScheduledFor: past,
		CreatedAt:    now,
	}
	// Future notification (not due yet)
	n2 := domain.Notification{
		UserID:       "u2",
		Type:         domain.NotificationPremiumExpiresSoon,
		ScheduledFor: future,
		CreatedAt:    now,
	}

	inserted, err := repo.Schedule(ctx, n1)
	require.NoError(t, err)
	require.True(t, inserted)

	inserted, err = repo.Schedule(ctx, n2)
	require.NoError(t, err)
	require.True(t, inserted)

	due, err := repo.FindDue(ctx, now, 10)
	require.NoError(t, err)
	assert.Len(t, due, 1)
	assert.Equal(t, "u1", due[0].UserID)
}

func TestNotificationFindDue_RespectsLimit(t *testing.T) {
	t.Parallel()
	db := setupTestDB(t)
	repo := NewNotificationRepo(db)
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Microsecond)
	base := now.Add(-2 * time.Hour)

	for i := 0; i < 5; i++ {
		n := domain.Notification{
			UserID:       "u" + string(rune('A'+i)),
			Type:         domain.NotificationPremiumExpiresSoon,
			ScheduledFor: base.Add(time.Duration(i) * time.Minute),
			CreatedAt:    now,
		}
		inserted, err := repo.Schedule(ctx, n)
		require.NoError(t, err)
		require.True(t, inserted)
	}

	due, err := repo.FindDue(ctx, now, 3)
	require.NoError(t, err)
	assert.Len(t, due, 3)
}

func TestNotificationMarkSent(t *testing.T) {
	t.Parallel()
	db := setupTestDB(t)
	repo := NewNotificationRepo(db)
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Microsecond)
	past := now.Add(-1 * time.Hour)

	n := domain.Notification{
		UserID:       "u1",
		Type:         domain.NotificationPremiumExpiresSoon,
		ScheduledFor: past,
		CreatedAt:    now,
	}
	inserted, err := repo.Schedule(ctx, n)
	require.NoError(t, err)
	require.True(t, inserted)

	due, err := repo.FindDue(ctx, now, 10)
	require.NoError(t, err)
	require.Len(t, due, 1)

	sentAt := now.Truncate(time.Microsecond)
	require.NoError(t, repo.MarkSent(ctx, due[0].ID, sentAt))

	// Should no longer appear as due
	due2, err := repo.FindDue(ctx, now, 10)
	require.NoError(t, err)
	assert.Empty(t, due2)
}

func TestNotificationFindDue_ScanError(t *testing.T) {
	t.Parallel()
	db := setupTestDB(t)
	repo := NewNotificationRepo(db)
	ctx := context.Background()

	_, err := db.ExecContext(ctx, `DROP TABLE notifications`)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `CREATE TABLE notifications (id TEXT PRIMARY KEY, user_id TEXT NOT NULL, type TEXT NOT NULL, scheduled_for TEXT NOT NULL, sent_at TEXT, created_at TEXT NOT NULL)`)
	require.NoError(t, err)

	_, err = db.ExecContext(ctx, `INSERT INTO notifications (id, user_id, type, scheduled_for, sent_at, created_at) VALUES ('notanumber', 'u1', 'PREMIUM_EXPIRES_SOON', '2026-01-01T00:00:00Z', NULL, '2026-01-01T00:00:00Z')`)
	require.NoError(t, err)

	_, err = repo.FindDue(ctx, time.Now().UTC().Add(24*time.Hour), 10)
	require.Error(t, err)
}

func TestNotificationSchedule_ClosedDB(t *testing.T) {
	t.Parallel()
	db := setupTestDB(t)
	repo := NewNotificationRepo(db)
	ctx := context.Background()

	require.NoError(t, db.Close())

	_, err := repo.Schedule(ctx, domain.Notification{
		UserID:       "u1",
		Type:         domain.NotificationPremiumExpiresSoon,
		ScheduledFor: time.Now().UTC(),
		CreatedAt:    time.Now().UTC(),
	})
	require.Error(t, err)
}

func TestNotificationFindDue_ClosedDB(t *testing.T) {
	t.Parallel()
	db := setupTestDB(t)
	repo := NewNotificationRepo(db)
	ctx := context.Background()

	require.NoError(t, db.Close())

	_, err := repo.FindDue(ctx, time.Now().UTC(), 10)
	require.Error(t, err)
}

func TestNotificationMarkSent_ClosedDB(t *testing.T) {
	t.Parallel()
	db := setupTestDB(t)
	repo := NewNotificationRepo(db)
	ctx := context.Background()

	require.NoError(t, db.Close())

	err := repo.MarkSent(ctx, 1, time.Now().UTC())
	require.Error(t, err)
}

func TestNotificationFindDue_Empty(t *testing.T) {
	t.Parallel()
	db := setupTestDB(t)
	repo := NewNotificationRepo(db)
	ctx := context.Background()

	now := time.Now().UTC()
	due, err := repo.FindDue(ctx, now, 10)
	require.NoError(t, err)
	assert.Empty(t, due)
}
