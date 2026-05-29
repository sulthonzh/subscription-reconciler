package sqlite

import (
	"context"
	"database/sql"
	"time"

	"github.com/sulthonzh/subscription-reconciler/internal/domain"
)

type EntitlementRepo struct {
	db *sql.DB
}

func NewEntitlementRepo(db *sql.DB) *EntitlementRepo {
	return &EntitlementRepo{db: db}
}

func (r *EntitlementRepo) GetByUserAndSource(ctx context.Context, userID string, source domain.Source) (*domain.Entitlement, error) {
	q := `
		SELECT ` + entitlementColumns[1:] + `
		FROM entitlements
		WHERE user_id = ? AND source = ?`

	row := getDB(ctx, r.db).QueryRowContext(ctx, q, userID, string(source))
	e, err := scanEntitlement(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &e, nil
}

func (r *EntitlementRepo) GetByUser(ctx context.Context, userID string) ([]domain.Entitlement, error) {
	q := `
		SELECT ` + entitlementColumns[1:] + `
		FROM entitlements
		WHERE user_id = ?`

	rows, err := getDB(ctx, r.db).QueryContext(ctx, q, userID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	return scanEntitlements(rows)
}

func (r *EntitlementRepo) Upsert(ctx context.Context, entitlement domain.Entitlement) error {
	const q = `
		INSERT OR REPLACE INTO entitlements (user_id, source, active, expires_at, reason, last_changed_at, last_event_time_ms, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`

	var expiresAt interface{}
	if entitlement.ExpiresAt != nil {
		expiresAt = formatTime(*entitlement.ExpiresAt)
	}

	_, err := getDB(ctx, r.db).ExecContext(ctx, q,
		entitlement.UserID,
		string(entitlement.Source),
		entitlement.Active,
		expiresAt,
		entitlement.Reason,
		formatTime(entitlement.LastChangedAt),
		entitlement.LastEventTimeMs,
		formatTime(entitlement.CreatedAt),
	)
	return err
}

func (r *EntitlementRepo) GetActiveBySource(ctx context.Context, source domain.Source) ([]domain.Entitlement, error) {
	q := `
		SELECT ` + entitlementColumns[1:] + `
		FROM entitlements
		WHERE source = ? AND active = true`

	rows, err := getDB(ctx, r.db).QueryContext(ctx, q, string(source))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	return scanEntitlements(rows)
}

func (r *EntitlementRepo) UpdateActive(ctx context.Context, userID string, source domain.Source, active bool, reason string) error {
	const q = `
		UPDATE entitlements
		SET active = ?, reason = ?, last_changed_at = ?
		WHERE user_id = ? AND source = ?`

	_, err := getDB(ctx, r.db).ExecContext(ctx, q,
		active, reason, formatTimeNow(),
		userID, string(source),
	)
	return err
}

func (r *EntitlementRepo) ExpireOverdue(ctx context.Context, now time.Time) (int, error) {
	const q = `
		UPDATE entitlements
		SET active = false, reason = 'EXPIRED', last_changed_at = ?
		WHERE active = true AND expires_at IS NOT NULL AND expires_at < ?`

	nowStr := formatTime(now)
	res, err := getDB(ctx, r.db).ExecContext(ctx, q, nowStr, nowStr)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

func (r *EntitlementRepo) GetExpiringBefore(ctx context.Context, before time.Time) ([]domain.Entitlement, error) {
	q := `SELECT ` + entitlementColumns[1:] + ` FROM entitlements WHERE active = true AND expires_at IS NOT NULL AND expires_at <= ?`
	rows, err := getDB(ctx, r.db).QueryContext(ctx, q, formatTime(before))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	return scanEntitlements(rows)
}
