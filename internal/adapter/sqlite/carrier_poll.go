package sqlite

import (
	"context"
	"database/sql"
	"time"
)

type CarrierPollLogRepo struct {
	db *sql.DB
}

func NewCarrierPollLogRepo(db *sql.DB) *CarrierPollLogRepo {
	return &CarrierPollLogRepo{db: db}
}

func (r *CarrierPollLogRepo) Insert(ctx context.Context, userID string, status string) error {
	const q = `
		INSERT INTO carrier_poll_log (user_id, status, polled_at, locked_until)
		VALUES (?, ?, ?, NULL)`

	_, err := getDB(ctx, r.db).ExecContext(ctx, q, userID, status, formatTime(time.Now()))
	return err
}

func (r *CarrierPollLogRepo) AcquireLock(ctx context.Context, userID string, lockedUntil time.Time) (bool, error) {
	var lockCount int
	err := getDB(ctx, r.db).QueryRowContext(ctx,
		"SELECT COUNT(*) FROM carrier_poll_log WHERE user_id = ? AND locked_until > ?",
		userID, formatTime(time.Now()),
	).Scan(&lockCount)
	if err != nil {
		return false, err
	}
	if lockCount > 0 {
		return false, nil
	}

	_, err = getDB(ctx, r.db).ExecContext(ctx,
		"INSERT INTO carrier_poll_log (user_id, status, polled_at, locked_until) VALUES (?, 'LOCKED', ?, ?)",
		userID, formatTime(time.Now()), formatTime(lockedUntil),
	)
	if err != nil {
		return false, err
	}
	return true, nil
}

func (r *CarrierPollLogRepo) ReleaseLock(ctx context.Context, userID string) error {
	const q = `
		UPDATE carrier_poll_log
		SET locked_until = NULL
		WHERE user_id = ? AND id = (
			SELECT MAX(id) FROM carrier_poll_log WHERE user_id = ?
		)`

	_, err := getDB(ctx, r.db).ExecContext(ctx, q, userID, userID)
	return err
}
