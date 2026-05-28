package port

import "context"

type CarrierClient interface {
	CheckPlan(ctx context.Context, userID string) (string, error)
}
