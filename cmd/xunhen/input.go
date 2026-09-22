package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"syscall"
	"unicode/utf8"

	"github.com/nuggocto/xunhen/internal/history"
	"github.com/nuggocto/xunhen/internal/limits"
	"github.com/nuggocto/xunhen/internal/undofile"
)

func loadUndo(ctx context.Context, path string, lim limits.Limits) (*undofile.DecodedFile, error) {
	var decoded *undofile.DecodedFile
	err := readRegular(ctx, path, "undo", lim.InputBytes, func(file *os.File) error {
		var err error
		decoded, err = undofile.Decode(ctx, path, file, lim)
		return err
	})
	if err != nil {
		return nil, err
	}

	return decoded, nil
}

// loadBase reads a base file and splits it into logical lines under the
// supported UTF-8/LF disk profile.
func loadBase(ctx context.Context, path string, lim limits.Limits) ([]string, error) {
	if err := lim.Validate(); err != nil {
		return nil, err
	}

	var data []byte
	err := readRegular(ctx, path, "base", lim.BaseBytes, func(file *os.File) error {
		var err error
		data, err = io.ReadAll(io.LimitReader(file, lim.BaseBytes+1))
		if err != nil {
			return fmt.Errorf("read base file: %w", err)
		}
		if int64(len(data)) > lim.BaseBytes {
			return inputError(undofile.Limit, path, "base input bytes", "input exceeds its budget")
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	return splitBase(path, data, lim)
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

	// Count before splitting. Eight MiB of LF bytes would otherwise build eight
	// million string headers, about 128 MiB, before the line budget rejects them.
	text := strings.TrimSuffix(string(data), "\n")
	if strings.Count(text, "\n") >= lim.StateLines {
		return nil, inputError(undofile.Limit, path, "base lines", "logical line count exceeds its budget")
	}

	lines := strings.Split(text, "\n")
	for _, line := range lines {
		if len(line) > lim.LineBytes {
			return nil, inputError(undofile.Limit, path, "base line bytes", "line exceeds its budget")
		}
	}

	return lines, nil
}

func inputError(kind undofile.ErrorKind, path, field, detail string) error {
	return &undofile.InputError{Kind: kind, Source: path, Offset: -1, Field: field, Detail: detail}
}

// readRegular opens path read-only and rejects anything but a regular file
// that stayed unchanged while read ran. Explicit paths follow symlinks.
// O_NONBLOCK keeps a FIFO from hanging the open; fstat then rejects it.
func readRegular(ctx context.Context, path, kind string, maxBytes int64, read func(*os.File) error) (err error) {
	if err := ctx.Err(); err != nil {
		return err
	}

	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return fmt.Errorf("open %s file: %w", kind, err)
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("close %s file: %w", kind, closeErr))
		}
	}()

	before, err := file.Stat()
	if err != nil {
		return fmt.Errorf("stat %s file: %w", kind, err)
	}
	if !before.Mode().IsRegular() {
		return fmt.Errorf("%s: %s input must be a regular file", path, kind)
	}
	if before.Size() > maxBytes {
		return inputError(undofile.Limit, path, kind+" input bytes", fmt.Sprintf("file exceeds %d bytes", maxBytes))
	}

	if err := read(file); err != nil {
		return err
	}

	after, err := file.Stat()
	if err != nil {
		return fmt.Errorf("stat %s file after reading: %w", kind, err)
	}
	if changedFile(before, after) {
		return fmt.Errorf("%s: %s input changed while reading; retry using an idle copy", path, kind)
	}

	return ctx.Err()
}

func changedFile(before, after os.FileInfo) bool {
	if before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
		return true
	}

	left, lok := before.Sys().(*syscall.Stat_t)
	right, rok := after.Sys().(*syscall.Stat_t)
	return lok && rok && left.Ctim != right.Ctim
}

// recoverStates validates the history and resolves every selector before it
// reads the base, so an unknown node fails without touching the base file.
// Each state is replayed from the verified reference in a fresh workspace.
func recoverStates(ctx context.Context, undoPath, basePath string, nodes []history.NodeID, lim limits.Limits) ([]*history.Snapshot, error) {
	file, err := loadUndo(ctx, undoPath, lim)
	if err != nil {
		return nil, err
	}

	h, err := history.New(ctx, file, lim)
	if err != nil {
		return nil, err
	}

	refs := make([]history.NodeRef, len(nodes))
	for i, id := range nodes {
		if refs[i], err = h.Lookup(id); err != nil {
			return nil, err
		}
	}

	lines, err := loadBase(ctx, basePath, lim)
	if err != nil {
		return nil, err
	}

	base, err := undofile.VerifyBase(ctx, file, basePath, lines, lim)
	if err != nil {
		return nil, err
	}

	reconstructor, err := history.Bind(h, base, lim)
	if err != nil {
		return nil, err
	}

	states := make([]*history.Snapshot, len(refs))
	for i, ref := range refs {
		if states[i], err = reconstructor.Reconstruct(ctx, ref); err != nil {
			return nil, err
		}
	}

	return states, nil
}
