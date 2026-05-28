package service

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"github.com/sulthonzh/subscription-reconciler/internal/domain"
	"github.com/sulthonzh/subscription-reconciler/internal/port"
)

type Poller struct {
	entRepo   port.EntitlementRepository
	pollRepo  port.CarrierPollLogRepository
	carrier   port.CarrierClient
	auditRepo port.AuditLogRepository
	logger    *slog.Logger
}

func NewPoller(
	entRepo port.EntitlementRepository,
	pollRepo port.CarrierPollLogRepository,
	carrier port.CarrierClient,
	auditRepo port.AuditLogRepository,
	logger *slog.Logger,
) *Poller {
	return &Poller{
		entRepo:   entRepo,
		pollRepo:  pollRepo,
		carrier:   carrier,
		auditRepo: auditRepo,
		logger:    logger,
	}
}

func (p *Poller) Run(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			p.logger.Info("poller shutting down")
			return
		case <-ticker.C:
			p.PollAll(ctx)
		}
	}
}

func (p *Poller) PollAll(ctx context.Context) {
	users, err := p.entRepo.GetActiveBySource(ctx, domain.SourceCarrier)
	if err != nil {
		p.logger.Error("failed to get active carrier users",
			slog.String("error", err.Error()),
		)
		return
	}

	if len(users) == 0 {
		return
	}

	sem := make(chan struct{}, 10)
	var wg sync.WaitGroup

	for _, user := range users {
		wg.Add(1)
		go func(userID string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			p.pollUser(ctx, userID)
		}(user.UserID)
	}

	wg.Wait()
}

func (p *Poller) pollUser(ctx context.Context, userID string) {
	acquired, err := p.pollRepo.AcquireLock(ctx, userID, time.Now().Add(2*time.Minute))
	if err != nil {
		p.logger.Error("failed to acquire lock",
			slog.String("user_id", userID),
			slog.String("error", err.Error()),
		)
		return
	}
	if !acquired {
		p.logger.Debug("skipping locked user",
			slog.String("user_id", userID),
		)
		return
	}

	defer func() {
		if err := p.pollRepo.ReleaseLock(ctx, userID); err != nil {
			p.logger.Error("failed to release lock",
				slog.String("user_id", userID),
				slog.String("error", err.Error()),
			)
		}
	}()

	status, err := p.carrier.CheckPlan(ctx, userID)
	if err != nil {
		p.logger.Error("carrier check failed",
			slog.String("user_id", userID),
			slog.String("error", err.Error()),
		)
		status = "api_error"
	}

	if err := p.pollRepo.Insert(ctx, userID, status); err != nil {
		p.logger.Error("failed to log poll result",
			slog.String("user_id", userID),
			slog.String("error", err.Error()),
		)
	}

	var previousState string
	if ent, err := p.entRepo.GetByUserAndSource(ctx, userID, domain.SourceCarrier); err == nil && ent != nil {
		stateJSON, _ := json.Marshal(map[string]interface{}{
			"active":  ent.Active,
			"source": "CARRIER",
		})
		previousState = string(stateJSON)
	} else {
		previousState = "{}"
	}

	switch status {
	case "inactive":
		if err := p.entRepo.UpdateActive(ctx, userID, domain.SourceCarrier, false, "CARRIER_INACTIVE"); err != nil {
			p.logger.Error("failed to deactivate carrier entitlement",
				slog.String("user_id", userID),
				slog.String("error", err.Error()),
			)
		} else if p.auditRepo != nil {
			entry := domain.AuditEntry{
				UserID:        userID,
				TriggerID:     "carrier_poll",
				Source:        domain.SourceCarrier,
				PreviousState: previousState,
				NextState:     `{"active":false,"source":"CARRIER","reason":"CARRIER_INACTIVE"}`,
				CreatedAt:     time.Now(),
			}
			if err := p.auditRepo.Insert(ctx, entry); err != nil {
				p.logger.Error("failed to write audit entry",
					slog.String("user_id", userID),
					slog.String("error", err.Error()),
				)
			}
		}
	case "api_error":
		p.logger.Warn("carrier api error, no state change",
			slog.String("user_id", userID),
		)
	}
}
