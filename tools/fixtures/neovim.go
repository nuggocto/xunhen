package main

import (
	"bytes"
	"context"
	_ "embed"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	producerVersion   = "v0.12.5"
	sourceRevision    = "5885a30e1e1225349079e7a1c4a3848aa8e43e42"
	packagingRevision = "583b707757678d79349f2f6d7497a288aec5e61e"
	producerDigest    = "5c2cd28eb9b608fff5ca07cd5ee0c31011a15b137b0e63513e253c2c3529bd7f"
	maxOutputBytes    = 64 << 10
)

//go:embed driver.lua
var driver []byte

func resolveProducer(path string) (string, error) {
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		return "", errors.New("the pinned producer requires Linux/amd64")
	}
	if !filepath.IsAbs(path) {
		return "", errors.New("provide an absolute executable path")
	}

	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	binary, err := readBounded(resolved, 128<<20)
	if err != nil {
		return "", err
	}
	if digest(binary) != producerDigest {
		return "", errors.New("the Neovim executable does not match the pinned producer SHA-256")
	}
	return resolved, nil
}

func prepareWork(dir string) error {
	for _, name := range []string{
		"home", "config", "data", "state", "cache", "runtime", "tmp", "undo",
	} {
		if err := os.Mkdir(filepath.Join(dir, name), 0700); err != nil {
			return err
		}
	}

	return os.WriteFile(filepath.Join(dir, "driver.lua"), driver, 0600)
}

func runDriver(ctx context.Context, nvim, dir string, recipe request) error {
	if err := writeJSON(filepath.Join(dir, "request.json"), recipe); err != nil {
		return err
	}

	_, err := invoke(ctx, nvim, dir,
		"--headless",
		"-u", "NONE",
		"--noplugin",
		"-i", "NONE",
		"-n",
		"-l", filepath.Join(dir, "driver.lua"),
	)
	return err
}

func invoke(parent context.Context, nvim, dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(parent, 20*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, nvim, args...)
	cmd.Dir = dir
	cmd.WaitDelay = time.Second
	cmd.Env = []string{
		"PATH=",
		"HOME=" + filepath.Join(dir, "home"),
		"XDG_CONFIG_HOME=" + filepath.Join(dir, "config"),
		"XDG_DATA_HOME=" + filepath.Join(dir, "data"),
		"XDG_STATE_HOME=" + filepath.Join(dir, "state"),
		"XDG_CACHE_HOME=" + filepath.Join(dir, "cache"),
		"XDG_RUNTIME_DIR=" + filepath.Join(dir, "runtime"),
		"TMPDIR=" + filepath.Join(dir, "tmp"),
		"NVIM_LOG_FILE=" + filepath.Join(dir, "nvim.log"),
		"NVIM_APPNAME=xunhen-fixtures",
		"LANG=C.UTF-8",
		"LC_ALL=C.UTF-8",
		"TERM=dumb",
	}

	var output limitedOutput
	cmd.Stdout = &output
	cmd.Stderr = &output

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("run Neovim: %w; output: %q", err, output.String())
	}
	return output.String(), nil
}

func readProducerVersion(ctx context.Context, nvim, dir string) (string, error) {
	version, err := invoke(ctx, nvim, dir, "--version")
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(version, "NVIM "+producerVersion+"\n") {
		return "", errors.New("unexpected Neovim version")
	}
	return version, nil
}

type limitedOutput struct {
	buffer bytes.Buffer
}

func (output *limitedOutput) String() string {
	return output.buffer.String()
}

func (output *limitedOutput) Write(data []byte) (int, error) {
	if output.buffer.Len()+len(data) > maxOutputBytes {
		return 0, errors.New("producer output exceeded 64 KiB")
	}
	return output.buffer.Write(data)
}
