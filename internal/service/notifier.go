package service

import (
	"context"
	"log/slog"
	"time"

	"github.com/sulthonzh/subscription-reconciler/internal/domain"
	"github.com/sulthonzh/subscription-reconciler/internal/port"
)

type Notifier struct {
	entRepo   port.EntitlementRepository
	notifRepo port.NotificationRepository
	logger    *slog.Logger
}

func NewNotifier(entRepo port.EntitlementRepository, notifRepo port.NotificationRepository, logger *slog.Logger) *Notifier {
	return &Notifier{
		entRepo:   entRepo,
		notifRepo: notifRepo,
		logger:    logger,
	}
}

func (n *Notifier) Run(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			n.logger.Info("notifier shutting down")
			return
		case <-ticker.C:
			count, err := n.SendDue(ctx)
			if err != nil {
				n.logger.Error("failed to send due notifications",
					slog.String("error", err.Error()),
				)
			} else if count > 0 {
				n.logger.Info("sent notifications",
					slog.Int("count", count),
				)
			}
		}
	}
}

func (n *Notifier) SendDue(ctx context.Context) (int, error) {
	due, err := n.notifRepo.FindDue(ctx, time.Now(), 100)
	if err != nil {
		return 0, err
	}

	for _, notif := range due {
		if err := n.notifRepo.MarkSent(ctx, notif.ID, time.Now()); err != nil {
			n.logger.Error("failed to mark notification sent",
				slog.Int64("id", notif.ID),
				slog.String("error", err.Error()),
			)
		}
	}

	return len(due), nil
}

func (n *Notifier) ScheduleForExpiring(ctx context.Context) (int, error) {
	now := time.Now()
	threshold := now.Add(domain.NotificationLeadTime)
	ents, err := n.entRepo.GetExpiringBefore(ctx, threshold)
	if err != nil {
		return 0, err
	}
	scheduled := 0
	for _, ent := range ents {
		notif := domain.ScheduleNotification(ent.UserID, *ent.ExpiresAt, now)
		inserted, err := n.notifRepo.Schedule(ctx, notif)
		if err != nil {
			n.logger.Error("failed to schedule notification", slog.String("user_id", ent.UserID), slog.String("error", err.Error()))
			continue
		}
		if inserted {
			scheduled++
		}
	}
	return scheduled, nil
}
