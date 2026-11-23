// Package config parses explicitly supplied values without reading process state.
package config

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

// ConfigError identifies the key and reason, never the supplied value.
type ConfigError struct {
	Key    string
	Reason string
}

func (e *ConfigError) Error() string { return fmt.Sprintf("configuration `%s` %s", e.Key, e.Reason) }

func GetOptionalString(values map[string]string, key string) (string, bool) {
	value := strings.TrimSpace(values[key])
	return value, value != ""
}
func GetString(values map[string]string, key, fallback string) string {
	if value, ok := GetOptionalString(values, key); ok {
		return value
	}
	return fallback
}
func GetRequiredString(values map[string]string, key string) (string, error) {
	if value, ok := GetOptionalString(values, key); ok {
		return value, nil
	}
	return "", &ConfigError{key, "is required and must not be blank"}
}
func GetBool(values map[string]string, key string, fallback bool) (bool, error) {
	value, ok := GetOptionalString(values, key)
	if !ok {
		return fallback, nil
	}
	switch value {
	case "true":
		return true, nil
	case "false":
		return false, nil
	}
	return false, &ConfigError{key, "must be `true` or `false`"}
}
func GetU64(values map[string]string, key string, fallback uint64) (uint64, error) {
	value, ok := GetOptionalString(values, key)
	if !ok {
		return fallback, nil
	}
	result, err := parseUnsigned(value)
	if err != nil {
		return 0, &ConfigError{key, "must be a valid unsigned 64-bit integer"}
	}
	return result, nil
}
func parseUnsigned(value string) (uint64, error) {
	// Rust accepts a leading plus sign for scalar unsigned integers.
	return strconv.ParseUint(strings.TrimPrefix(value, "+"), 10, 64)
}
func GetDurationSeconds(values map[string]string, key string, fallback time.Duration) (time.Duration, error) {
	value, ok := GetOptionalString(values, key)
	if !ok {
		return fallback, nil
	}
	amount, err := parseUnsigned(value)
	if err != nil {
		return 0, &ConfigError{key, "must be an unsigned integer number of seconds"}
	}
	return duration(key, amount, time.Second)
}
func GetDuration(values map[string]string, key string, fallback time.Duration) (time.Duration, error) {
	value, ok := GetOptionalString(values, key)
	if !ok {
		return fallback, nil
	}
	i := 0
	for i < len(value) && value[i] >= '0' && value[i] <= '9' {
		i++
	}
	if i == len(value) {
		return 0, &ConfigError{key, "must include a duration suffix: `ms`, `s`, `m`, or `h`"}
	}
	amount, err := strconv.ParseUint(value[:i], 10, 64)
	if err != nil {
		return 0, &ConfigError{key, "must start with an unsigned integer"}
	}
	var unit time.Duration
	switch value[i:] {
	case "ms":
		unit = time.Millisecond
	case "s":
		unit = time.Second
	case "m":
		unit = time.Minute
	case "h":
		unit = time.Hour
	default:
		return 0, &ConfigError{key, "must use duration suffix `ms`, `s`, `m`, or `h`"}
	}
	return duration(key, amount, unit)
}
func duration(key string, amount uint64, unit time.Duration) (time.Duration, error) {
	if amount > uint64(math.MaxInt64)/uint64(unit) {
		return 0, &ConfigError{key, "duration is too large"}
	}
	return time.Duration(amount) * unit, nil
}
