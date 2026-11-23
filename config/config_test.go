package config_test

import (
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/riskibarqy/ros-go-common/config"
)

func TestStrings(t *testing.T) {
	values := map[string]string{"present": "  service \t", "blank": " \n "}
	if got := config.GetString(values, "present", "fallback"); got != "service" {
		t.Fatal(got)
	}
	for _, key := range []string{"missing", "blank"} {
		if value, ok := config.GetOptionalString(values, key); ok || value != "" {
			t.Fatalf("%s: %q %t", key, value, ok)
		}
		if got := config.GetString(values, key, " fallback "); got != " fallback " {
			t.Fatal(got)
		}
		if _, err := config.GetRequiredString(values, key); err == nil {
			t.Fatal("accepted missing/blank", key)
		}
	}
	if got, err := config.GetRequiredString(values, "present"); err != nil || got != "service" {
		t.Fatal(got, err)
	}
}
func TestBool(t *testing.T) {
	for _, tc := range []struct {
		value   string
		want    bool
		invalid bool
	}{
		{"", true, false}, {"  ", true, false}, {"true", true, false}, {" false ", false, false},
		{"TRUE", false, true}, {"False", false, true}, {"1", false, true}, {"yes", false, true},
	} {
		t.Run(tc.value, func(t *testing.T) {
			got, err := config.GetBool(map[string]string{"KEY": tc.value}, "KEY", true)
			if (err != nil) != tc.invalid || !tc.invalid && got != tc.want {
				t.Fatal(got, err)
			}
		})
	}
}
func TestUnsigned(t *testing.T) {
	for _, tc := range []struct {
		value   string
		want    uint64
		invalid bool
	}{
		{"", 7, false}, {"42", 42, false}, {" +42 ", 42, false}, {"0", 0, false}, {"18446744073709551615", math.MaxUint64, false},
		{"-1", 0, true}, {"1.5", 0, true}, {"18446744073709551616", 0, true}, {"++1", 0, true},
	} {
		t.Run(tc.value, func(t *testing.T) {
			got, err := config.GetU64(map[string]string{"KEY": tc.value}, "KEY", 7)
			if (err != nil) != tc.invalid || !tc.invalid && got != tc.want {
				t.Fatal(got, err)
			}
		})
	}
}
func TestDurations(t *testing.T) {
	for _, tc := range []struct {
		value   string
		want    time.Duration
		invalid bool
	}{
		{"", time.Second, false}, {"250ms", 250 * time.Millisecond, false}, {" 2s ", 2 * time.Second, false},
		{"3m", 3 * time.Minute, false}, {"4h", 4 * time.Hour, false}, {"0ms", 0, false},
		{"10", 0, true}, {"-1s", 0, true}, {"+1s", 0, true}, {"1.5s", 0, true}, {"1d", 0, true}, {"1 s", 0, true},
		{"18446744073709551615h", 0, true}, {"9223372036854775808ms", 0, true},
	} {
		t.Run(tc.value, func(t *testing.T) {
			got, err := config.GetDuration(map[string]string{"KEY": tc.value}, "KEY", time.Second)
			if (err != nil) != tc.invalid || !tc.invalid && got != tc.want {
				t.Fatal(got, err)
			}
		})
	}
	for _, tc := range []struct {
		value   string
		want    time.Duration
		invalid bool
	}{
		{"", time.Minute, false}, {" 2 ", 2 * time.Second, false}, {"+3", 3 * time.Second, false}, {"-1", 0, true}, {"1s", 0, true}, {"18446744073709551615", 0, true},
	} {
		got, err := config.GetDurationSeconds(map[string]string{"KEY": tc.value}, "KEY", time.Minute)
		if (err != nil) != tc.invalid || !tc.invalid && got != tc.want {
			t.Fatalf("%q: %v %v", tc.value, got, err)
		}
	}
}
func TestErrorsDoNotRetainValues(t *testing.T) {
	const secret = "private-sensitive-value"
	_, err := config.GetDuration(map[string]string{"TIMEOUT": secret}, "TIMEOUT", 0)
	var configError *config.ConfigError
	if !errors.As(err, &configError) || configError.Key != "TIMEOUT" || strings.Contains(err.Error(), secret) {
		t.Fatal(err)
	}
}
