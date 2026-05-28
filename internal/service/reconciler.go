package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/sulthonzh/subscription-reconciler/internal/domain"
	"github.com/sulthonzh/subscription-reconciler/internal/port"
)

type Reconciler struct {
	entRepo    port.EntitlementRepository
	eventRepo  port.StoreEventRepository
	notifRepo  port.NotificationRepository
	auditRepo  port.AuditLogRepository
	txProvider port.TransactionProvider
	logger     *slog.Logger
}

func NewReconciler(
	entRepo port.EntitlementRepository,
	eventRepo port.StoreEventRepository,
	notifRepo port.NotificationRepository,
	auditRepo port.AuditLogRepository,
	txProvider port.TransactionProvider,
	logger *slog.Logger,
) *Reconciler {
	return &Reconciler{
		entRepo:    entRepo,
		eventRepo:  eventRepo,
		notifRepo:  notifRepo,
		auditRepo:  auditRepo,
		txProvider: txProvider,
		logger:     logger,
	}
}

func (r *Reconciler) ProcessStoreEvent(ctx context.Context, event domain.StoreEvent) (bool, error) {
	var processed bool
	txErr := r.txProvider.WithinTx(ctx, func(txCtx context.Context) error {
		inserted, err := r.eventRepo.Insert(txCtx, event)
		if err != nil {
			return fmt.Errorf("insert store event: %w", err)
		}
		if !inserted {
			r.logger.Info("duplicate event ignored",
				slog.String("event_id", event.EventID),
				slog.String("user_id", event.UserID),
			)
			return nil
		}

		existing, err := r.entRepo.GetByUserAndSource(txCtx, event.UserID, domain.SourceStore)
		if err != nil {
			return fmt.Errorf("get entitlement: %w", err)
		}

		if existing != nil {
			if existing.LastEventTimeMs > event.EventTimeMs {
				r.logger.Info("stale event ignored",
					slog.String("event_id", event.EventID),
					slog.String("user_id", event.UserID),
					slog.Int64("last_event_time_ms", existing.LastEventTimeMs),
					slog.Int64("event_time_ms", event.EventTimeMs),
				)
				return nil
			}
		}

		var prevState string
		if existing != nil {
			prevState = entitlementJSON(existing)
		}

		newEntitlement, changed, err := domain.ApplyStoreEvent(existing, event, time.Now())
		if err != nil {
			return fmt.Errorf("apply store event: %w", err)
		}

		if err := r.entRepo.Upsert(txCtx, *newEntitlement); err != nil {
			return fmt.Errorf("upsert entitlement: %w", err)
		}

		if changed && newEntitlement.Active && newEntitlement.ExpiresAt != nil && event.Type != domain.EventBillingIssue {
			notif := domain.ScheduleNotification(event.UserID, *newEntitlement.ExpiresAt, time.Now())
			if _, err := r.notifRepo.Schedule(txCtx, notif); err != nil {
				r.logger.Error("failed to schedule notification",
					slog.String("user_id", event.UserID),
					slog.String("error", err.Error()),
				)
			}
		}

		if r.auditRepo != nil && changed {
			nextState := entitlementJSON(newEntitlement)
			entry := domain.AuditEntry{
				UserID:        event.UserID,
				TriggerID:     event.EventID,
				Source:        domain.SourceStore,
				PreviousState: prevState,
				NextState:     nextState,
				CreatedAt:     time.Now(),
			}
			if err := r.auditRepo.Insert(txCtx, entry); err != nil {
				r.logger.Error("failed to write audit entry",
					slog.String("user_id", event.UserID),
					slog.String("error", err.Error()),
				)
			}
		}

		processed = true
		return nil
	})
	if txErr != nil {
		return false, txErr
	}
	return processed, nil
}

func (r *Reconciler) RevokeMarketplace(ctx context.Context, userIDs []string) (revoked int, skipped int, err error) {
	txErr := r.txProvider.WithinTx(ctx, func(txCtx context.Context) error {
		for _, userID := range userIDs {
			ent, err := r.entRepo.GetByUserAndSource(txCtx, userID, domain.SourceMarketplace)
			if err != nil {
				return fmt.Errorf("get entitlement for %s: %w", userID, err)
			}
			if ent == nil || !ent.Active {
				skipped++
				continue
			}

			if err := r.entRepo.UpdateActive(txCtx, userID, domain.SourceMarketplace, false, "MARKETPLACE_REVOKED"); err != nil {
				return fmt.Errorf("revoke marketplace for %s: %w", userID, err)
			}

			if r.auditRepo != nil {
				entry := domain.AuditEntry{
					UserID:        userID,
					TriggerID:     "marketplace_revoke",
					Source:        domain.SourceMarketplace,
					PreviousState: entitlementJSON(ent),
					NextState:     `{"active":false,"source":"MARKETPLACE","reason":"MARKETPLACE_REVOKED"}`,
					CreatedAt:     time.Now(),
				}
				if err := r.auditRepo.Insert(txCtx, entry); err != nil {
					r.logger.Error("failed to write audit entry",
						slog.String("user_id", userID),
						slog.String("error", err.Error()),
					)
				}
			}

			revoked++
		}
		return nil
	})
	if txErr != nil {
		return revoked, skipped, txErr
	}
	return revoked, skipped, nil
}

func (r *Reconciler) GetEntitlement(ctx context.Context, userID string) (*domain.Entitlement, error) {
	rows, err := r.entRepo.GetByUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("get entitlements: %w", err)
	}

	resolved := domain.ResolveEntitlements(rows)
	return &resolved, nil
}

func (r *Reconciler) GetTimeline(ctx context.Context, userID string) ([]domain.AuditEntry, error) {
	return r.auditRepo.GetByUser(ctx, userID)
}

func (r *Reconciler) ExpireOverdue(ctx context.Context) (int, error) {
	return r.entRepo.ExpireOverdue(ctx, time.Now())
}

func entitlementJSON(e *domain.Entitlement) string {
	type state struct {
		Active bool   `json:"active"`
		Source string `json:"source"`
		Reason string `json:"reason"`
	}
	s := state{
		Active: e.Active,
		Source: string(e.Source),
		Reason: e.Reason,
	}
	b, _ := json.Marshal(s)
	return string(b)
}
