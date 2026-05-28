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
	const q = `
		SELECT user_id, source, active, expires_at, reason, last_changed_at, created_at
		FROM entitlements
		WHERE user_id = ? AND source = ?`

	row := r.db.QueryRowContext(ctx, q, userID, string(source))
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
	const q = `
		SELECT user_id, source, active, expires_at, reason, last_changed_at, created_at
		FROM entitlements
		WHERE user_id = ?`

	rows, err := r.db.QueryContext(ctx, q, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []domain.Entitlement
	for rows.Next() {
		var e domain.Entitlement
		var expiresAt sql.NullString
		var sourceStr string
		if err := rows.Scan(
			&e.UserID, &sourceStr, &e.Active, &expiresAt,
			&e.Reason, &skipTime{}, &skipTime{},
		); err != nil {
			return nil, err
		}
		e.Source = domain.Source(sourceStr)
		e.ExpiresAt = scanTime(expiresAt)
		result = append(result, e)
	}
	return result, rows.Err()
}

func (r *EntitlementRepo) Upsert(ctx context.Context, entitlement domain.Entitlement) error {
	const q = `
		INSERT OR REPLACE INTO entitlements (user_id, source, active, expires_at, reason, last_changed_at, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`

	var expiresAt interface{}
	if entitlement.ExpiresAt != nil {
		expiresAt = formatTime(*entitlement.ExpiresAt)
	}

	_, err := r.db.ExecContext(ctx, q,
		entitlement.UserID,
		string(entitlement.Source),
		entitlement.Active,
		expiresAt,
		entitlement.Reason,
		formatTime(entitlement.LastChangedAt),
		formatTime(entitlement.CreatedAt),
	)
	return err
}

func (r *EntitlementRepo) GetActiveBySource(ctx context.Context, source domain.Source) ([]domain.Entitlement, error) {
	const q = `
		SELECT user_id, source, active, expires_at, reason, last_changed_at, created_at
		FROM entitlements
		WHERE source = ? AND active = true`

	rows, err := r.db.QueryContext(ctx, q, string(source))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanEntitlements(rows)
}

func (r *EntitlementRepo) UpdateActive(ctx context.Context, userID string, source domain.Source, active bool, reason string) error {
	const q = `
		UPDATE entitlements
		SET active = ?, reason = ?, last_changed_at = ?
		WHERE user_id = ? AND source = ?`

	_, err := r.db.ExecContext(ctx, q,
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
	res, err := r.db.ExecContext(ctx, q, nowStr, nowStr)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}
