package sqlite

import (
	"database/sql"
	"time"

	"github.com/sulthonzh/subscription-reconciler/internal/domain"
)

// skipTime scans a TEXT column representing a timestamp but discards the value.
type skipTime struct{}

func (skipTime) Scan(interface{}) error { return nil }

func formatTimeNow() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}

func scanEntitlement(row *sql.Row) (domain.Entitlement, error) {
	var e domain.Entitlement
	var sourceStr string
	var expiresAt sql.NullString
	err := row.Scan(
		&e.UserID, &sourceStr, &e.Active, &expiresAt,
		&e.Reason, &skipTime{}, &skipTime{},
	)
	if err != nil {
		return e, err
	}
	e.Source = domain.Source(sourceStr)
	e.ExpiresAt = scanTime(expiresAt)
	return e, nil
}

func scanEntitlements(rows *sql.Rows) ([]domain.Entitlement, error) {
	var result []domain.Entitlement
	for rows.Next() {
		var e domain.Entitlement
		var sourceStr string
		var expiresAt sql.NullString
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
