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

	n := NewNotifier(notifRepo, testLogger())

	count, err := n.SendDue(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 2, count)
	assert.Len(t, notifRepo.marked, 2)
	assert.Equal(t, int64(1), notifRepo.marked[0])
	assert.Equal(t, int64(2), notifRepo.marked[1])
}

func TestSendDue_NoDueNotifications(t *testing.T) {
	notifRepo := newMockNotifRepo()

	n := NewNotifier(notifRepo, testLogger())

	count, err := n.SendDue(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, count)
	assert.Len(t, notifRepo.marked, 0)
}

func TestNotifier_Run_RespectsContext(t *testing.T) {
	notifRepo := newMockNotifRepo()
	n := NewNotifier(notifRepo, testLogger())

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

	n := NewNotifier(notifRepo, testLogger())

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	n.Run(ctx, 5*time.Millisecond)

	assert.True(t, len(notifRepo.marked) >= 1, "ticker should fire and mark notification sent")
}

func TestSendDue_FindDueError(t *testing.T) {
	notifRepo := newMockNotifRepo()
	notifRepo.findDueErr = fmt.Errorf("db down")

	n := NewNotifier(notifRepo, testLogger())

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

	n := NewNotifier(notifRepo, testLogger())

	count, err := n.SendDue(context.Background())
	require.NoError(t, err, "MarkSent error should not fail SendDue")
	assert.Equal(t, 1, count)
}

func TestNotifier_Run_FindDueError(t *testing.T) {
	notifRepo := newMockNotifRepo()
	notifRepo.findDueErr = fmt.Errorf("db down")

	n := NewNotifier(notifRepo, testLogger())

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	n.Run(ctx, 5*time.Millisecond)
}
