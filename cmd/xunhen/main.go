// Command xunhen inspects Neovim undo histories, recovers retained states, and
// compares them. It reads its inputs and never modifies them.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	// Report closed output pipes through the same error path as other writes.
	signal.Ignore(syscall.SIGPIPE)
	// No background work needs joining. Keep SIGINT's default disposition so
	// a blocked stdout write does not swallow interrupts.
	os.Exit(run(context.Background(), os.Args[1:], os.Stdout, os.Stderr))
}
