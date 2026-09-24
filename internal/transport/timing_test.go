package transport

import (
	"errors"
	"testing"
	"time"
)

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		in      Timing
		wantErr bool
	}{
		{"defaults valid", DefaultTiming, false},
		{"tiny valid", Timing{
			DialTimeout: 1, HandshakeTimeout: 1, SetupTimeout: 1,
			KeepAliveInterval: 1, PingTimeout: 1, StreamOpenTimeout: 1,
			DrainTimeout: 1, MinBackoff: 1, MaxBackoff: 1,
			HealthyWindow: 1, JitterRatio: 0,
		}, false},
		{"zero dial timeout", Timing{DialTimeout: 0}, true},
		{"negative jitter", Timing{MinBackoff: 1, MaxBackoff: 1, HealthyWindow: 1, JitterRatio: -1}, true},
		{"over-one jitter", Timing{MinBackoff: 1, MaxBackoff: 1, HealthyWindow: 1, JitterRatio: 2}, true},
		{"floor above ceiling", Timing{MinBackoff: 2, MaxBackoff: 1, HealthyWindow: 2}, true},
		{"healthy window below floor", Timing{MinBackoff: 2, MaxBackoff: 3, HealthyWindow: 1}, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.in.validate()
			if tc.wantErr {
				if !errors.Is(err, ErrInvalidTiming) {
					t.Fatalf("want ErrInvalidTiming, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestDefaultTimingValues(t *testing.T) {
	if DefaultTiming.MinBackoff != time.Second {
		t.Fatalf("MinBackoff = %v, want 1s", DefaultTiming.MinBackoff)
	}
	if DefaultTiming.MaxBackoff != 30*time.Second {
		t.Fatalf("MaxBackoff = %v, want 30s", DefaultTiming.MaxBackoff)
	}
	if DefaultTiming.HealthyWindow != 60*time.Second {
		t.Fatalf("HealthyWindow = %v, want 60s", DefaultTiming.HealthyWindow)
	}
	if DefaultTiming.JitterRatio != 0.2 {
		t.Fatalf("JitterRatio = %v, want 0.2", DefaultTiming.JitterRatio)
	}
}
