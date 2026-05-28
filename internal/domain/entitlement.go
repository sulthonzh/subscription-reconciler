package domain

import (
	"errors"
	"time"
)

type Source string

const (
	SourceStore       Source = "STORE"
	SourceCarrier     Source = "CARRIER"
	SourceMarketplace Source = "MARKETPLACE"
	SourceNone        Source = "NONE"
)

type EventType string

const (
	EventInitialPurchase EventType = "INITIAL_PURCHASE"
	EventRenewal         EventType = "RENEWAL"
	EventCancellation    EventType = "CANCELLATION"
	EventBillingIssue    EventType = "BILLING_ISSUE"
	EventExpiration      EventType = "EXPIRATION"
	EventUnCancellation  EventType = "UN_CANCELLATION"
)

type Entitlement struct {
	UserID          string
	Source          Source
	Active          bool
	ExpiresAt       *time.Time
	Reason          string
	LastChangedAt   time.Time
	LastEventTimeMs int64
	CreatedAt       time.Time
}

type StoreEvent struct {
	EventID     string
	UserID      string
	Type        EventType
	EventTimeMs int64
	ProductID   string
}

var ErrInvalidEventType = errors.New("invalid event type")
var ErrProductNotFound = errors.New("unknown product ID")

// ApplyStoreEvent applies a store webhook event to a STORE-sourced entitlement.
// Returns the new entitlement state and whether a state change occurred.
// If entitlement is nil, creates a new one.
func ApplyStoreEvent(entitlement *Entitlement, event StoreEvent, now time.Time) (*Entitlement, bool, error) {
	expiresAt, err := GetExpiryTime(event.EventTimeMs, event.ProductID)
	if err != nil {
		return nil, false, err
	}

	reason := string(event.Type)

	if entitlement == nil {
		entitlement = &Entitlement{
			UserID:    event.UserID,
			Source:    SourceStore,
			CreatedAt: now,
		}
	}

	entitlement.LastEventTimeMs = event.EventTimeMs

	switch event.Type {
	case EventInitialPurchase:
		entitlement.Active = true
		entitlement.ExpiresAt = &expiresAt
		entitlement.Reason = reason
		entitlement.LastChangedAt = now
		return entitlement, true, nil

	case EventRenewal:
		changed := !entitlement.Active || entitlement.Reason != reason
		entitlement.Active = true
		entitlement.ExpiresAt = &expiresAt
		entitlement.Reason = reason
		entitlement.LastChangedAt = now
		return entitlement, changed, nil

	case EventUnCancellation:
		changed := !entitlement.Active
		entitlement.Active = true
		entitlement.ExpiresAt = &expiresAt
		entitlement.Reason = reason
		entitlement.LastChangedAt = now
		return entitlement, changed, nil

	case EventCancellation:
		// Access stays active until expires_at. Only update reason.
		changed := entitlement.Reason != reason
		entitlement.Reason = reason
		entitlement.LastChangedAt = now
		return entitlement, changed, nil

	case EventBillingIssue:
		// No state change. Informational only. Do not update LastChangedAt.
		changed := entitlement.Reason != reason
		entitlement.Reason = reason
		return entitlement, changed, nil

	case EventExpiration:
		changed := entitlement.Active
		entitlement.Active = false
		entitlement.Reason = reason
		entitlement.LastChangedAt = now
		return entitlement, changed, nil

	default:
		return nil, false, ErrInvalidEventType
	}
}

// sourcePriority defines the resolution order for active entitlements.
// Lower index = higher priority.
var sourcePriority = []Source{SourceStore, SourceMarketplace, SourceCarrier}

// ResolveEntitlements picks the canonical entitlement from multiple source rows.
// Priority: STORE > MARKETPLACE > CARRIER.
// If none active, returns an entitlement with SourceNone.
// If no rows, returns a zero-value entitlement with SourceNone.
func ResolveEntitlements(rows []Entitlement) Entitlement {
	if len(rows) == 0 {
		return Entitlement{Source: SourceNone}
	}

	// Build a set of active sources for priority lookup.
	activeBySource := make(map[Source]Entitlement, len(rows))
	for _, row := range rows {
		if row.Active {
			activeBySource[row.Source] = row
		}
	}

	// Pick highest-priority active source.
	for _, src := range sourcePriority {
		if ent, ok := activeBySource[src]; ok {
			return ent
		}
	}

	// None active — return the most recently changed row with NONE source.
	result := Entitlement{Source: SourceNone}
	result.LastChangedAt = rows[0].LastChangedAt
	result.Reason = rows[0].Reason
	for _, row := range rows[1:] {
		if row.LastChangedAt.After(result.LastChangedAt) {
			result.LastChangedAt = row.LastChangedAt
			result.Reason = row.Reason
		}
	}
	return result
}
