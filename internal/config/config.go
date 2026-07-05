// Package config loads server configuration from environment variables.
package config

import (
	"fmt"
	"os"
	"time"
)

// Config holds all runtime configuration.
type Config struct {
	Currency    string        // e.g. EUR, USD
	IdleTimeout time.Duration // shut the camoufox browser down after this long with no requests (0 disables)
}

// FromEnv builds a Config from BOOKING_* environment variables and validates it.
func FromEnv() (Config, error) {
	c := Config{
		Currency:    getenv("BOOKING_CURRENCY", "EUR"),
		IdleTimeout: 2 * time.Minute,
	}
	if v := os.Getenv("BOOKING_IDLE_TIMEOUT"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return c, fmt.Errorf("BOOKING_IDLE_TIMEOUT %q is not a valid duration (e.g. 2m, 90s, 0 to disable): %w", v, err)
		}
		if d < 0 {
			return c, fmt.Errorf("BOOKING_IDLE_TIMEOUT %q must not be negative (use 0 to disable)", v)
		}
		c.IdleTimeout = d
	}
	if !validCurrency(c.Currency) {
		return c, fmt.Errorf("BOOKING_CURRENCY %q must be a 3-letter code, e.g. EUR or USD", c.Currency)
	}
	return c, nil
}

// validCurrency reports whether s is a 3-letter code. Charset-checked so an
// operator-supplied value can't break out of the searchresults nflt/price
// filter, which appends the currency to the URL without escaping.
func validCurrency(s string) bool {
	if len(s) != 3 {
		return false
	}
	for _, r := range s {
		isLetter := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
		if !isLetter {
			return false
		}
	}
	return true
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
