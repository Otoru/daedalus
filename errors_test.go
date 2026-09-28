package daedalus

import (
	"errors"
	"fmt"
	"testing"
)

// TestSentinelErrorsSupportErrorsIs ensures that each named error category —
// invalid config, a breached limit, no compatible plant, and an unroutable
// edge — can be recognized with errors.Is even after being wrapped with
// context via fmt.Errorf and %w.
func TestSentinelErrorsSupportErrorsIs(t *testing.T) {
	sentinels := []struct {
		name string
		err  error
	}{
		{"ErrInvalidConfig", ErrInvalidConfig},
		{"ErrLimitExceeded", ErrLimitExceeded},
		{"ErrNoCompatiblePlant", ErrNoCompatiblePlant},
		{"ErrUnroutableEdge", ErrUnroutableEdge},
		{"ErrUnconnectablePlacement", ErrUnconnectablePlacement},
	}
	for _, tc := range sentinels {
		t.Run(tc.name, func(t *testing.T) {
			if tc.err == nil {
				t.Fatalf("%s is nil", tc.name)
			}
			wrapped := fmt.Errorf("generate layout: %w", tc.err)
			if !errors.Is(wrapped, tc.err) {
				t.Errorf("errors.Is did not recognize %s after wrapping", tc.name)
			}
		})
	}

	if errors.Is(ErrLimitExceeded, ErrInvalidConfig) {
		t.Error("distinct sentinels must not be equivalent")
	}
}
