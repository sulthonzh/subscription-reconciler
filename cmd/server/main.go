package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	mw "github.com/go-chi/chi/v5/middleware"
	_ "modernc.org/sqlite"

	"github.com/sulthonzh/subscription-reconciler/internal/adapter/carrierhttp"
	"github.com/sulthonzh/subscription-reconciler/internal/adapter/httphandler"
	"github.com/sulthonzh/subscription-reconciler/internal/adapter/sqlite"
	appMiddleware "github.com/sulthonzh/subscription-reconciler/internal/middleware"
	"github.com/sulthonzh/subscription-reconciler/internal/service"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	port := envOrDefault("PORT", "8080")
	dbPath := envOrDefault("DB_PATH", "entitlements.db")
	carrierURL := envOrDefault("CARRIER_URL", "http://localhost:8081")

	logger.Info("starting server",
		slog.String("port", port),
		slog.String("db_path", dbPath),
		slog.String("carrier_url", carrierURL),
	)

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		logger.Error("failed to open database", slog.String("error", err.Error()))
		os.Exit(1)
	}
	defer db.Close()

	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	if err := runMigrations(db, logger); err != nil {
		logger.Error("failed to run migrations", slog.String("error", err.Error()))
		os.Exit(1)
	}

	entRepo := sqlite.NewEntitlementRepo(db)
	eventRepo := sqlite.NewStoreEventRepo(db)
	notifRepo := sqlite.NewNotificationRepo(db)
	pollRepo := sqlite.NewCarrierPollLogRepo(db)
	auditRepo := sqlite.NewAuditLogRepo(db)
	txProvider := sqlite.NewTxProvider(db)

	carrierClient := carrierhttp.NewClient(carrierURL)

	reconciler := service.NewReconciler(entRepo, eventRepo, notifRepo, auditRepo, txProvider, logger)
	poller := service.NewPoller(entRepo, pollRepo, carrierClient, auditRepo, logger)
	notifier := service.NewNotifier(entRepo, notifRepo, logger)

	handler := httphandler.New(reconciler)

	r := chi.NewRouter()
	r.Use(mw.RequestID)
	r.Use(mw.RealIP)
	r.Use(appMiddleware.RateLimiter())
	r.Use(appMiddleware.BodySizeLimit)
	r.Use(appMiddleware.CORS)
	r.Use(appMiddleware.RequestLogger(logger))
	r.Use(mw.Recoverer)

	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})

	r.Mount("/", handler.Routes())

	srv := &http.Server{
		Addr:         ":" + port,
		Handler:      r,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go poller.Run(ctx, 5*time.Minute)
	go notifier.Run(ctx, 1*time.Minute)

	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				count, err := reconciler.ExpireOverdue(ctx)
				if err != nil {
					logger.Error("expiry sweeper error", slog.String("error", err.Error()))
				} else if count > 0 {
					logger.Info("expired overdue entitlements", slog.Int("count", count))
				}
			}
		}
	}()

	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				count, err := notifier.ScheduleForExpiring(ctx)
				if err != nil {
					logger.Error("proactive notification scheduler error", slog.String("error", err.Error()))
				} else if count > 0 {
					logger.Info("scheduled expiring notifications", slog.Int("count", count))
				}
			}
		}
	}()

	go func() {
		logger.Info("server listening", slog.String("addr", srv.Addr))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("server error", slog.String("error", err.Error()))
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	logger.Info("shutdown signal received")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("server shutdown error", slog.String("error", err.Error()))
	}

	logger.Info("server stopped")
}

func runMigrations(db *sql.DB, logger *slog.Logger) error {
	schema := `
CREATE TABLE IF NOT EXISTS entitlements (
    user_id       TEXT NOT NULL,
    source        TEXT NOT NULL,
    active        BOOLEAN NOT NULL DEFAULT FALSE,
    expires_at    TEXT,
    reason        TEXT,
    last_changed_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    last_event_time_ms INTEGER NOT NULL DEFAULT 0,
    created_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    PRIMARY KEY (user_id, source)
);

CREATE TABLE IF NOT EXISTS store_events (
    event_id      TEXT PRIMARY KEY,
    user_id       TEXT    NOT NULL,
    type          TEXT    NOT NULL,
    event_time_ms INTEGER NOT NULL,
    product_id    TEXT    NOT NULL,
    processed_at  TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

CREATE TABLE IF NOT EXISTS carrier_poll_log (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id       TEXT    NOT NULL,
    polled_at     TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    status        TEXT    NOT NULL,
    locked_until  TEXT
);

CREATE TABLE IF NOT EXISTS notifications (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id       TEXT NOT NULL,
    type          TEXT NOT NULL DEFAULT 'PREMIUM_EXPIRES_SOON',
    scheduled_for TEXT NOT NULL,
    sent_at       TEXT,
    created_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    UNIQUE(user_id, type, scheduled_for)
);

CREATE TABLE IF NOT EXISTS audit_log (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id       TEXT NOT NULL,
    trigger_id    TEXT,
    source        TEXT NOT NULL,
    previous_state TEXT NOT NULL,
    next_state    TEXT NOT NULL,
    created_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
`
	if _, err := db.Exec(schema); err != nil {
		return fmt.Errorf("running schema: %w", err)
	}

	// Idempotent column additions for existing databases.
	db.Exec("ALTER TABLE entitlements ADD COLUMN last_event_time_ms INTEGER NOT NULL DEFAULT 0")

	logger.Info("migrations applied")
	return nil
}

func envOrDefault(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}
