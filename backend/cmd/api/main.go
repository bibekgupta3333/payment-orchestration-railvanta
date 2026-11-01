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
	"github.com/railvanta/railvanta/backend/internal/event"
	"github.com/railvanta/railvanta/backend/internal/payment"
	"github.com/railvanta/railvanta/backend/internal/provider/mock"
	"github.com/railvanta/railvanta/backend/internal/storage"
	httpapi "github.com/railvanta/railvanta/backend/internal/transport/http"
)

func main() {
	if handled, code := runSubcommand(os.Args[1:], os.Stdout, os.Stderr); handled {
		os.Exit(code)
	}
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
	// The dispatcher starts only after migrations (storage.Open) and seeding.
	providers := payment.ProviderSet{"mock": mock.New(store.DB, mock.Options{DelayedSuccessDelay: cfg.Dispatcher.MockDelayedSuccessDelay, TimeoutDelay: cfg.Dispatcher.MockTimeoutDelay})}
	executor := payment.NewExecutor(store.DB, payment.ExecutorOptions{Providers: providers, ProviderTimeout: cfg.Dispatcher.ProviderTimeout, Logger: logger})
	recovery := payment.NewRecovery(store.DB, payment.RecoveryOptions{
		Providers: providers, LookupTimeout: cfg.Dispatcher.ProviderTimeout, BaseBackoff: cfg.Dispatcher.RecoveryBaseBackoff,
		MaxBackoff: cfg.Dispatcher.RecoveryMaxBackoff, LookupDelay: cfg.Dispatcher.RecoveryLookupDelay, Logger: logger,
	})
	// Startup scan (WBS 2.4.3): before the dispatcher starts, nothing holds a
	// live lease, so every submitted attempt is stale and possibly executed.
	if recovered, err := recovery.RecoverStale(ctx); err != nil {
		return fmt.Errorf("recover stale attempts: %w", err)
	} else if recovered > 0 {
		logger.Warn("stale submitted attempts moved to the lookup path", "count", recovered)
	}
	dispatcher := event.NewDispatcher(store.DB, event.Options{
		PollInterval: cfg.Dispatcher.PollInterval, LeaseDuration: cfg.Dispatcher.LeaseDuration, DrainTimeout: cfg.Dispatcher.DrainTimeout,
		MaxInFlight: cfg.Dispatcher.MaxInFlight, Logger: logger,
	})
	executor.Register(dispatcher)
	recovery.Register(dispatcher)
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
		if err := storage.CheckWritable(checkCtx, store.DB); err != nil {
			logger.Error("readiness degraded", "error", err.Error())
			writeHealth(w, http.StatusServiceUnavailable)
			return
		}
		writeHealth(w, http.StatusOK)
	})
	api := &http.Server{Handler: httpapi.NewHandler(store.DB, httpapi.Config{Logger: logger, PublicOrigin: cfg.PublicOrigin, RequestsPerMinute: 600, Router: executor.Router(), OnAccepted: dispatcher.Notify}), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 1 << 20}
	health := &http.Server{Handler: healthMux, ReadHeaderTimeout: 2 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second}
	errorsCh := make(chan error, 2)
	go func() { errorsCh <- api.Serve(apiListener) }()
	go func() { errorsCh <- health.Serve(healthListener) }()
	dispatcher.Start()
	logger.Info("sandbox API ready", "http_addr", apiListener.Addr().String(), "health_addr", healthListener.Addr().String(), "demo_mode", cfg.DemoMode)
	var serveErr error
	select {
	case <-ctx.Done():
	case serveErr = <-errorsCh:
	}
	draining.Store(true)
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	// Stop claiming and drain bounded in-flight work alongside HTTP shutdown;
	// the deferred store.Close runs only after both have returned.
	dispatcherErr := make(chan error, 1)
	go func() { dispatcherErr <- dispatcher.Stop(shutdownCtx) }()
	apiErr := api.Shutdown(shutdownCtx)
	healthErr := health.Shutdown(shutdownCtx)
	if err := <-dispatcherErr; err != nil {
		logger.Warn("dispatcher stop", "error", err.Error())
	}
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
