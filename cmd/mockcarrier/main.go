package main

import (
	"encoding/json"
	"log/slog"
	"math/rand"
	"net/http"
	"os"
	"time"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	port := "8081"
	if p := os.Getenv("PORT"); p != "" {
		port = p
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/mock/carrier/plan", func(w http.ResponseWriter, r *http.Request) {
		userID := r.URL.Query().Get("userId")
		if userID == "" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "userId is required"})
			return
		}

		roll := rand.Float64()
		var status string
		var httpStatus int

		switch {
		case roll < 0.85:
			status = "active"
			httpStatus = http.StatusOK
		case roll < 0.95:
			status = "inactive"
			httpStatus = http.StatusOK
		default:
			status = "api_error"
			httpStatus = http.StatusServiceUnavailable
		}

		logger.Info("carrier plan check",
			slog.String("userId", userID),
			slog.String("status", status),
		)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(httpStatus)
		json.NewEncoder(w).Encode(map[string]string{"status": status})
	})

	srv := &http.Server{
		Addr:         ":" + port,
		Handler:      mux,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
	}

	logger.Info("mock carrier server listening", slog.String("port", port))
	if err := srv.ListenAndServe(); err != nil {
		logger.Error("mock carrier server error", slog.String("error", err.Error()))
		os.Exit(1)
	}
}
