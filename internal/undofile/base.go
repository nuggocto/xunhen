package undofile

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/nuggocto/xunhen/internal/limits"
)

// VerifiedBase binds immutable logical lines to one decoded file's reference
// hash. Its lines may include bytes outside the command's supported disk text
// profile; that profile is checked when reading a base from disk.
type VerifiedBase struct {
	file  *DecodedFile
	lines []string
	bytes int
}

// VerifyBase checks the producer's line count and buffer hash. Lines use buffer
// bytes, so an embedded NUL is NUL and no line may contain LF. The source is
// only a diagnostic label. Strings are immutable; the line slice is copied.
func VerifyBase(ctx context.Context, file *DecodedFile, source string, lines []string, lim limits.Limits) (*VerifiedBase, error) {
	if err := lim.Validate(); err != nil {
		return nil, err
	}

	meta, ok := file.Metadata()
	if !ok {
		return nil, errors.New("base requires a complete decoded file")
	}

	fail := func(kind ErrorKind, detail string) error {
		return &InputError{Kind: kind, Source: source, Offset: -1, Field: "base text", Detail: detail}
	}

	if len(lines) > lim.StateLines {
		return nil, fail(Limit, "base exceeds the state line limit")
	}
	if len(lines) != int(meta.BaseLines) {
		return nil, fail(Mismatch, fmt.Sprintf("line count %d does not match undo reference", len(lines)))
	}

	digest := sha256.New()
	stateBytes := 0

	// Hash through a buffer, one digest write per 64 KiB rather than two per
	// line. A line longer than the buffer grows it once.
	buffer := make([]byte, 0, 64<<10)

	for i, line := range lines {
		if i%1024 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		if len(line) > lim.LineBytes || len(line)+1 > lim.StateBytes-stateBytes {
			return nil, fail(Limit, "base exceeds the line or state byte limit")
		}
		// A buffer line never holds LF. Rejecting it here also keeps "a\nb"
		// from verifying as "a\x00b", which hashes the same way.
		if strings.Contains(line, "\n") {
			return nil, fail(Invalid, "line contains LF")
		}
		stateBytes += len(line) + 1

		// Neovim stores an embedded NUL as LF in an internal memline, and
		// terminates each line with NUL.
		start := len(buffer)
		buffer = append(buffer, line...)
		for rest := buffer[start:]; ; {
			at := bytes.IndexByte(rest, 0)
			if at < 0 {
				break
			}
			rest[at] = '\n'
			rest = rest[at+1:]
		}
		buffer = append(buffer, 0)

		if len(buffer) >= 64<<10 {
			_, _ = digest.Write(buffer) // SHA-256 writes never fail.
			buffer = buffer[:0]
		}
	}
	_, _ = digest.Write(buffer)

	if [32]byte(digest.Sum(nil)) != meta.BaseHash && !emptyAutomaticSave(lines, meta.BaseHash) {
		return nil, fail(Mismatch, "content hash does not match undo reference")
	}

	return &VerifiedBase{file: file, lines: slices.Clone(lines), bytes: stateBytes}, nil
}

// emptyAutomaticSave matches the one hash exception: automatic persistence
// omits the dummy line of an empty buffer from the hash input.
func emptyAutomaticSave(lines []string, hash [32]byte) bool {
	return len(lines) == 1 && lines[0] == "" && hash == sha256.Sum256(nil)
}

// File identifies the decoded history this base was verified against.
func (b *VerifiedBase) File() *DecodedFile {
	if b == nil {
		return nil
	}

	return b.file
}

// LineCount is the number of logical lines in the verified reference.
func (b *VerifiedBase) LineCount() int {
	if b == nil {
		return 0
	}

	return len(b.lines)
}

// Lines returns a copy of the verified reference lines.
func (b *VerifiedBase) Lines() []string {
	if b == nil {
		return nil
	}

	return slices.Clone(b.lines)
}

// StateBytes counts each line and its logical terminator.
func (b *VerifiedBase) StateBytes() int {
	if b == nil {
		return 0
	}

	return b.bytes
}
