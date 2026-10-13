package config

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// Config contains the settings used to start the application.
type Config struct {
	DatabaseURL           string
	DatabaseTimeout       time.Duration
	HTTPAddr              string
	HTTPReadHeaderTimeout time.Duration
	HTTPReadTimeout       time.Duration
	HTTPWriteTimeout      time.Duration
	HTTPIdleTimeout       time.Duration
	HTTPShutdownTimeout   time.Duration
}

// Load reads and validates the process environment, using defaults for unset variables.
func Load() (Config, error) {
	cfg := Config{HTTPAddr: ":8080"}
	if value, ok := os.LookupEnv("HTTP_ADDR"); ok {
		cfg.HTTPAddr = value
	}
	if err := validateHTTPAddr(cfg.HTTPAddr); err != nil {
		return Config{}, fmt.Errorf("HTTP_ADDR: %w", err)
	}
	cfg.DatabaseURL = os.Getenv("DATABASE_URL")
	parsed, parseErr := url.Parse(cfg.DatabaseURL)
	if parseErr != nil || parsed == nil || (parsed.Scheme != "postgres" && parsed.Scheme != "postgresql") || parsed.Hostname() == "" || parsed.Path == "" || parsed.Path == "/" {
		return Config{}, errors.New("DATABASE_URL: must be a PostgreSQL URL with a host and database name")
	}

	var err error
	cfg.DatabaseTimeout, err = loadDuration("DATABASE_TIMEOUT", 3*time.Second)
	if err != nil {
		return Config{}, err
	}
	cfg.HTTPReadHeaderTimeout, err = loadDuration("HTTP_READ_HEADER_TIMEOUT", 5*time.Second)
	if err != nil {
		return Config{}, err
	}
	cfg.HTTPReadTimeout, err = loadDuration("HTTP_READ_TIMEOUT", 10*time.Second)
	if err != nil {
		return Config{}, err
	}
	cfg.HTTPWriteTimeout, err = loadDuration("HTTP_WRITE_TIMEOUT", 10*time.Second)
	if err != nil {
		return Config{}, err
	}
	cfg.HTTPIdleTimeout, err = loadDuration("HTTP_IDLE_TIMEOUT", 60*time.Second)
	if err != nil {
		return Config{}, err
	}
	cfg.HTTPShutdownTimeout, err = loadDuration("HTTP_SHUTDOWN_TIMEOUT", 10*time.Second)
	if err != nil {
		return Config{}, err
	}

	return cfg, nil
}

func validateHTTPAddr(addr string) error {
	if addr == "" || strings.IndexFunc(addr, unicode.IsSpace) >= 0 {
		return errors.New("must be a non-empty host:port address without whitespace")
	}

	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("must use host:port format: %w", err)
	}
	portNumber, err := strconv.ParseUint(port, 10, 16)
	if err != nil {
		return fmt.Errorf("port must be a number between 1 and 65535: %w", err)
	}
	if portNumber == 0 {
		return errors.New("port must be between 1 and 65535")
	}

	return nil
}

func loadDuration(name string, fallback time.Duration) (time.Duration, error) {
	value, ok := os.LookupEnv(name)
	if !ok {
		return fallback, nil
	}

	duration, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("%s: must be a valid duration such as 5s or 1m: %w", name, err)
	}
	if duration <= 0 {
		return 0, fmt.Errorf("%s: must be greater than zero", name)
	}

	return duration, nil
}
