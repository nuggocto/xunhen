package limits_test

import (
	"math"
	"testing"

	"github.com/nuggocto/xunhen/internal/limits"
)

func TestFiniteBudgets(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		change func(*limits.Limits)
		valid  bool
	}{
		{"defaults", func(*limits.Limits) {}, true},
		{
			name: "lower budgets",
			change: func(l *limits.Limits) {
				l.Nodes = 1
				l.InputBytes = 1
			},
			valid: true,
		},
		{"zero configuration", func(l *limits.Limits) { *l = limits.Limits{} }, false},
		{"overflowing input budget", func(l *limits.Limits) { l.InputBytes = math.MaxInt64 }, false},
		{"unlimited base bytes", func(l *limits.Limits) { l.BaseBytes = 0 }, false},
		{"excess state bytes", func(l *limits.Limits) { l.StateBytes++ }, false},
		{"negative nodes", func(l *limits.Limits) { l.Nodes = -1 }, false},
		{"excess entries", func(l *limits.Limits) { l.Entries++ }, false},
		{"unlimited stored lines", func(l *limits.Limits) { l.StoredLines = 0 }, false},
		{"excess state lines", func(l *limits.Limits) { l.StateLines++ }, false},
		{"unlimited line bytes", func(l *limits.Limits) { l.LineBytes = 0 }, false},
		{"excess options", func(l *limits.Limits) { l.OptionalBytes++ }, false},
		{"negative text budget", func(l *limits.Limits) { l.TextBytes = -1 }, false},
		{"excess output", func(l *limits.Limits) { l.OutputBytes++ }, false},
		{"unlimited diff workspace", func(l *limits.Limits) { l.DiffWorkspaceBytes = 0 }, false},
		{"excess diff steps", func(l *limits.Limits) { l.DiffSteps++ }, false},
		{"negative diff comparison budget", func(l *limits.Limits) { l.DiffCompareBytes = -1 }, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			l := limits.Default()
			tt.change(&l)

			if err := l.Validate(); (err == nil) != tt.valid {
				t.Fatalf("validation = %v; valid = %v", err, tt.valid)
			}
		})
	}
}
