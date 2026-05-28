package domain

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var fixedNow = time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC)

func TestApplyStoreEvent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		entitlement  *Entitlement
		event        StoreEvent
		wantActive   bool
		wantReason   string
		wantChanged  bool
		wantErr      bool
		wantExpiresAt *time.Time
	}{
		{
			name:        "INITIAL_PURCHASE creates new entitlement",
			entitlement: nil,
			event: StoreEvent{
				EventID:     "evt_001",
				UserID:      "u_42",
				Type:        EventInitialPurchase,
				EventTimeMs: 1716700000000,
				ProductID:   "premium_monthly",
			},
			wantActive:  true,
			wantReason:  "INITIAL_PURCHASE",
			wantChanged: true,
			wantErr:     false,
		},
		{
			name: "RENEWAL extends existing entitlement",
			entitlement: &Entitlement{
				UserID:        "u_42",
				Source:        SourceStore,
				Active:        true,
				ExpiresAt:     ptrTime(time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)),
				Reason:        "INITIAL_PURCHASE",
				LastChangedAt: fixedNow.Add(-24 * time.Hour),
				CreatedAt:     fixedNow.Add(-48 * time.Hour),
			},
			event: StoreEvent{
				EventID:     "evt_002",
				UserID:      "u_42",
				Type:        EventRenewal,
				EventTimeMs: 1716700000000,
				ProductID:   "premium_monthly",
			},
			wantActive:  true,
			wantReason:  "RENEWAL",
			wantChanged: true,
			wantErr:     false,
		},
		{
			name: "RENEWAL from inactive reactivates",
			entitlement: &Entitlement{
				UserID:        "u_42",
				Source:        SourceStore,
				Active:        false,
				Reason:        "EXPIRATION",
				LastChangedAt: fixedNow.Add(-24 * time.Hour),
				CreatedAt:     fixedNow.Add(-48 * time.Hour),
			},
			event: StoreEvent{
				EventID:     "evt_003",
				UserID:      "u_42",
				Type:        EventRenewal,
				EventTimeMs: 1716700000000,
				ProductID:   "premium_yearly",
			},
			wantActive:  true,
			wantReason:  "RENEWAL",
			wantChanged: true,
			wantErr:     false,
		},
		{
			name: "CANCELLATION keeps active, updates reason",
			entitlement: &Entitlement{
				UserID:        "u_42",
				Source:        SourceStore,
				Active:        true,
				ExpiresAt:     ptrTime(time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)),
				Reason:        "RENEWAL",
				LastChangedAt: fixedNow.Add(-24 * time.Hour),
				CreatedAt:     fixedNow.Add(-48 * time.Hour),
			},
			event: StoreEvent{
				EventID:     "evt_004",
				UserID:      "u_42",
				Type:        EventCancellation,
				EventTimeMs: 1716700000000,
				ProductID:   "premium_monthly",
			},
			wantActive:  true,
			wantReason:  "CANCELLATION",
			wantChanged: true,
			wantErr:     false,
		},
		{
			name: "CANCELLATION no change if reason already CANCELLATION",
			entitlement: &Entitlement{
				UserID:        "u_42",
				Source:        SourceStore,
				Active:        true,
				ExpiresAt:     ptrTime(time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)),
				Reason:        "CANCELLATION",
				LastChangedAt: fixedNow.Add(-24 * time.Hour),
				CreatedAt:     fixedNow.Add(-48 * time.Hour),
			},
			event: StoreEvent{
				EventID:     "evt_004b",
				UserID:      "u_42",
				Type:        EventCancellation,
				EventTimeMs: 1716700000000,
				ProductID:   "premium_monthly",
			},
			wantActive:  true,
			wantReason:  "CANCELLATION",
			wantChanged: false,
			wantErr:     false,
		},
		{
			name: "BILLING_ISSUE no state change, preserves LastChangedAt",
			entitlement: &Entitlement{
				UserID:        "u_42",
				Source:        SourceStore,
				Active:        true,
				ExpiresAt:     ptrTime(time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)),
				Reason:        "RENEWAL",
				LastChangedAt: fixedNow.Add(-24 * time.Hour),
				CreatedAt:     fixedNow.Add(-48 * time.Hour),
			},
			event: StoreEvent{
				EventID:     "evt_005",
				UserID:      "u_42",
				Type:        EventBillingIssue,
				EventTimeMs: 1716700000000,
				ProductID:   "premium_monthly",
			},
			wantActive:  true,
			wantReason:  "BILLING_ISSUE",
			wantChanged: true,
			wantErr:     false,
		},
		{
			name: "BILLING_ISSUE no change if reason already BILLING_ISSUE",
			entitlement: &Entitlement{
				UserID:        "u_42",
				Source:        SourceStore,
				Active:        true,
				ExpiresAt:     ptrTime(time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)),
				Reason:        "BILLING_ISSUE",
				LastChangedAt: fixedNow.Add(-24 * time.Hour),
				CreatedAt:     fixedNow.Add(-48 * time.Hour),
			},
			event: StoreEvent{
				EventID:     "evt_005b",
				UserID:      "u_42",
				Type:        EventBillingIssue,
				EventTimeMs: 1716700000000,
				ProductID:   "premium_monthly",
			},
			wantActive:  true,
			wantReason:  "BILLING_ISSUE",
			wantChanged: false,
			wantErr:     false,
		},
		{
			name: "EXPIRATION deactivates",
			entitlement: &Entitlement{
				UserID:        "u_42",
				Source:        SourceStore,
				Active:        true,
				ExpiresAt:     ptrTime(time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)),
				Reason:        "RENEWAL",
				LastChangedAt: fixedNow.Add(-24 * time.Hour),
				CreatedAt:     fixedNow.Add(-48 * time.Hour),
			},
			event: StoreEvent{
				EventID:     "evt_006",
				UserID:      "u_42",
				Type:        EventExpiration,
				EventTimeMs: 1716700000000,
				ProductID:   "premium_monthly",
			},
			wantActive:  false,
			wantReason:  "EXPIRATION",
			wantChanged: true,
			wantErr:     false,
		},
		{
			name: "EXPIRATION no change if already inactive",
			entitlement: &Entitlement{
				UserID:        "u_42",
				Source:        SourceStore,
				Active:        false,
				Reason:        "EXPIRATION",
				LastChangedAt: fixedNow.Add(-24 * time.Hour),
				CreatedAt:     fixedNow.Add(-48 * time.Hour),
			},
			event: StoreEvent{
				EventID:     "evt_006b",
				UserID:      "u_42",
				Type:        EventExpiration,
				EventTimeMs: 1716700000000,
				ProductID:   "premium_monthly",
			},
			wantActive:  false,
			wantReason:  "EXPIRATION",
			wantChanged: false,
			wantErr:     false,
		},
		{
			name: "UN_CANCELLATION reactivates",
			entitlement: &Entitlement{
				UserID:        "u_42",
				Source:        SourceStore,
				Active:        false,
				Reason:        "CANCELLATION",
				LastChangedAt: fixedNow.Add(-24 * time.Hour),
				CreatedAt:     fixedNow.Add(-48 * time.Hour),
			},
			event: StoreEvent{
				EventID:     "evt_007",
				UserID:      "u_42",
				Type:        EventUnCancellation,
				EventTimeMs: 1716700000000,
				ProductID:   "premium_monthly",
			},
			wantActive:  true,
			wantReason:  "UN_CANCELLATION",
			wantChanged: true,
			wantErr:     false,
			wantExpiresAt: ptrTime(time.UnixMilli(1716700000000).Add(30 * 24 * time.Hour)),
		},
		{
			name: "UN_CANCELLATION no change if already active",
			entitlement: &Entitlement{
				UserID:        "u_42",
				Source:        SourceStore,
				Active:        true,
				ExpiresAt:     ptrTime(time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)),
				Reason:        "RENEWAL",
				LastChangedAt: fixedNow.Add(-24 * time.Hour),
				CreatedAt:     fixedNow.Add(-48 * time.Hour),
			},
			event: StoreEvent{
				EventID:     "evt_007b",
				UserID:      "u_42",
				Type:        EventUnCancellation,
				EventTimeMs: 1716700000000,
				ProductID:   "premium_monthly",
			},
			wantActive:  true,
			wantReason:  "UN_CANCELLATION",
			wantChanged: false,
			wantErr:     false,
		},
		{
			name:        "invalid event type returns error",
			entitlement: nil,
			event: StoreEvent{
				EventID:     "evt_bad",
				UserID:      "u_42",
				Type:        EventType("UNKNOWN"),
				EventTimeMs: 1716700000000,
				ProductID:   "premium_monthly",
			},
			wantActive:  false,
			wantReason:  "",
			wantChanged: false,
			wantErr:     true,
		},
		{
			name: "UN_CANCELLATION creates new entitlement",
			entitlement: nil,
			event: StoreEvent{
				EventID:     "evt_008",
				UserID:      "u_42",
				Type:        EventUnCancellation,
				EventTimeMs: 1716700000000,
				ProductID:   "premium_yearly",
			},
			wantActive:  true,
			wantReason:  "UN_CANCELLATION",
			wantChanged: true,
			wantErr:     false,
			wantExpiresAt: ptrTime(time.UnixMilli(1716700000000).Add(365 * 24 * time.Hour)),
		},
		{
			name:        "unknown product returns error",
			entitlement: nil,
			event: StoreEvent{
				EventID:     "evt_noproduct",
				UserID:      "u_42",
				Type:        EventInitialPurchase,
				EventTimeMs: 1716700000000,
				ProductID:   "unknown_product",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result, changed, err := ApplyStoreEvent(tt.entitlement, tt.event, fixedNow)

			if tt.wantErr {
				require.Error(t, err)
				assert.Nil(t, result)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, result)
			assert.Equal(t, tt.wantActive, result.Active)
			assert.Equal(t, tt.wantReason, result.Reason)
			assert.Equal(t, tt.wantChanged, changed)
			assert.Equal(t, tt.event.UserID, result.UserID)
			assert.Equal(t, SourceStore, result.Source)
			assert.Equal(t, tt.event.EventTimeMs, result.LastEventTimeMs, "LastEventTimeMs should match event EventTimeMs")
			
			if tt.wantExpiresAt != nil {
				require.NotNil(t, result.ExpiresAt, "ExpiresAt should not be nil for this test case")
				assert.Equal(t, *tt.wantExpiresAt, *result.ExpiresAt, "ExpiresAt should be computed from product duration")
			} else if tt.event.Type == EventInitialPurchase || tt.event.Type == EventRenewal || tt.event.Type == EventUnCancellation {
				// These events should always set ExpiresAt
				require.NotNil(t, result.ExpiresAt, "ExpiresAt should be set for %s events", tt.event.Type)
			}

			if tt.event.Type == EventBillingIssue && tt.entitlement != nil {
				assert.Equal(t, tt.entitlement.LastChangedAt, result.LastChangedAt,
					"BILLING_ISSUE should not change LastChangedAt")
			} else {
				assert.Equal(t, fixedNow, result.LastChangedAt)
			}
		})
	}
}

func TestApplyStoreEvent_SetsExpiryFromProduct(t *testing.T) {
	t.Parallel()

	event := StoreEvent{
		EventID:     "evt_expiry_check",
		UserID:      "u_42",
		Type:        EventInitialPurchase,
		EventTimeMs: 1716700000000,
		ProductID:   "premium_monthly",
	}

	result, _, err := ApplyStoreEvent(nil, event, fixedNow)
	require.NoError(t, err)
	require.NotNil(t, result.ExpiresAt)

	expectedExpiry := time.UnixMilli(1716700000000).Add(30 * 24 * time.Hour)
	assert.Equal(t, expectedExpiry, *result.ExpiresAt)
}

func TestResolveEntitlements(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		rows       []Entitlement
		wantSource Source
		wantActive bool
	}{
		{
			name:       "multiple active picks STORE",
			rows:       []Entitlement{
				{UserID: "u_1", Source: SourceCarrier, Active: true, LastChangedAt: fixedNow},
				{UserID: "u_1", Source: SourceStore, Active: true, LastChangedAt: fixedNow},
				{UserID: "u_1", Source: SourceMarketplace, Active: true, LastChangedAt: fixedNow},
			},
			wantSource: SourceStore,
			wantActive: true,
		},
		{
			name:       "carrier only picks CARRIER",
			rows:       []Entitlement{
				{UserID: "u_1", Source: SourceCarrier, Active: true, LastChangedAt: fixedNow},
			},
			wantSource: SourceCarrier,
			wantActive: true,
		},
		{
			name:       "store and marketplace active picks STORE",
			rows:       []Entitlement{
				{UserID: "u_1", Source: SourceMarketplace, Active: true, LastChangedAt: fixedNow},
				{UserID: "u_1", Source: SourceStore, Active: true, LastChangedAt: fixedNow},
			},
			wantSource: SourceStore,
			wantActive: true,
		},
		{
			name:       "none active returns NONE",
			rows:       []Entitlement{
				{UserID: "u_1", Source: SourceStore, Active: false, Reason: "EXPIRATION", LastChangedAt: fixedNow.Add(-1 * time.Hour)},
				{UserID: "u_1", Source: SourceCarrier, Active: false, Reason: "CARRIER_INACTIVE", LastChangedAt: fixedNow},
			},
			wantSource: SourceNone,
			wantActive: false,
		},
		{
			name:       "empty returns NONE",
			rows:       []Entitlement{},
			wantSource: SourceNone,
			wantActive: false,
		},
		{
			name:       "marketplace active, store inactive picks MARKETPLACE",
			rows:       []Entitlement{
				{UserID: "u_1", Source: SourceStore, Active: false, LastChangedAt: fixedNow},
				{UserID: "u_1", Source: SourceMarketplace, Active: true, LastChangedAt: fixedNow},
			},
			wantSource: SourceMarketplace,
			wantActive: true,
		},
		{
			name:       "none active picks most recently changed reason",
			rows:       []Entitlement{
				{UserID: "u_1", Source: SourceStore, Active: false, Reason: "EXPIRATION", LastChangedAt: fixedNow.Add(-2 * time.Hour)},
				{UserID: "u_1", Source: SourceCarrier, Active: false, Reason: "CARRIER_INACTIVE", LastChangedAt: fixedNow},
			},
			wantSource: SourceNone,
			wantActive: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := ResolveEntitlements(tt.rows)
			assert.Equal(t, tt.wantSource, result.Source)
			assert.Equal(t, tt.wantActive, result.Active)
		})
	}
}

func TestResolveEntitlements_InactivePicksMostRecent(t *testing.T) {
	t.Parallel()

	rows := []Entitlement{
		{UserID: "u_1", Source: SourceStore, Active: false, Reason: "EXPIRATION", LastChangedAt: fixedNow.Add(-2 * time.Hour)},
		{UserID: "u_1", Source: SourceCarrier, Active: false, Reason: "CARRIER_INACTIVE", LastChangedAt: fixedNow},
	}

	result := ResolveEntitlements(rows)
	assert.Equal(t, SourceNone, result.Source)
	assert.Equal(t, "CARRIER_INACTIVE", result.Reason)
	assert.Equal(t, fixedNow, result.LastChangedAt)
}

func ptrTime(t time.Time) *time.Time {
	return &t
}
