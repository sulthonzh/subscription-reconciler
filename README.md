# Subscription Reconciler

Premium entitlement reconciler for multi-channel subscription management. Ingests signals from in-app store (webhooks), mobile carrier (polling), and third-party marketplace (bulk revoke) to maintain canonical premium access state per user.

## Quick Start

```bash
# Run with Docker (includes mock carrier)
docker compose up --build

# Or locally
go run ./cmd/server
```

The server starts on `:8080`, mock carrier on `:8081`.

## Architecture

```
cmd/
  server/          # HTTP server entry point
  mockcarrier/     # Mock carrier API (85% active / 10% inactive / 5% error)
internal/
  domain/          # Pure business logic (state machine, resolution, notifications)
  port/            # Interfaces (repositories, carrier client)
  adapter/
    sqlite/        # SQLite repository implementations
    carrierhttp/   # Carrier HTTP client
    httphandler/   # Chi HTTP handlers
  service/         # Application services (reconciler, poller, notifier)
  middleware/       # HTTP middleware
migrations/        # SQLite schema migrations
```

### Data Flow

1. **Store webhooks** → `POST /webhooks/store` → dedup via `event_id` PK → stale check via `last_event_time_ms` → state machine → upsert entitlement → schedule expiry notification
2. **Carrier polling** → every 5 min, concurrent goroutines (semaphore=10) with DB-level locking (`locked_until`) → deactivate on "inactive" status
3. **Marketplace revoke** → `POST /webhooks/marketplace/revoke` → deactivate only MARKETPLACE-sourced rows
4. **Expiry sweeper** → safety-net goroutine every 5 min → expires `active=true AND expires_at < now`
5. **Notification sender** → every 1 min → picks up due notifications → marks sent

### Entitlement Resolution Priority

When multiple sources exist for a user: **STORE > MARKETPLACE > CARRIER > NONE**

### Event State Machine

| Event | Effect |
|---|---|
| `INITIAL_PURCHASE` | Activate with expiry from product duration |
| `RENEWAL` | Extend expiry from product duration |
| `CANCELLATION` | Keep active until expiry, reason=CANCELLED |
| `BILLING_ISSUE` | Informational only, no access change |
| `EXPIRATION` | Deactivate, reason=EXPIRED |
| `UN_CANCELLATION` | Re-activate with new expiry |

### Product Durations

| Product | Duration |
|---|---|
| `premium_monthly` | 30 days |
| `premium_yearly` | 365 days |

## API

### Health Check

```
GET /health
→ 200 {"status":"ok"}
```

### Store Webhook

```
POST /webhooks/store
Content-Type: application/json

{
  "eventId": "evt_abc123",
  "userId": "u_42",
  "type": "INITIAL_PURCHASE",
  "eventTimeMs": 1716700000000,
  "productId": "premium_monthly"
}

→ 200 {"status":"processed"}
→ 200 {"status":"ignored"}    // duplicate or stale
→ 400 {"error":"..."}         // validation error
```

### Marketplace Bulk Revoke

```
POST /webhooks/marketplace/revoke
Content-Type: application/json

{
  "userIds": ["u_42", "u_91"]
}

→ 200 {"revoked":2, "skipped":0}
→ 400 {"error":"..."}
```

### Get Entitlement

```
GET /users/{id}/entitlement

→ 200 {
  "active": true,
  "source": "STORE",
  "expiresAt": "2024-06-15T10:00:00Z",
  "lastChangedAt": "2024-05-16T10:00:00Z",
  "reason": "RENEWAL"
}
```

## Configuration

| Env Var | Default | Description |
|---|---|---|
| `PORT` | `8080` | HTTP server port |
| `DB_PATH` | `entitlements.db` | SQLite database path |
| `CARRIER_URL` | `http://localhost:8081` | Carrier API base URL |

## Background Workers

| Worker | Interval | Description |
|---|---|---|
| Carrier Poller | 5 min | Polls carrier API for active CARRIER-sourced users |
| Expiry Sweeper | 5 min | Expires overdue entitlements past `expires_at` |
| Notifier | 1 min | Sends due expiry notifications |

All workers respect `SIGINT`/`SIGTERM` for graceful shutdown.

## Development

```bash
make build          # Compile binary
make test           # Run tests with race detector and coverage
make run            # Run locally
make docker         # Build and run with Docker Compose
make clean          # Remove build artifacts and DB files
```

## Test Coverage

| Package | Coverage |
|---|---|
| domain | 100% |
| service | 99% |
| httphandler | 97% |
| carrierhttp | 88% |
| sqlite | 90% |

## Tech Stack

- **Go 1.26** with strict mode
- **Chi v5** HTTP router
- **SQLite** with WAL mode (pure-Go driver: `modernc.org/sqlite`)
- **inline SQL migrations**
- **Docker** multi-stage builds (Alpine)
