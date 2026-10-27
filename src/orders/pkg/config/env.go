package config

import (
	"os"
	"strconv"
	"time"
)

// EnvInt reads a non-negative runtime setting; malformed values keep the default.
func EnvInt(name string, fallback int) int {
	value, err := strconv.Atoi(os.Getenv(name))
	if err != nil || value < 0 {
		return fallback
	}
	return value
}
func EnvSeconds(name string, fallback int) time.Duration {
	seconds := EnvInt(name, fallback)
	if seconds == 0 {
		seconds = fallback
	}
	return time.Duration(seconds) * time.Second
}
func SQSWaitTime() int32 {
	value := EnvInt("SQS_WAIT_TIME_SECONDS", 20)
	if value > 20 {
		value = 20
	}
	return int32(value)
}
