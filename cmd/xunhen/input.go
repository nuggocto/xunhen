package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"syscall"

	"github.com/nuggocto/xunhen/internal/limits"
	"github.com/nuggocto/xunhen/internal/undofile"
)

// Explicit paths follow symlinks. O_NONBLOCK prevents a FIFO target from
// hanging open; fstat then rejects anything other than a regular file.
func loadUndo(ctx context.Context, path string, lim limits.Limits) (decoded *undofile.DecodedFile, err error) {
	if err := lim.Validate(); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, fmt.Errorf("open undo file: %w", err)
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("close undo file: %w", closeErr))
			decoded = nil
		}
	}()

	before, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !before.Mode().IsRegular() {
		return nil, fmt.Errorf("%s: undo input must be a regular file", path)
	}
	if before.Size() > lim.InputBytes {
		return nil, &undofile.InputError{
			Kind:   undofile.Limit,
			Source: path,
			Offset: -1,
			Field:  "undo input bytes",
			Detail: fmt.Sprintf("file exceeds %d bytes", lim.InputBytes),
		}
	}

	decoded, err = undofile.Decode(ctx, path, file, lim)
	if err != nil {
		return nil, err
	}

	after, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if changedFile(before, after) {
		return nil, fmt.Errorf("%s: undo input changed while reading; retry using an idle copy", path)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	return decoded, nil
}

func changedFile(before, after os.FileInfo) bool {
	if before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
		return true
	}

	left, lok := before.Sys().(*syscall.Stat_t)
	right, rok := after.Sys().(*syscall.Stat_t)
	return lok && rok && left.Ctim != right.Ctim
}
