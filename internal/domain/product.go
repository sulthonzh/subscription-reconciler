package domain

import (
	"time"
)

var ProductDurations = map[string]time.Duration{
	"premium_monthly": 30 * 24 * time.Hour,
	"premium_yearly":  365 * 24 * time.Hour,
}

// GetExpiryTime computes the expiration time from an event's millisecond timestamp and product ID.
func GetExpiryTime(eventTimeMs int64, productID string) (time.Time, error) {
	duration, ok := ProductDurations[productID]
	if !ok {
		return time.Time{}, ErrProductNotFound
	}
	eventTime := time.UnixMilli(eventTimeMs)
	return eventTime.Add(duration), nil
}
