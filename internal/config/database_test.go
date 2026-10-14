package config_test

import (
	"testing"
)

func TestLoadDatabaseConfiguration(t *testing.T) {
	for _, value := range []string{"", "invalid", "https://host/db", "postgres://host", "postgres://host/", "postgres://secret%ZZ@host/db"} {
		t.Run(value, func(t *testing.T) {
			unsetConfigEnv(t)
			t.Setenv("DATABASE_URL", value)
			assertLoadError(t, "DATABASE_URL")
		})
	}
	for _, value := range []string{"", "0s", "-1s", "bad"} {
		t.Run("timeout "+value, func(t *testing.T) {
			unsetConfigEnv(t)
			t.Setenv("DATABASE_TIMEOUT", value)
			assertLoadError(t, "DATABASE_TIMEOUT")
		})
	}
}
