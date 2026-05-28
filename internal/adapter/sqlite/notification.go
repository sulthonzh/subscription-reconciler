package sqlite

import (
	"context"
	"database/sql"
	"time"

	"github.com/sulthonzh/subscription-reconciler/internal/domain"
)

type NotificationRepo struct {
	db *sql.DB
}

func NewNotificationRepo(db *sql.DB) *NotificationRepo {
	return &NotificationRepo{db: db}
}

func (r *NotificationRepo) Schedule(ctx context.Context, notification domain.Notification) (bool, error) {
	const q = `
		INSERT OR IGNORE INTO notifications (user_id, type, scheduled_for, created_at)
		VALUES (?, ?, ?, ?)`

	res, err := getDB(ctx, r.db).ExecContext(ctx, q,
		notification.UserID,
		string(notification.Type),
		formatTime(notification.ScheduledFor),
		formatTime(notification.CreatedAt),
	)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

func (r *NotificationRepo) FindDue(ctx context.Context, now time.Time, limit int) ([]domain.Notification, error) {
	const q = `
		SELECT id, user_id, type, scheduled_for, sent_at, created_at
		FROM notifications
		WHERE sent_at IS NULL AND scheduled_for <= ?
		ORDER BY scheduled_for
		LIMIT ?`

	rows, err := getDB(ctx, r.db).QueryContext(ctx, q, formatTime(now), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []domain.Notification
	for rows.Next() {
		var n domain.Notification
		var typeStr string
		var scheduledFor sql.NullString
		var sentAt sql.NullString
		var createdAt sql.NullString
		if err := rows.Scan(&n.ID, &n.UserID, &typeStr, &scheduledFor, &sentAt, &createdAt); err != nil {
			return nil, err
		}
		n.Type = domain.NotificationType(typeStr)
		n.ScheduledFor = derefTime(scanTime(scheduledFor))
		n.SentAt = scanTime(sentAt)
		n.CreatedAt = derefTime(scanTime(createdAt))
		result = append(result, n)
	}
	return result, rows.Err()
}

func (r *NotificationRepo) MarkSent(ctx context.Context, id int64, now time.Time) error {
	const q = `UPDATE notifications SET sent_at = ? WHERE id = ?`
	_, err := getDB(ctx, r.db).ExecContext(ctx, q, formatTime(now), id)
	return err
}
