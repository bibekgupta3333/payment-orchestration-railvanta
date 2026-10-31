package main

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/railvanta/railvanta/backend/internal/auth"
	"github.com/railvanta/railvanta/backend/internal/config"
	"github.com/railvanta/railvanta/backend/internal/storage"
	httpapi "github.com/railvanta/railvanta/backend/internal/transport/http"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := run(ctx, logger); err != nil {
		logger.Error("API stopped", "error", err.Error())
		os.Exit(1)
	}
}

func run(ctx context.Context, logger *slog.Logger) error {
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		return err
	}
	store, err := storage.Open(ctx, cfg.SQLitePath)
	if err != nil {
		return fmt.Errorf("initialize database: %w", err)
	}
	defer func() {
		if err := store.Close(); err != nil {
			logger.Error("close database", "error", err.Error())
		}
	}()
	if cfg.DemoMode {
		if err := auth.SeedDemo(ctx, store.DB, auth.DemoCredentials{OperatorKey: os.Getenv("DEMO_OPERATOR_KEY"), ReaderKey: os.Getenv("DEMO_READER_KEY"), OtherWorkspaceKey: os.Getenv("DEMO_OTHER_WORKSPACE_KEY"), SessionToken: os.Getenv("DEMO_SESSION_TOKEN"), CSRFToken: os.Getenv("DEMO_CSRF_TOKEN")}); err != nil {
			return fmt.Errorf("initialize demo fixtures: %w", err)
		}
	}
	apiListener, err := net.Listen("tcp", cfg.HTTPAddr)
	if err != nil {
		return fmt.Errorf("listen API: %w", err)
	}
	defer apiListener.Close()
	healthListener, err := net.Listen("tcp", cfg.HealthAddr)
	if err != nil {
		return fmt.Errorf("listen health: %w", err)
	}
	defer healthListener.Close()
	var draining atomic.Bool
	healthMux := http.NewServeMux()
	healthMux.HandleFunc("GET /health/live", func(w http.ResponseWriter, _ *http.Request) { writeHealth(w, http.StatusOK) })
	healthMux.HandleFunc("GET /health/startup", func(w http.ResponseWriter, _ *http.Request) { writeHealth(w, http.StatusOK) })
	healthMux.HandleFunc("GET /health/ready", func(w http.ResponseWriter, r *http.Request) {
		if draining.Load() {
			writeHealth(w, http.StatusServiceUnavailable)
			return
		}
		sqlDB, err := store.DB.DB()
		if err != nil {
			writeHealth(w, http.StatusServiceUnavailable)
			return
		}
		checkCtx, cancel := context.WithTimeout(r.Context(), time.Second)
		defer cancel()
		if err := sqlDB.PingContext(checkCtx); err != nil {
			writeHealth(w, http.StatusServiceUnavailable)
			return
		}
		writeHealth(w, http.StatusOK)
	})
	api := &http.Server{Handler: httpapi.NewHandler(store.DB, httpapi.Config{Logger: logger, PublicOrigin: cfg.PublicOrigin, RequestsPerMinute: 600}), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 1 << 20}
	health := &http.Server{Handler: healthMux, ReadHeaderTimeout: 2 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second}
	errorsCh := make(chan error, 2)
	go func() { errorsCh <- api.Serve(apiListener) }()
	go func() { errorsCh <- health.Serve(healthListener) }()
	logger.Info("sandbox API ready", "http_addr", apiListener.Addr().String(), "health_addr", healthListener.Addr().String(), "demo_mode", cfg.DemoMode)
	var serveErr error
	select {
	case <-ctx.Done():
	case serveErr = <-errorsCh:
	}
	draining.Store(true)
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	apiErr := api.Shutdown(shutdownCtx)
	healthErr := health.Shutdown(shutdownCtx)
	if apiErr != nil {
		_ = api.Close()
	}
	if healthErr != nil {
		_ = health.Close()
	}
	if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
		return serveErr
	}
	return errors.Join(apiErr, healthErr)
}

func writeHealth(w http.ResponseWriter, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Request-ID", rand.Text())
	w.WriteHeader(status)
	if status == http.StatusOK {
		_, _ = w.Write([]byte("{\"status\":\"ok\"}\n"))
	} else {
		_, _ = w.Write([]byte("{\"status\":\"unavailable\"}\n"))
	}
}
