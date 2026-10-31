package config

import "testing"

func TestLoad(t *testing.T) {
	tests := []struct {
		name      string
		env       map[string]string
		wantError bool
	}{
		{"missing environment", map[string]string{}, true},
		{"production refused", map[string]string{"APP_ENV": "production"}, true},
		{"sandbox defaults", map[string]string{"APP_ENV": "sandbox"}, false},
		{"demo wildcard refused", map[string]string{"APP_ENV": "sandbox", "DEMO_MODE": "true"}, true},
		{"demo loopback", map[string]string{"APP_ENV": "sandbox", "DEMO_MODE": "true", "HTTP_ADDR": "127.0.0.1:8080"}, false},
		{"demo ipv6", map[string]string{"APP_ENV": "sandbox", "DEMO_MODE": "true", "HTTP_ADDR": "[::1]:8080"}, false},
		{"health wildcard refused", map[string]string{"APP_ENV": "sandbox", "HEALTH_ADDR": ":8081"}, true},
		{"health hostname refused", map[string]string{"APP_ENV": "sandbox", "HEALTH_ADDR": "localhost:8081"}, true},
		{"origin credentials refused", map[string]string{"APP_ENV": "sandbox", "PUBLIC_APP_ORIGIN": "https://secret@example.com"}, true},
		{"origin path refused", map[string]string{"APP_ENV": "sandbox", "PUBLIC_APP_ORIGIN": "https://example.com/path"}, true},
		{"invalid demo boolean", map[string]string{"APP_ENV": "sandbox", "DEMO_MODE": "yes"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Load(func(key string) string { return tt.env[key] })
			if (err != nil) != tt.wantError {
				t.Fatalf("Load error=%v, wantError=%v", err, tt.wantError)
			}
			if err == nil && got.SQLitePath == "" {
				t.Fatal("missing database path")
			}
		})
	}
}
