package sqlite

import (
	"context"
	"database/sql"

	"github.com/sulthonzh/subscription-reconciler/internal/domain"
)

type AuditLogRepo struct {
	db *sql.DB
}

func NewAuditLogRepo(db *sql.DB) *AuditLogRepo {
	return &AuditLogRepo{db: db}
}

func (r *AuditLogRepo) Insert(ctx context.Context, entry domain.AuditEntry) error {
	const q = `
		INSERT INTO audit_log (user_id, trigger_id, source, previous_state, next_state, created_at)
		VALUES (?, ?, ?, ?, ?, ?)`

	_, err := getDB(ctx, r.db).ExecContext(ctx, q,
		entry.UserID,
		entry.TriggerID,
		string(entry.Source),
		entry.PreviousState,
		entry.NextState,
		formatTime(entry.CreatedAt),
	)
	return err
}

func (r *AuditLogRepo) GetByUser(ctx context.Context, userID string) ([]domain.AuditEntry, error) {
	const q = `
		SELECT id, user_id, trigger_id, source, previous_state, next_state, created_at
		FROM audit_log
		WHERE user_id = ?
		ORDER BY created_at ASC`

	rows, err := getDB(ctx, r.db).QueryContext(ctx, q, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []domain.AuditEntry
	for rows.Next() {
		var e domain.AuditEntry
		var sourceStr string
		var triggerID sql.NullString
		var createdAt sql.NullString
		if err := rows.Scan(
			&e.ID, &e.UserID, &triggerID, &sourceStr,
			&e.PreviousState, &e.NextState, &createdAt,
		); err != nil {
			return nil, err
		}
		e.TriggerID = triggerID.String
		e.Source = domain.Source(sourceStr)
		e.CreatedAt = derefTime(scanTime(createdAt))
		result = append(result, e)
	}
	return result, rows.Err()
}
