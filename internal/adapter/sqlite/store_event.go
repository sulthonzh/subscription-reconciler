package sqlite

import (
	"context"
	"database/sql"

	"github.com/sulthonzh/subscription-reconciler/internal/domain"
)

type StoreEventRepo struct {
	db *sql.DB
}

func NewStoreEventRepo(db *sql.DB) *StoreEventRepo {
	return &StoreEventRepo{db: db}
}

func (r *StoreEventRepo) Insert(ctx context.Context, event domain.StoreEvent) (bool, error) {
	const q = `
		INSERT OR IGNORE INTO store_events (event_id, user_id, type, event_time_ms, product_id)
		VALUES (?, ?, ?, ?, ?)`

	res, err := getDB(ctx, r.db).ExecContext(ctx, q,
		event.EventID,
		event.UserID,
		string(event.Type),
		event.EventTimeMs,
		event.ProductID,
	)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}
