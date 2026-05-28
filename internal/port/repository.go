package port

import (
	"context"
	"database/sql"
	"time"

	"github.com/sulthonzh/subscription-reconciler/internal/domain"
)

type DBTX interface {
	ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row
}

type TransactionProvider interface {
	WithinTx(ctx context.Context, fn func(ctx context.Context) error) error
}

type EntitlementRepository interface {
	GetByUserAndSource(ctx context.Context, userID string, source domain.Source) (*domain.Entitlement, error)
	GetByUser(ctx context.Context, userID string) ([]domain.Entitlement, error)
	Upsert(ctx context.Context, entitlement domain.Entitlement) error
	GetActiveBySource(ctx context.Context, source domain.Source) ([]domain.Entitlement, error)
	UpdateActive(ctx context.Context, userID string, source domain.Source, active bool, reason string) error
	ExpireOverdue(ctx context.Context, now time.Time) (int, error)
	GetExpiringBefore(ctx context.Context, before time.Time) ([]domain.Entitlement, error)
}

type StoreEventRepository interface {
	Insert(ctx context.Context, event domain.StoreEvent) (bool, error)
}

type CarrierPollLogRepository interface {
	Insert(ctx context.Context, userID string, status string) error
	AcquireLock(ctx context.Context, userID string, lockedUntil time.Time) (bool, error)
	ReleaseLock(ctx context.Context, userID string) error
}

type NotificationRepository interface {
	Schedule(ctx context.Context, notification domain.Notification) (bool, error)
	FindDue(ctx context.Context, now time.Time, limit int) ([]domain.Notification, error)
	MarkSent(ctx context.Context, id int64, now time.Time) error
}

type AuditLogRepository interface {
	Insert(ctx context.Context, entry domain.AuditEntry) error
	GetByUser(ctx context.Context, userID string) ([]domain.AuditEntry, error)
}
