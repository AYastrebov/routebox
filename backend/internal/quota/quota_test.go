package quota

import (
	"math"
	"testing"
)

func TestExhausted(t *testing.T) {
	tests := []struct {
		name          string
		quota, rx, tx int64
		want          bool
	}{
		{"no limit", 0, 1 << 40, 1 << 40, false},
		{"no limit, nothing used", 0, 0, 0, false},
		{"under", 100, 40, 50, false},
		{"one byte short", 100, 99, 0, false},
		{"exactly at the limit", 100, 60, 40, true},
		{"over", 100, 500, 500, true},
		{"rx alone reaches it", 100, 100, 0, true},
		{"tx alone reaches it", 100, 0, 100, true},
		{"zero used against a limit", 100, 0, 0, false},
		{"negative quota is no limit", -1, 5, 5, false},
		{"overflowing sum is still exhausted", 100, math.MaxInt64, math.MaxInt64, true},
		{"huge quota, huge use", math.MaxInt64, math.MaxInt64 - 1, 1, true},
		{"huge quota, not quite", math.MaxInt64, math.MaxInt64 - 2, 1, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Exhausted(tt.quota, tt.rx, tt.tx); got != tt.want {
				t.Fatalf("Exhausted(%d, %d, %d) = %v, want %v", tt.quota, tt.rx, tt.tx, got, tt.want)
			}
		})
	}
}

func TestState(t *testing.T) {
	tests := []struct {
		name          string
		quota, rx, tx int64
		enabled       bool
		expiresAt     int64
		now           int64
		want          Reason
	}{
		{"plain active peer", 0, 0, 0, true, 0, 1000, ReasonNone},
		{"active with room left and time left", 100, 10, 10, true, 2000, 1000, ReasonNone},

		{"manual beats everything", 100, 500, 500, false, 500, 1000, ReasonManual},
		{"manual alone", 0, 0, 0, false, 0, 1000, ReasonManual},
		{"manual beats quota", 100, 200, 0, false, 0, 1000, ReasonManual},
		{"manual beats expiry", 0, 0, 0, false, 500, 1000, ReasonManual},

		{"quota beats expiry", 100, 100, 0, true, 500, 1000, ReasonQuota},
		{"quota alone", 100, 50, 50, true, 0, 1000, ReasonQuota},

		{"expired alone", 0, 0, 0, true, 1000, 1000, ReasonExpired},
		{"expiry boundary is strict: now == expiresAt", 0, 0, 0, true, 1000, 1000, ReasonExpired},
		{"one second before expiry", 0, 0, 0, true, 1001, 1000, ReasonNone},
		{"long past expiry", 0, 0, 0, true, 1, 1000, ReasonExpired},
		{"expiresAt 0 never expires", 0, 0, 0, true, 0, math.MaxInt64, ReasonNone},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := State(tt.quota, tt.rx, tt.tx, tt.enabled, tt.expiresAt, tt.now)
			if got != tt.want {
				t.Fatalf("State(%d, %d, %d, %v, %d, %d) = %q, want %q",
					tt.quota, tt.rx, tt.tx, tt.enabled, tt.expiresAt, tt.now, got, tt.want)
			}
		})
	}
}
