# Subscription Reconciler

Premium entitlement reconciler for multi-channel subscription management. Ingests signals from in-app store (webhooks), mobile carrier (polling), and third-party marketplace (bulk revoke) to maintain canonical premium access state per user.

![Go Version](https://img.shields.io/badge/Go-1.26-00ADD8?logo=go)
![Coverage](https://img.shields.io/badge/coverage-85.5%25-green)
![License](https://img.shields.io/badge/license-MIT-blue)
![Docker](https://img.shields.io/badge/docker-ready-2496ED?logo=docker)
![SQLite](https://img.shields.io/badge/SQLite-pure%20Go-003B57?logo=sqlite)
![Chi](https://img.shields.io/badge/chi-v5.3-00ADD8)

## Table of Contents

- [Overview](#overview)
- [Features](#features)
- [Quick Start](#quick-start)
- [Architecture](#architecture)
- [API Reference](#api-reference)
- [Event Processing](#event-processing)
- [Background Workers](#background-workers)
- [Middleware](#middleware)
- [Configuration](#configuration)
- [Testing](#testing)
- [Deployment](#deployment)
- [Tech Stack](#tech-stack)
- [Project Stats](#project-stats)

## Overview

Subscription Reconciler solves the complex problem of managing premium entitlements across multiple channels. Modern subscription services receive purchase signals from various sources: in-app store webhooks, mobile carrier billing, and third-party marketplaces. Each source operates independently, leading to potential inconsistencies in user access status.

This system maintains a canonical entitlement state per user by ingesting signals from all channels and applying a deterministic resolution priority. It handles state transitions through a robust event processing system, manages background cleanup of expired entitlements, and provides a reliable API for checking user premium status.

The system is built with a hexagonal architecture using ports and adapters, ensuring business logic remains independent from external concerns like databases and HTTP transports.

## Features

- Multi-channel subscription reconciliation with STORE > MARKETPLACE > CARRIER priority
- Event-driven state machine for handling purchase, renewal, cancellation, and expiry events
- Background workers for carrier polling, expiry cleanup, and notifications
- SQLite persistence with WAL mode and inline schema migrations
- RESTful API with comprehensive error handling and validation
- Middleware stack including rate limiting, CORS, and logging
- Docker support with multi-stage builds
- Graceful shutdown for all background workers

## Quick Start

**Prerequisites:**
- Go 1.26 or later
- Docker and Docker Compose (for containerized deployment)

**Docker (recommended):**
```bash
docker compose up --build
```

**Local development:**
```bash
go run ./cmd/server
```

**Development commands:**
```bash
make build          # Compile binary
make test           # Run tests with race detector and coverage
make run            # Run locally
make docker         # Build and run with Docker Compose
make clean          # Remove build artifacts and DB files
```

The server starts on `:8080` with mock carrier on `:8081`.

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

## API Reference

### Health Check

```
GET /health
```

**Response:**
```json
{
  "status": "ok"
}
```

**Status Codes:**
- 200 OK - Health check passed

**Example:**
```bash
curl -X GET http://localhost:8080/health
```

### Store Webhook

```
POST /webhooks/store
Content-Type: application/json
```

**Request Body:**
```json
{
  "eventId": "evt_abc123",
  "userId": "u_42",
  "type": "INITIAL_PURCHASE",
  "eventTimeMs": 1716700000000,
  "productId": "premium_monthly"
}
```

**Response:**
```json
{
  "status": "processed"
}
```

**Status Codes:**
- 200 OK - Event processed successfully
- 200 OK - Event ignored (duplicate or stale)
- 400 Bad Request - Validation error

**Example:**
```bash
curl -X POST http://localhost:8080/webhooks/store \
  -H "Content-Type: application/json" \
  -d '{
    "eventId": "evt_abc123",
    "userId": "u_42",
    "type": "INITIAL_PURCHASE",
    "eventTimeMs": 1716700000000,
    "productId": "premium_monthly"
  }'
```

### Marketplace Bulk Revoke

```
POST /webhooks/marketplace/revoke
Content-Type: application/json
```

**Request Body:**
```json
{
  "userIds": ["u_42", "u_91"]
}
```

**Response:**
```json
{
  "revoked": 2,
  "skipped": 0
}
```

**Status Codes:**
- 200 OK - Revocation processed successfully
- 400 Bad Request - Invalid request body

**Example:**
```bash
curl -X POST http://localhost:8080/webhooks/marketplace/revoke \
  -H "Content-Type: application/json" \
  -d '{
    "userIds": ["u_42", "u_91"]
  }'
```

### Get User Entitlement

```
GET /users/{id}/entitlement
```

**Response:**
```json
{
  "active": true,
  "source": "STORE",
  "expiresAt": "2024-06-15T10:00:00Z",
  "lastChangedAt": "2024-05-16T10:00:00Z",
  "reason": "RENEWAL"
}
```

**Status Codes:**
- 200 OK - Returns resolved entitlement (defaults to `{active: false, source: "NONE"}` if user has no records)
- 500 Internal Server Error - Database error

**Example:**
```bash
curl -X GET http://localhost:8080/users/u_42/entitlement
```

### Get User Timeline

```
GET /users/{id}/timeline
```

**Response:**
```json
[
  {
    "triggerId": "evt_abc123",
    "source": "STORE",
    "previousState": "",
    "nextState": "{\"active\":true,\"source\":\"STORE\",\"reason\":\"INITIAL_PURCHASE\"}",
    "createdAt": "2024-05-16T10:00:00Z"
  }
]
```

Returns an empty array `[]` if the user has no audit history.

**Status Codes:**
- 200 OK - Timeline retrieved (may be empty array)

**Example:**
```bash
curl -X GET http://localhost:8080/users/u_42/timeline
```

## Event Processing

The system processes events through a deterministic state machine. Each event type triggers specific state changes:

1. **Initial Purchase**: Creates new active entitlement with calculated expiry
2. **Renewal**: Extends existing entitlement period without resetting priority
3. **Cancellation**: Marks entitlement as cancelled while keeping access until expiry
4. **Billing Issue**: No state change - informational only
5. **Expiration**: Deactivates expired entitlements
6. **Un-cancellation**: Reactivates cancelled entitlements with new expiry

Entitlement resolution follows a strict priority when multiple sources exist for the same user: STORE > MARKETPLACE > CARRIER > NONE.

## Background Workers

| Worker | Interval | Purpose | Concurrency |
|---|---|---|---|
| Carrier Poller | 5 min | Polls carrier API for active users | 10 concurrent requests |
| Expiry Sweeper | 5 min | Safely expires overdue entitlements | Single goroutine |
| Notifier | 1 min | Sends due expiry notifications | 5 concurrent jobs |
| Notification Scheduler | 5 min | Schedules new expiry notifications | Single goroutine |

All workers respect `SIGINT` and `SIGTERM` for graceful shutdown, completing in-flight operations before termination.

## Middleware

The HTTP server processes requests through the following middleware chain:

1. **RequestID** - Generates unique request IDs for tracing
2. **RealIP** - Extracts real client IP from proxy headers
3. **RateLimiter** - Limits requests per IP (100/minute)
4. **BodySizeLimit** - Limits request body size (1MB max)
5. **CORS** - Handles cross-origin requests
6. **RequestLogger** - Logs HTTP requests in JSON format
7. **Recoverer** - Recovers from panics and returns 500 errors

## Configuration

| Environment Variable | Default | Type | Description |
|---|---|---|---|
| `PORT` | `8080` | string | HTTP server port |
| `DB_PATH` | `entitlements.db` | string | SQLite database path |
| `CARRIER_URL` | `http://localhost:8081` | string | Carrier API base URL |

## Testing

Run tests with comprehensive coverage:

```bash
go test ./... -v -race -coverprofile=coverage.out
go tool cover -html=coverage.out
```

Test categories:
- **Unit tests**: Isolated business logic testing
- **Integration tests**: Database and HTTP handler testing

**Package Coverage:**
![domain](https://img.shields.io/badge/domain-100.0%25-brightgreen)
![httphandler](https://img.shields.io/badge/httphandler-92.1%25-green)
![service](https://img.shields.io/badge/service-87.8%25-yellow-green)
![carrierhttp](https://img.shields.io/badge/carrierhttp-88.2%25-yellow-green)
![sqlite](https://img.shields.io/badge/sqlite-83.2%25-yellow)
![middleware](https://img.shields.io/badge/middleware-63.1%25-orange)
![Overall](https://img.shields.io/badge/overall-85.5%25-yellow-green)

## Deployment

**Docker Compose (recommended):**
```bash
docker compose up --build
```

The server starts on `:8080` and the mock carrier on `:8081`. Both containers use multi-stage Alpine builds with CGO disabled.

**Health Check:**
```bash
curl http://localhost:8080/health
```

**Graceful Shutdown:**
The server listens for `SIGINT`/`SIGTERM` signals. On receipt:
1. Stops accepting new HTTP connections
2. Waits up to 10 seconds for in-flight requests to complete
3. Cancels all background worker contexts
4. Closes the database connection

## Tech Stack

| Component | Library | Version | Why Chosen |
|---|---|---|---|
| Runtime | Go | 1.26.2 | Performance, type safety, strong standard library |
| HTTP Router | Chi | v5.3.0 | Lightweight, idiomatic, middleware support |
| Database | SQLite | modernc.org v1.50.1 | File-based, no external dependencies, pure Go driver |
| HTTP Client | net/http | built-in | Standard library reliability |
| Testing | testify | v1.11.1 | Rich assertions, mock support |
| Data Validation | built-in encoding/json | built-in | Type-safe, standard approach |

## Project Stats

- **Total Lines of Code**: 5,984
- **Go Files**: 48
- **Packages**: 9 (domain, port, adapter/sqlite, adapter/carrierhttp, adapter/httphandler, service, middleware, cmd/server, cmd/mockcarrier)
- **Test Packages**: 7 (all internal packages + tests/integration)
- **Test Coverage**: 85.5%
- **Docker Support**: Multi-stage Alpine builds