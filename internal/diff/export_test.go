package diff

import (
	"context"
	"slices"

	"github.com/nuggocto/xunhen/internal/limits"
)

// Lines lets the external tests compare arbitrary line lists. Snapshots can
// only come from a replayed history, which cannot express most test inputs.
func Lines(ctx context.Context, left, right []string, lim limits.Limits) ([]Hunk, error) {
	return compare(ctx, slices.Clone(left), slices.Clone(right), lim)
}
