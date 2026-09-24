package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/nuggocto/xunhen/internal/input"
	"github.com/nuggocto/xunhen/internal/limits"
	"github.com/nuggocto/xunhen/internal/undofile"
)

// loadUndo decodes an explicit undo file and returns the identity it was read
// from, so a later step can confirm the path still names that file.
func loadUndo(ctx context.Context, path string, lim limits.Limits) (*undofile.DecodedFile, input.Identity, error) {
	var decoded *undofile.DecodedFile
	identity, err := input.ReadFile(ctx, path, "undo", lim.InputBytes, func(r io.Reader) error {
		var err error
		decoded, err = undofile.Decode(ctx, path, r, lim)
		return err
	})
	if err != nil {
		return nil, input.Identity{}, err
	}

	return decoded, identity, nil
}

// readText reads a base or source file and splits it into logical lines under
// the supported UTF-8/LF disk profile. kind names the input in messages.
func readText(ctx context.Context, path, kind string, lim limits.Limits) ([]string, input.Identity, error) {
	if err := lim.Validate(); err != nil {
		return nil, input.Identity{}, err
	}

	// ReadFile rejects a file above the limit and never delivers more than
	// the size it had when opened, so the whole text fits the limit.
	var data []byte
	identity, err := input.ReadFile(ctx, path, kind, lim.BaseBytes, func(r io.Reader) error {
		var err error
		if data, err = io.ReadAll(r); err != nil {
			return fmt.Errorf("read %s file: %w", kind, err)
		}
		return nil
	})
	if err != nil {
		return nil, input.Identity{}, err
	}

	lines, err := splitBase(path, data, lim)
	if err != nil {
		return nil, input.Identity{}, err
	}

	return lines, identity, nil
}

// splitBase follows Neovim's reader for Unix files: LF ends a line, and a
// final LF does not start another one. An empty file is one empty line.
func splitBase(path string, data []byte, lim limits.Limits) ([]string, error) {
	var problem string
	switch {
	case bytes.HasPrefix(data, []byte("\xef\xbb\xbf")):
		problem = "UTF-8 BOM"
	case !utf8.Valid(data):
		problem = "invalid UTF-8"
	case bytes.IndexByte(data, 0) >= 0:
		problem = "embedded NUL"
	case bytes.Contains(data, []byte("\r\n")):
		problem = "CRLF base text"
	}
	if problem != "" {
		return nil, inputError(undofile.Unsupported, path, "base text", problem)
	}

	// Count before splitting. A 64 MiB base of LF bytes would otherwise build 64
	// million string headers, about 1 GiB, before the line limit rejects them.
	text := strings.TrimSuffix(string(data), "\n")
	if strings.Count(text, "\n") >= lim.StateLines {
		return nil, inputError(undofile.Limit, path, "base lines", "logical line count exceeds its limit")
	}

	lines := strings.Split(text, "\n")
	for _, line := range lines {
		if len(line) > lim.LineBytes {
			return nil, inputError(undofile.Limit, path, "base line bytes", "line exceeds its limit")
		}
	}

	return lines, nil
}

func inputError(kind undofile.ErrorKind, path, field, detail string) error {
	return &undofile.InputError{Kind: kind, Source: path, Offset: -1, Field: field, Detail: detail}
}
