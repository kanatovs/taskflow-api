package config_test

import (
	"strings"
	"testing"
	"time"

	"github.com/kanatovs/taskflow-api/internal/config"
)

func TestJWTConfiguration(t *testing.T) {
	for _, secret := range []string{"", "short", strings.Repeat(" ", 32)} {
		unsetConfigEnv(t)
		t.Setenv("JWT_SECRET", secret)
		assertLoadError(t, "JWT_SECRET")
	}
	for _, ttl := range []string{"", "0s", "-1m", "500ms", "bad"} {
		unsetConfigEnv(t)
		t.Setenv("JWT_ACCESS_TTL", ttl)
		assertLoadError(t, "JWT_ACCESS_TTL")
	}
	unsetConfigEnv(t)
	t.Setenv("JWT_ACCESS_TTL", "30m")
	cfg, err := config.Load()
	if err != nil || cfg.JWTAccessTTL != 30*time.Minute {
		t.Fatalf("JWT configuration: %v", err)
	}
}
