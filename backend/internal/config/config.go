// Package config validates the sandbox's runtime environment before startup.
package config

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"time"
)

type Config struct {
	Environment  string
	HTTPAddr     string
	HealthAddr   string
	SQLitePath   string
	PublicOrigin string
	DemoMode     bool
	Dispatcher   Dispatcher
}

// Dispatcher holds outbox dispatcher and mock provider timing. Defaults suit
// interactive use; tests and E2E shorten them through the environment.
type Dispatcher struct {
	PollInterval    time.Duration // DISPATCHER_POLL_INTERVAL (default 250ms)
	LeaseDuration   time.Duration // DISPATCHER_LEASE (default 30s)
	DrainTimeout    time.Duration // DISPATCHER_DRAIN_TIMEOUT (default 5s)
	MaxInFlight     int           // DISPATCHER_MAX_IN_FLIGHT (default 4)
	ProviderTimeout time.Duration // PROVIDER_TIMEOUT (default 10s)
	// MockDelayedSuccessDelay and MockTimeoutDelay tune the deterministic mock.
	MockDelayedSuccessDelay time.Duration // MOCK_DELAYED_SUCCESS_DELAY (default 2s)
	MockTimeoutDelay        time.Duration // MOCK_TIMEOUT_DELAY (default 5s)
	// RecoveryBaseBackoff and RecoveryMaxBackoff bound the delay between
	// lookups while an outcome stays unknown (WBS 2.4).
	RecoveryBaseBackoff time.Duration // RECOVERY_BASE_BACKOFF (default 1s)
	RecoveryMaxBackoff  time.Duration // RECOVERY_MAX_BACKOFF (default 5m)
	// RecoveryLookupDelay postpones the first lookup of a stale attempt found
	// at startup. RECOVERY_LOOKUP_DELAY (default 0)
	RecoveryLookupDelay time.Duration
	// RecoveryResolveDelay postpones the FIRST same-provider lookup after an
	// ambiguous outcome so a demo can show the unknown state. RECOVERY_RESOLVE_DELAY (default 0)
	RecoveryResolveDelay time.Duration
}

func Load(getenv func(string) string) (Config, error) {
	c := Config{Environment: getenv("APP_ENV"), HTTPAddr: value(getenv("HTTP_ADDR"), ":8080"), HealthAddr: value(getenv("HEALTH_ADDR"), "127.0.0.1:8081"), SQLitePath: value(getenv("SQLITE_PATH"), "data/railvanta.db"), PublicOrigin: value(getenv("PUBLIC_APP_ORIGIN"), "http://localhost:3000")}
	if c.Environment != "sandbox" {
		return Config{}, errors.New("APP_ENV must be explicitly set to sandbox")
	}
	if _, _, err := net.SplitHostPort(c.HTTPAddr); err != nil {
		return Config{}, errors.New("HTTP_ADDR must be a host:port address")
	}
	if !Loopback(c.HealthAddr) {
		return Config{}, errors.New("HEALTH_ADDR must bind a literal loopback address")
	}
	var err error
	if raw := getenv("DEMO_MODE"); raw != "" {
		c.DemoMode, err = strconv.ParseBool(raw)
		if err != nil {
			return Config{}, errors.New("DEMO_MODE must be true or false")
		}
	}
	if c.DemoMode && !Loopback(c.HTTPAddr) {
		return Config{}, errors.New("DEMO_MODE requires a literal loopback HTTP_ADDR")
	}
	if c.Dispatcher, err = loadDispatcher(getenv); err != nil {
		return Config{}, err
	}
	u, err := url.Parse(c.PublicOrigin)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return Config{}, errors.New("PUBLIC_APP_ORIGIN must be an HTTP(S) origin without a path or credentials")
	}
	return c, nil
}

// Loopback uses literal IPs to avoid trusting DNS for the demo's bind boundary.
func Loopback(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func loadDispatcher(getenv func(string) string) (Dispatcher, error) {
	d := Dispatcher{}
	durations := []struct {
		name          string
		target        *time.Duration
		fallback      time.Duration
		minimum, most time.Duration
	}{
		{"DISPATCHER_POLL_INTERVAL", &d.PollInterval, 250 * time.Millisecond, 10 * time.Millisecond, time.Minute},
		{"DISPATCHER_LEASE", &d.LeaseDuration, 30 * time.Second, 300 * time.Millisecond, time.Hour},
		{"DISPATCHER_DRAIN_TIMEOUT", &d.DrainTimeout, 5 * time.Second, 0, time.Minute},
		{"PROVIDER_TIMEOUT", &d.ProviderTimeout, 10 * time.Second, 10 * time.Millisecond, 5 * time.Minute},
		{"MOCK_DELAYED_SUCCESS_DELAY", &d.MockDelayedSuccessDelay, 2 * time.Second, time.Millisecond, 5 * time.Minute},
		{"MOCK_TIMEOUT_DELAY", &d.MockTimeoutDelay, 5 * time.Second, time.Millisecond, 5 * time.Minute},
		{"RECOVERY_BASE_BACKOFF", &d.RecoveryBaseBackoff, time.Second, 10 * time.Millisecond, time.Hour},
		{"RECOVERY_MAX_BACKOFF", &d.RecoveryMaxBackoff, 5 * time.Minute, 10 * time.Millisecond, 24 * time.Hour},
		{"RECOVERY_LOOKUP_DELAY", &d.RecoveryLookupDelay, 0, 0, time.Hour},
		{"RECOVERY_RESOLVE_DELAY", &d.RecoveryResolveDelay, 0, 0, time.Minute},
	}
	for _, item := range durations {
		*item.target = item.fallback
		raw := getenv(item.name)
		if raw == "" {
			continue
		}
		parsed, err := time.ParseDuration(raw)
		if err != nil || parsed < item.minimum || parsed > item.most {
			return Dispatcher{}, fmt.Errorf("%s must be a duration between %s and %s", item.name, item.minimum, item.most)
		}
		*item.target = parsed
	}
	d.MaxInFlight = 4
	if raw := getenv("DISPATCHER_MAX_IN_FLIGHT"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 64 {
			return Dispatcher{}, errors.New("DISPATCHER_MAX_IN_FLIGHT must be an integer from 1 to 64")
		}
		d.MaxInFlight = n
	}
	return d, nil
}

func value(raw, fallback string) string {
	if raw == "" {
		return fallback
	}
	return raw
}
