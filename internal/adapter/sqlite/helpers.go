package sqlite

import (
	"database/sql"
	"time"

	"github.com/sulthonzh/subscription-reconciler/internal/domain"
)

func formatTimeNow() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}

func derefTime(p *time.Time) time.Time {
	if p != nil {
		return *p
	}
	return time.Time{}
}

const entitlementColumns = `
	user_id, source, active, expires_at, reason, last_changed_at, last_event_time_ms, created_at`

func scanEntitlement(row *sql.Row) (domain.Entitlement, error) {
	var e domain.Entitlement
	var sourceStr string
	var expiresAt sql.NullString
	var lastChangedAt sql.NullString
	var createdAt sql.NullString
	err := row.Scan(
		&e.UserID, &sourceStr, &e.Active, &expiresAt,
		&e.Reason, &lastChangedAt, &e.LastEventTimeMs, &createdAt,
	)
	if err != nil {
		return e, err
	}
	e.Source = domain.Source(sourceStr)
	e.ExpiresAt = scanTime(expiresAt)
	e.LastChangedAt = derefTime(scanTime(lastChangedAt))
	e.CreatedAt = derefTime(scanTime(createdAt))
	return e, nil
}

func scanEntitlements(rows *sql.Rows) ([]domain.Entitlement, error) {
	var result []domain.Entitlement
	for rows.Next() {
		var e domain.Entitlement
		var sourceStr string
		var expiresAt sql.NullString
		var lastChangedAt sql.NullString
		var createdAt sql.NullString
		if err := rows.Scan(
			&e.UserID, &sourceStr, &e.Active, &expiresAt,
			&e.Reason, &lastChangedAt, &e.LastEventTimeMs, &createdAt,
		); err != nil {
			return nil, err
		}
		e.Source = domain.Source(sourceStr)
		e.ExpiresAt = scanTime(expiresAt)
		e.LastChangedAt = derefTime(scanTime(lastChangedAt))
		e.CreatedAt = derefTime(scanTime(createdAt))
		result = append(result, e)
	}
	return result, rows.Err()
}
