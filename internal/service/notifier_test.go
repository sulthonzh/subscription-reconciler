package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sulthonzh/subscription-reconciler/internal/domain"
)

func TestSendDue_MarksNotifications(t *testing.T) {
	notifRepo := newMockNotifRepo()
	now := time.Now()
	notifRepo.due = []domain.Notification{
		{ID: 1, UserID: "u_42", Type: domain.NotificationPremiumExpiresSoon, ScheduledFor: now},
		{ID: 2, UserID: "u_43", Type: domain.NotificationPremiumExpiresSoon, ScheduledFor: now},
	}

	n := NewNotifier(nil, notifRepo, testLogger())

	count, err := n.SendDue(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 2, count)
	assert.Len(t, notifRepo.marked, 2)
	assert.Equal(t, int64(1), notifRepo.marked[0])
	assert.Equal(t, int64(2), notifRepo.marked[1])
}

func TestSendDue_NoDueNotifications(t *testing.T) {
	notifRepo := newMockNotifRepo()

	n := NewNotifier(nil, notifRepo, testLogger())

	count, err := n.SendDue(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, count)
	assert.Len(t, notifRepo.marked, 0)
}

func TestNotifier_Run_RespectsContext(t *testing.T) {
	notifRepo := newMockNotifRepo()
	n := NewNotifier(nil, notifRepo, testLogger())

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	n.Run(ctx, 10*time.Millisecond)
}

func TestNotifier_Run_TickerFires(t *testing.T) {
	notifRepo := newMockNotifRepo()
	now := time.Now()
	notifRepo.due = []domain.Notification{
		{ID: 1, UserID: "u_42", Type: domain.NotificationPremiumExpiresSoon, ScheduledFor: now},
	}

	n := NewNotifier(nil, notifRepo, testLogger())

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	n.Run(ctx, 5*time.Millisecond)

	assert.True(t, len(notifRepo.marked) >= 1, "ticker should fire and mark notification sent")
}

func TestSendDue_FindDueError(t *testing.T) {
	notifRepo := newMockNotifRepo()
	notifRepo.findDueErr = fmt.Errorf("db down")

	n := NewNotifier(nil, notifRepo, testLogger())

	_, err := n.SendDue(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "db down")
}

func TestSendDue_MarkSentError(t *testing.T) {
	notifRepo := newMockNotifRepo()
	now := time.Now()
	notifRepo.due = []domain.Notification{
		{ID: 1, UserID: "u_42", Type: domain.NotificationPremiumExpiresSoon, ScheduledFor: now},
	}
	notifRepo.markSentErr = fmt.Errorf("mark fail")

	n := NewNotifier(nil, notifRepo, testLogger())

	count, err := n.SendDue(context.Background())
	require.NoError(t, err, "MarkSent error should not fail SendDue")
	assert.Equal(t, 1, count)
}

func TestNotifier_Run_FindDueError(t *testing.T) {
	notifRepo := newMockNotifRepo()
	notifRepo.findDueErr = fmt.Errorf("db down")

	n := NewNotifier(nil, notifRepo, testLogger())

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	n.Run(ctx, 5*time.Millisecond)
}

func TestScheduleForExpiring_MultipleEntitlements(t *testing.T) {
	entRepo := newMockEntRepo()
	notifRepo := newMockNotifRepo()
	now := time.Now()
	threshold := now.Add(domain.NotificationLeadTime)

	entRepo.entitlements["u_1:STORE"] = &domain.Entitlement{
		UserID: "u_1", Source: domain.SourceStore,
		Active: true, ExpiresAt: &threshold, LastChangedAt: now, CreatedAt: now,
	}
	entRepo.entitlements["u_2:STORE"] = &domain.Entitlement{
		UserID: "u_2", Source: domain.SourceStore,
		Active: true, ExpiresAt: &threshold, LastChangedAt: now, CreatedAt: now,
	}

	n := NewNotifier(entRepo, notifRepo, testLogger())

	scheduled, err := n.ScheduleForExpiring(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 2, scheduled)
	assert.Len(t, notifRepo.scheduled, 2)
	assert.Contains(t, []string{"u_1", "u_2"}, notifRepo.scheduled[0].UserID)
	assert.Contains(t, []string{"u_1", "u_2"}, notifRepo.scheduled[1].UserID)
}

func TestScheduleForExpiring_GetExpiringBeforeError(t *testing.T) {
	entRepo := newMockEntRepo()
	entRepo.getExpiringBeforeErr = fmt.Errorf("db down")

	n := NewNotifier(entRepo, newMockNotifRepo(), testLogger())

	_, err := n.ScheduleForExpiring(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "db down")
}

func TestScheduleForExpiring_ScheduleErrorContinue(t *testing.T) {
	entRepo := newMockEntRepo()
	notifRepo := newMockNotifRepo()
	now := time.Now()
	threshold := now.Add(domain.NotificationLeadTime)

	entRepo.entitlements["u_1:STORE"] = &domain.Entitlement{
		UserID: "u_1", Source: domain.SourceStore,
		Active: true, ExpiresAt: &threshold, LastChangedAt: now, CreatedAt: now,
	}
	entRepo.entitlements["u_2:STORE"] = &domain.Entitlement{
		UserID: "u_2", Source: domain.SourceStore,
		Active: true, ExpiresAt: &threshold, LastChangedAt: now, CreatedAt: now,
	}

	notifRepo.firstScheduleFail = true

	n := NewNotifier(entRepo, notifRepo, testLogger())

	scheduled, err := n.ScheduleForExpiring(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, scheduled, "Should continue after one fails")
	assert.Len(t, notifRepo.scheduled, 2, "Should store both notifications but only count inserted ones")
	assert.Contains(t, []string{"u_1", "u_2"}, notifRepo.scheduled[0].UserID)
	assert.Contains(t, []string{"u_1", "u_2"}, notifRepo.scheduled[1].UserID)
}

func TestScheduleForExpiring_ScheduleDuplicate(t *testing.T) {
	entRepo := newMockEntRepo()
	notifRepo := newMockNotifRepo()
	now := time.Now()
	threshold := now.Add(domain.NotificationLeadTime)

	// Configure to return false (duplicate)
	notifRepo.newScheduleReturnFalse = true

	entRepo.entitlements["u_1:STORE"] = &domain.Entitlement{
		UserID: "u_1", Source: domain.SourceStore,
		Active: true, ExpiresAt: &threshold, LastChangedAt: now, CreatedAt: now,
	}

	n := NewNotifier(entRepo, notifRepo, testLogger())

	scheduled, err := n.ScheduleForExpiring(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, scheduled, "Should not count duplicates")
	assert.Len(t, notifRepo.scheduled, 1, "Should still schedule the notification")
}

func TestScheduleForExpiring_NoEntitlements(t *testing.T) {
	entRepo := newMockEntRepo()
	notifRepo := newMockNotifRepo()

	n := NewNotifier(entRepo, notifRepo, testLogger())

	scheduled, err := n.ScheduleForExpiring(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, scheduled)
	assert.Len(t, notifRepo.scheduled, 0)
}
