package config

import (
	"errors"
	"net"
	"os"
	"strconv"
	"time"
)

// Redis is optional: an empty address disables caching.
type Redis struct {
	Addr             string
	Password         string
	DB               int
	TTL              time.Duration
	DialTimeout      time.Duration
	OperationTimeout time.Duration
}

func loadRedis() (Redis, error) {
	cfg := Redis{Addr: os.Getenv("REDIS_ADDR"), Password: os.Getenv("REDIS_PASSWORD")}
	if cfg.Addr != "" {
		host, _, splitErr := net.SplitHostPort(cfg.Addr)
		if err := validateHTTPAddr(cfg.Addr); err != nil || splitErr != nil || host == "" {
			return Redis{}, errors.New("REDIS_ADDR: must use host:port with a port between 1 and 65535")
		}
	}
	if value, ok := os.LookupEnv("REDIS_DB"); ok {
		var err error
		cfg.DB, err = strconv.Atoi(value)
		if err != nil || cfg.DB < 0 || cfg.DB > 15 {
			return Redis{}, errors.New("REDIS_DB: must be an integer between 0 and 15")
		}
	}
	for _, setting := range []struct {
		name     string
		target   *time.Duration
		fallback time.Duration
	}{
		{"CACHE_TTL", &cfg.TTL, time.Minute},
		{"REDIS_DIAL_TIMEOUT", &cfg.DialTimeout, 200 * time.Millisecond},
		{"REDIS_OPERATION_TIMEOUT", &cfg.OperationTimeout, 100 * time.Millisecond},
	} {
		value, err := loadDuration(setting.name, setting.fallback)
		if err != nil {
			return Redis{}, err
		}
		if value < time.Millisecond {
			return Redis{}, errors.New(setting.name + ": must be at least one millisecond")
		}
		*setting.target = value
	}
	return cfg, nil
}
