package config_test

import (
	"strings"
	"testing"
	"time"

	"github.com/adam/bookingcom-mcp/internal/config"
)

func TestFromEnvIdleTimeout(t *testing.T) {
	tests := []struct {
		name    string
		env     string // "" means unset
		want    time.Duration
		wantErr string // substring; "" means no error
	}{
		{name: "default when unset", env: "", want: 2 * time.Minute},
		{name: "valid duration", env: "90s", want: 90 * time.Second},
		{name: "zero disables", env: "0", want: 0},
		{name: "unparseable", env: "soon", wantErr: "not a valid duration"},
		{name: "negative rejected", env: "-5m", wantErr: "must not be negative"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("BOOKING_IDLE_TIMEOUT", tt.env) // "" is treated as unset by FromEnv
			c, err := config.FromEnv()
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("FromEnv() err=%v, want substring %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("FromEnv() unexpected err=%v", err)
			}
			if c.IdleTimeout != tt.want {
				t.Errorf("IdleTimeout=%v, want %v", c.IdleTimeout, tt.want)
			}
		})
	}
}
