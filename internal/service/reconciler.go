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
	entRepo   port.EntitlementRepository
	eventRepo port.StoreEventRepository
	notifRepo port.NotificationRepository
	auditRepo port.AuditLogRepository
	logger    *slog.Logger
}

func NewReconciler(
	entRepo port.EntitlementRepository,
	eventRepo port.StoreEventRepository,
	notifRepo port.NotificationRepository,
	auditRepo port.AuditLogRepository,
	logger *slog.Logger,
) *Reconciler {
	return &Reconciler{
		entRepo:   entRepo,
		eventRepo: eventRepo,
		notifRepo: notifRepo,
		auditRepo: auditRepo,
		logger:    logger,
	}
}

func (r *Reconciler) ProcessStoreEvent(ctx context.Context, event domain.StoreEvent) (bool, error) {
	inserted, err := r.eventRepo.Insert(ctx, event)
	if err != nil {
		return false, fmt.Errorf("insert store event: %w", err)
	}
	if !inserted {
		r.logger.Info("duplicate event ignored",
			slog.String("event_id", event.EventID),
			slog.String("user_id", event.UserID),
		)
		return false, nil
	}

	existing, err := r.entRepo.GetByUserAndSource(ctx, event.UserID, domain.SourceStore)
	if err != nil {
		return false, fmt.Errorf("get entitlement: %w", err)
	}

	if existing != nil {
		eventTime := time.UnixMilli(event.EventTimeMs)
		if existing.LastChangedAt.After(eventTime) {
			r.logger.Info("stale event ignored",
				slog.String("event_id", event.EventID),
				slog.String("user_id", event.UserID),
				slog.Time("last_changed_at", existing.LastChangedAt),
				slog.Time("event_time", eventTime),
			)
			return false, nil
		}
	}

	var prevState string
	if existing != nil {
		prevState = entitlementJSON(existing)
	}

	newEntitlement, changed, err := domain.ApplyStoreEvent(existing, event, time.Now())
	if err != nil {
		return false, fmt.Errorf("apply store event: %w", err)
	}

	if err := r.entRepo.Upsert(ctx, *newEntitlement); err != nil {
		return false, fmt.Errorf("upsert entitlement: %w", err)
	}

	if changed && newEntitlement.Active && newEntitlement.ExpiresAt != nil && event.Type != domain.EventBillingIssue {
		notif := domain.ScheduleNotification(event.UserID, *newEntitlement.ExpiresAt, time.Now())
		if _, err := r.notifRepo.Schedule(ctx, notif); err != nil {
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
		if err := r.auditRepo.Insert(ctx, entry); err != nil {
			r.logger.Error("failed to write audit entry",
				slog.String("user_id", event.UserID),
				slog.String("error", err.Error()),
			)
		}
	}

	return true, nil
}

func (r *Reconciler) RevokeMarketplace(ctx context.Context, userIDs []string) (revoked int, skipped int, err error) {
	for _, userID := range userIDs {
		ent, err := r.entRepo.GetByUserAndSource(ctx, userID, domain.SourceMarketplace)
		if err != nil {
			return revoked, skipped, fmt.Errorf("get entitlement for %s: %w", userID, err)
		}
		if ent == nil || !ent.Active {
			skipped++
			continue
		}

		if err := r.entRepo.UpdateActive(ctx, userID, domain.SourceMarketplace, false, "MARKETPLACE_REVOKED"); err != nil {
			return revoked, skipped, fmt.Errorf("revoke marketplace for %s: %w", userID, err)
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
			if err := r.auditRepo.Insert(ctx, entry); err != nil {
				r.logger.Error("failed to write audit entry",
					slog.String("user_id", userID),
					slog.String("error", err.Error()),
				)
			}
		}

		revoked++
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
