package discover

import (
	"errors"
	"fmt"
)

// Problem names why a search did not produce a usable history.
type Problem uint8

// Search problems, in the order the rules check them.
const (
	// Incomplete: a directory or candidate could not be examined, so the
	// search can claim neither a unique match nor absence.
	Incomplete Problem = iota + 1
	// Ambiguous: more than one history qualifies.
	Ambiguous
	// Mismatch: histories exist at the source's names, but none matches the
	// source text.
	Mismatch
	// NoBase: histories exist, but the source cannot serve as a base to
	// verify them.
	NoBase
	// Invalid: every candidate is malformed or unsupported.
	Invalid
	// NotFound: no file exists at any candidate name.
	NotFound
)

// SearchError is a search that did not produce a usable history. Its Result
// holds every report for a detailed explanation.
type SearchError struct {
	Problem Problem
	Result  *Result
}

func (e *SearchError) Error() string {
	switch e.Problem {
	case Incomplete:
		return "the search could not examine every undo directory and candidate, so no history is chosen"
	case Ambiguous:
		return "more than one undo history matches; choose one with --undo"
	case Mismatch:
		return "an undo history exists for this source path, but its reference text differs from the source"
	case NoBase:
		return "an undo history exists for this source path, but the source cannot be used to verify it"
	case Invalid:
		return "the undo files found for this source path are not valid supported histories"
	default:
		return "no undo history for this source path in the supplied directories"
	}
}

// Verified returns the one history that decoded, validated, and matched the
// source text, after a complete search. Reconstruction needs this.
func (r *Result) Verified() (*Load, error) {
	index, problem := choose(r.Reports, r.Scope, false, r.noBase)
	if problem != 0 {
		return nil, &SearchError{Problem: problem, Result: r}
	}
	if r.Reports[index].Outcome != Verified || r.verified == nil {
		panic("discover: chosen history was not kept")
	}

	return r.verified, nil
}

// ForInspection returns the history to inspect: the one verified history if
// there is one, and otherwise a lone validated history whose association with
// the source is unverified. Inspection reads metadata only, so it does not
// need the base, but it still refuses to choose between histories.
func (r *Result) ForInspection() (*Load, error) {
	index, problem := choose(r.Reports, r.Scope, true, r.noBase)
	if problem != 0 {
		return nil, &SearchError{Problem: problem, Result: r}
	}

	switch {
	case r.Reports[index].Outcome == Verified && r.verified != nil:
		return r.verified, nil
	case r.Reports[index].Outcome == Unverified && r.unverified != nil:
		return r.unverified, nil
	case !r.keepUnverified:
		return nil, errors.New("discover: the search did not keep unverified histories")
	default:
		panic("discover: chosen history was not kept")
	}
}

// choose applies the selection rules to a finished search. It returns the
// index of the chosen report, or the problem that prevents a choice. With
// allowUnverified, a lone unverified history may be chosen when no verified
// history exists. noBase says that the source could not serve as a base.
//
// The rules never let order decide: a choice is made only when exactly one
// report qualifies, and only after every directory and candidate was
// examined.
func choose(reports []Report, scope []Scope, allowUnverified, noBase bool) (int, Problem) {
	for _, s := range scope {
		if s.Status == Unavailable {
			return -1, Incomplete
		}
	}

	var verified, unverified []int
	rejected := false
	for i, r := range reports {
		switch r.Outcome {
		case Unexamined:
			return -1, Incomplete
		case Verified:
			verified = append(verified, i)
		case Unverified:
			unverified = append(unverified, i)
		case Rejected:
			rejected = true
		}
	}

	switch {
	case len(verified) > 1:
		return -1, Ambiguous
	case len(verified) == 1:
		return verified[0], 0
	case allowUnverified && len(unverified) > 1:
		return -1, Ambiguous
	case allowUnverified && len(unverified) == 1:
		return unverified[0], 0
	case len(unverified) > 0 && noBase:
		return -1, NoBase
	case len(unverified) > 0:
		return -1, Mismatch
	case rejected:
		return -1, Invalid
	default:
		return -1, NotFound
	}
}

// String names an outcome for diagnostics.
func (o Outcome) String() string {
	switch o {
	case Verified:
		return "verified"
	case Unverified:
		return "not verified"
	case Rejected:
		return "rejected"
	case Unexamined:
		return "not examined"
	case Duplicate:
		return "duplicate"
	default:
		return fmt.Sprintf("outcome %d", o)
	}
}
