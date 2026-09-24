package diff

import (
	"context"
	"slices"

	"github.com/nuggocto/xunhen/internal/limits"
)

// Lines lets the external tests compare arbitrary line lists. Snapshots can
// only come from a replayed history, which cannot express most test inputs.
func Lines(ctx context.Context, left, right []string, lim limits.Limits) ([]Hunk, error) {
	return compare(ctx, slices.Clone(left), slices.Clone(right), lim, defaultEffort)
}

// LinesWithEffort compares with lowered effort thresholds, so small inputs
// reach the anchored and plain stages.
func LinesWithEffort(ctx context.Context, left, right []string, exact, total int) ([]Hunk, error) {
	return compare(ctx, slices.Clone(left), slices.Clone(right), limits.Default(), effort{exact: exact, total: total})
}
