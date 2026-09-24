package discover

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/nuggocto/xunhen/internal/history"
	"github.com/nuggocto/xunhen/internal/input"
	"github.com/nuggocto/xunhen/internal/limits"
	"github.com/nuggocto/xunhen/internal/undofile"
)

// Request describes one source-based search.
type Request struct {
	Target Target
	Dirs   []string // undo directories as the user supplied them

	// Base is the source text prepared for verification. It is nil when the
	// source could not serve as a base, and BaseErr then says why.
	Base    *undofile.PreparedBase
	BaseErr error

	// KeepUnverified keeps a validated history whose base did not verify,
	// for inspection, which needs no base. Otherwise only a verified history
	// is kept.
	KeepUnverified bool
}

// Outcome classifies one examined candidate.
type Outcome uint8

// Candidate outcomes.
const (
	// Verified: the file decoded, validated, and matched the source text.
	Verified Outcome = iota + 1
	// Unverified: the file decoded and validated, but the source is missing,
	// unsupported, or differs from its reference text.
	Unverified
	// Rejected: the file is not a supported, valid history.
	Rejected
	// Unexamined: the file could not be examined, so the search is
	// incomplete: a permission failure, a symlink, or an exhausted limit.
	Unexamined
	// Duplicate: the same file as an earlier candidate, through a hard link
	// or a second name.
	Duplicate
)

// Report is what the search learned about one candidate. Path and Err are
// untrusted text; escape them for a terminal.
type Report struct {
	Path     string
	Identity input.Identity
	Outcome  Outcome
	Err      error  // why the candidate was not verified
	SameAs   string // for a Duplicate, the earlier report's path
}

// DirStatus classifies one supplied undo directory.
type DirStatus uint8

// Directory statuses.
const (
	// Searched: the directory was open while its candidate names were
	// examined.
	Searched DirStatus = iota + 1
	// Repeated: the same directory as an earlier argument, under the same or
	// another name.
	Repeated
	// Unavailable: the directory could not be opened, so the search is
	// incomplete.
	Unavailable
)

// Scope records one supplied undo directory.
type Scope struct {
	Path   string
	Status DirStatus
	Err    error  // for Unavailable
	SameAs string // for Repeated, the earlier argument
}

// Load is a validated history found by a search, with its source association.
type Load struct {
	Path     string
	Identity input.Identity
	File     *undofile.DecodedFile
	History  *history.History

	// Base is the verified source text, or nil when the association is
	// unverified; Err then says why.
	Base *undofile.VerifiedBase
	Err  error
}

// Result is a finished search. Selection happens through Verified and
// ForInspection, which apply the rules in choose.go.
type Result struct {
	Target  Target
	Scope   []Scope
	Reports []Report

	// At most one verified and one unverified history are kept, so a search
	// never holds more than two decoded files besides the one it is reading.
	verified, unverified *Load
	keepUnverified       bool
	noBase               bool
}

// ErrNoBase reports a candidate that could not be verified because the source
// could not serve as a base.
var ErrNoBase = errors.New("the source text is not available to verify against")

// alias is a supplied directory path that named a directory already held. The
// search reads the directory once, through the held handle, but the path must
// still name it at the end: retargeted, it could hold another history.
type alias struct {
	path     string
	identity input.Identity
}

// probe is one candidate name looked up in one held directory, kept so the
// search can confirm at the end that nothing it relied on changed.
type probe struct {
	dir   *input.Dir
	name  string
	entry input.Entry
}

// Search examines the candidate names for req.Target in every supplied
// directory before choosing anything, so neither argument order nor directory
// order nor modification time can decide between histories. It reads only
// candidate names, never a directory listing, and never descends into
// subdirectories.
//
// Search returns an error only for cancellation, for an invalid request, and
// for an input that changed during the search, which makes every finding
// unreliable. Everything else is recorded in the Result.
func Search(ctx context.Context, req Request, lim limits.Limits) (result *Result, err error) {
	if err := lim.Validate(); err != nil {
		return nil, err
	}
	if len(req.Dirs) == 0 || len(req.Dirs) > lim.SearchDirs {
		return nil, fmt.Errorf("a search needs 1 to %d undo directories", lim.SearchDirs)
	}
	if req.Target.Path == "" {
		return nil, errors.New("a search needs a source path")
	}

	result = &Result{Target: req.Target, keepUnverified: req.KeepUnverified, noBase: req.Base == nil}
	s := &search{ctx: ctx, req: req, lim: lim, result: result, remaining: lim.SearchBytes}

	defer func() {
		for _, d := range s.held {
			if closeErr := d.Close(); closeErr != nil && err == nil {
				err = closeErr
			}
		}
		if err != nil {
			result = nil
		}
	}()

	// The sidecar name only applies in the source's own directory, and only
	// when the user supplied that directory.
	sourceDir, dirErr := input.Stat(req.Target.Dir())
	s.haveSourceDir = dirErr == nil

	for _, path := range req.Dirs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := s.searchDir(path, sourceDir); err != nil {
			return nil, err
		}
	}

	if err := s.recheck(); err != nil {
		return nil, err
	}

	return result, nil
}

type search struct {
	ctx    context.Context
	req    Request
	lim    limits.Limits
	result *Result

	held          []*input.Dir
	aliases       []alias
	probes        []probe
	haveSourceDir bool
	remaining     int64
	verified      int
	unverified    int
}

func (s *search) searchDir(path string, sourceDir input.Identity) error {
	d, err := input.OpenDir(path)
	if err != nil {
		s.result.Scope = append(s.result.Scope, Scope{Path: path, Status: Unavailable, Err: err})
		return nil
	}

	for _, held := range s.held {
		if held.Identity().SameFile(d.Identity()) {
			_ = d.Close() // A second handle to a directory already held.
			s.aliases = append(s.aliases, alias{path: path, identity: held.Identity()})
			s.result.Scope = append(s.result.Scope, Scope{Path: path, Status: Repeated, SameAs: held.Path()})
			return nil
		}
	}
	s.held = append(s.held, d)
	s.result.Scope = append(s.result.Scope, Scope{Path: path, Status: Searched})

	names := []string{s.req.Target.UndoName()}
	if s.haveSourceDir && d.Identity().SameFile(sourceDir) {
		names = append(names, s.req.Target.SidecarName())
	}

	for _, name := range names {
		if err := s.examine(d, name); err != nil {
			return err
		}
	}

	return nil
}

// examine looks up one candidate name and, when it names a new regular file
// within the byte limit, decodes, validates, and verifies it.
func (s *search) examine(d *input.Dir, name string) error {
	path := joinPath(d.Path(), name)
	entry, err := d.Lookup(name)
	if err != nil {
		s.report(Report{Path: path, Outcome: Unexamined, Err: err})
		return nil
	}
	s.probes = append(s.probes, probe{dir: d, name: name, entry: entry})

	switch {
	case !entry.Exists:
		return nil
	case entry.Symlink():
		s.report(Report{Path: path, Identity: entry.Identity, Outcome: Unexamined, Err: errSymlinkCandidate})
		return nil
	case !entry.Regular():
		s.report(Report{Path: path, Identity: entry.Identity, Outcome: Rejected, Err: input.ErrNotRegular})
		return nil
	}

	for _, earlier := range s.result.Reports {
		if earlier.Outcome != Duplicate && earlier.Identity.SameFile(entry.Identity) {
			s.report(Report{Path: path, Identity: entry.Identity, Outcome: Duplicate, SameAs: earlier.Path})
			return nil
		}
	}

	// Charge the whole file before reading it, so that failed and rejected
	// reads count as fully as successful ones.
	if entry.Identity.Size > s.remaining {
		err := &LimitError{Path: path, Size: entry.Identity.Size, Remaining: s.remaining}
		s.report(Report{Path: path, Identity: entry.Identity, Outcome: Unexamined, Err: err})
		return nil
	}
	s.remaining -= entry.Identity.Size

	return s.load(d, name, path, entry)
}

var errSymlinkCandidate = errors.New("the candidate is a symbolic link, which discovery does not follow; pass it with --undo to use it")

// LimitError reports a candidate left unread because the search had already
// read too much.
type LimitError struct {
	Path            string
	Size, Remaining int64
}

func (e *LimitError) Error() string {
	return fmt.Sprintf("%s: %d bytes would exceed the search's remaining %d-byte limit", e.Path, e.Size, e.Remaining)
}

func (s *search) load(d *input.Dir, name, path string, entry input.Entry) error {
	var file *undofile.DecodedFile
	// ReadEntry refuses a file that changed since the lookup and never reads
	// past its looked-up size, which is what the search charged for it.
	identity, err := d.ReadEntry(s.ctx, name, entry.Identity, s.lim.InputBytes, func(r io.Reader) error {
		var err error
		file, err = undofile.Decode(s.ctx, path, r, s.lim)
		return err
	})
	if err != nil {
		return s.failed(path, entry.Identity, err)
	}

	h, err := history.New(s.ctx, file, s.lim)
	if err != nil {
		return s.failed(path, identity, err)
	}

	loaded := &Load{Path: path, Identity: identity, File: file, History: h, Err: s.req.BaseErr}
	if s.req.Base != nil {
		loaded.Base, loaded.Err = s.req.Base.Verify(file)
	} else if loaded.Err == nil {
		loaded.Err = ErrNoBase
	}

	if loaded.Base != nil {
		s.keepVerified(loaded)
		s.report(Report{Path: path, Identity: identity, Outcome: Verified})
		return nil
	}

	s.keepUnverified(loaded)
	s.report(Report{Path: path, Identity: identity, Outcome: Unverified, Err: loaded.Err})
	return nil
}

// failed classifies an error from reading or validating a candidate.
// Cancellation and changed inputs end the search; a malformed or unsupported
// file is rejected; anything that stopped the candidate from being judged
// leaves the search incomplete.
func (s *search) failed(path string, identity input.Identity, err error) error {
	var changed *input.ChangedError
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || errors.As(err, &changed) {
		return err
	}

	outcome := Unexamined
	var problem *undofile.InputError
	if errors.As(err, &problem) {
		switch problem.Kind {
		case undofile.Invalid, undofile.Truncated, undofile.Unsupported:
			outcome = Rejected
		}
	}

	s.report(Report{Path: path, Identity: identity, Outcome: outcome, Err: err})
	return nil
}

func (s *search) keepVerified(l *Load) {
	s.verified++
	s.result.verified = nil
	if s.verified == 1 {
		s.result.verified = l
	}

	// A verified history takes precedence over any unverified one.
	s.result.unverified = nil
}

func (s *search) keepUnverified(l *Load) {
	s.unverified++
	s.result.unverified = nil
	if s.req.KeepUnverified && s.verified == 0 && s.unverified == 1 {
		s.result.unverified = l
	}
}

func (s *search) report(r Report) {
	s.result.Reports = append(s.result.Reports, r)
}

// recheck confirms that every supplied directory path, repeated ones
// included, still names the directory that was searched, and that every
// candidate name still refers to what was found, including names that were
// absent. A change there could hide or add a history, so the whole result
// would be unreliable.
func (s *search) recheck() error {
	for _, d := range s.held {
		if err := d.Recheck(); err != nil {
			return err
		}
	}
	for _, a := range s.aliases {
		if now, err := input.Stat(a.path); err != nil || !now.SameFile(a.identity) {
			return &input.ChangedError{Path: a.path, Kind: "undo directory", Detail: "now names a different directory"}
		}
	}

	for _, p := range s.probes {
		now, err := p.dir.Lookup(p.name)
		if err != nil || !now.Same(p.entry) {
			return &input.ChangedError{
				Path:   joinPath(p.dir.Path(), p.name),
				Kind:   "undo",
				Detail: "appeared, disappeared, or changed during the search",
			}
		}
	}

	return nil
}

// joinPath shows a candidate under the directory as the user wrote it.
func joinPath(dir, name string) string {
	if len(dir) != 0 && dir[len(dir)-1] == '/' {
		return dir + name
	}

	return dir + "/" + name
}
