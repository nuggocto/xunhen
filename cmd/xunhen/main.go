// Command xunhen inspects Neovim undo histories, recovers retained states, and
// compares them. It reads its inputs and never modifies them.
package main

import (
	"context"
	"os"
	"os/signal"
	"runtime/debug"
	"syscall"
)

func main() {
	// Report closed output pipes through the same error path as other writes.
	signal.Ignore(syscall.SIGPIPE)

	// Keep the heap near its live size once it grows large. The largest
	// accepted inputs need under 650 MiB of live data, so this soft limit
	// kept every measured worst case under 800 MiB of resident memory;
	// ordinary runs never reach it. GOMEMLIMIT takes precedence when set.
	if os.Getenv("GOMEMLIMIT") == "" {
		debug.SetMemoryLimit(768 << 20)
	}

	// No background work needs joining. Keep SIGINT's default disposition so
	// a blocked stdout write does not swallow interrupts.
	os.Exit(run(context.Background(), os.Args[1:], os.Stdout, os.Stderr))
}
