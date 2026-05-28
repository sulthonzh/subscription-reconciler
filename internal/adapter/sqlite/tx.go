package sqlite

import (
	"context"
	"database/sql"
	"fmt"
)

type txKey struct{}

type TxProvider struct {
	db *sql.DB
}

func NewTxProvider(db *sql.DB) *TxProvider {
	return &TxProvider{db: db}
}

func (p *TxProvider) WithinTx(ctx context.Context, fn func(ctx context.Context) error) error {
	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}

	txCtx := context.WithValue(ctx, txKey{}, tx)
	if err := fn(txCtx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func getDB(ctx context.Context, fallback *sql.DB) DBTX {
	if tx, ok := ctx.Value(txKey{}).(*sql.Tx); ok {
		return tx
	}
	return fallback
}

type DBTX = interface {
	ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row
}
