package domain

import "time"

type NotificationType string

const (
	NotificationPremiumExpiresSoon NotificationType = "PREMIUM_EXPIRES_SOON"
)

const NotificationLeadTime = 24 * time.Hour

type Notification struct {
	ID           int64
	UserID       string
	Type         NotificationType
	ScheduledFor time.Time
	SentAt       *time.Time
	CreatedAt    time.Time
}

// ScheduleNotification creates a notification for expiring premium.
// scheduledFor is 24h before expiresAt, clamped to now if in the past.
func ScheduleNotification(userID string, expiresAt time.Time, now time.Time) Notification {
	scheduledFor := expiresAt.Add(-NotificationLeadTime)
	if scheduledFor.Before(now) {
		scheduledFor = now
	}

	return Notification{
		UserID:       userID,
		Type:         NotificationPremiumExpiresSoon,
		ScheduledFor: scheduledFor,
		CreatedAt:    now,
	}
}
