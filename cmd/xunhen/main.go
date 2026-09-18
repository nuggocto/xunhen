package main

import (
	"os"
	"os/signal"
	"syscall"
)

func main() {
	// Report closed output pipes through the same error path as other writes.
	signal.Ignore(syscall.SIGPIPE)
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}
