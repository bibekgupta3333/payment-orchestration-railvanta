// Package config validates the sandbox's runtime environment before startup.
package config

import (
	"errors"
	"net"
	"net/url"
	"strconv"
)

type Config struct {
	Environment  string
	HTTPAddr     string
	HealthAddr   string
	SQLitePath   string
	PublicOrigin string
	DemoMode     bool
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
	if raw := getenv("DEMO_MODE"); raw != "" {
		var err error
		c.DemoMode, err = strconv.ParseBool(raw)
		if err != nil {
			return Config{}, errors.New("DEMO_MODE must be true or false")
		}
	}
	if c.DemoMode && !Loopback(c.HTTPAddr) {
		return Config{}, errors.New("DEMO_MODE requires a literal loopback HTTP_ADDR")
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

func value(raw, fallback string) string {
	if raw == "" {
		return fallback
	}
	return raw
}
