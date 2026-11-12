package config

import (
	"os"
	"time"
)

type Mode string

const (
	ModeLeaky Mode = "leaky"
	ModeFixed Mode = "fixed"
)

type Config struct {
	AppAddr          string
	DebugAddr        string
	UpstreamURL      string
	Mode             Mode
	InventoryTimeout time.Duration
	ShutdownTimeout  time.Duration
}

func Load() Config {
	return Config{
		AppAddr:          getEnv("APP_ADDR", ":8080"),
		DebugAddr:        getEnv("DEBUG_ADDR", "127.0.0.1:6060"),
		UpstreamURL:      getEnv("UPSTREAM_URL", "http://localhost:9090"),
		Mode:             parseMode(getEnv("APP_MODE", string(ModeLeaky))),
		InventoryTimeout: getDuration("INVENTORY_TIMEOUT", 200*time.Millisecond),
		ShutdownTimeout:  getDuration("SHUTDOWN_TIMEOUT", 10*time.Second),
	}
}

func (c Config) IsLeaky() bool {
	return c.Mode == ModeLeaky
}

func parseMode(value string) Mode {
	if Mode(value) == ModeFixed {
		return ModeFixed
	}
	return ModeLeaky
}

func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok && value != "" {
		return value
	}
	return fallback
}

func getDuration(key string, fallback time.Duration) time.Duration {
	parsed, err := time.ParseDuration(getEnv(key, ""))
	if err != nil {
		return fallback
	}
	return parsed
}
