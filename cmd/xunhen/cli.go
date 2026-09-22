package main

import (
	"context"
	"io"

	"github.com/nuggocto/xunhen/internal/limits"
	"github.com/nuggocto/xunhen/internal/termtext"
)

const (
	exitSuccess     = 0
	exitFailure     = 1
	exitUsage       = 2
	exitInterrupted = 130
)

const helpText = `xunhen - seek traces in Neovim's saved undo history

Usage:
  xunhen [--help | --version]
  xunhen help [command]
  xunhen version
  xunhen inspect --undo PATH

Available commands:
  help       Show help
  version    Show version and build information
  inspect    Describe an undo history

Planned commands (not available in this build):
  show       Reconstruct a retained state
  diff       Compare two retained states
  browse     Explore a history in the terminal

This development build provides history inspection, help, and version reporting.
`

func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if err := ctx.Err(); err != nil {
		return operationError(stderr, err)
	}
	if len(args) == 0 {
		return writeOutput(stdout, stderr, helpText)
	}

	switch args[0] {
	case "-h", "--help":
		if len(args) != 1 {
			return diagnostic(stderr, exitUsage, "help does not accept extra arguments")
		}
		return writeOutput(stdout, stderr, helpText)

	case "help":
		if len(args) == 1 {
			return writeOutput(stdout, stderr, helpText)
		}
		if len(args) != 2 {
			return diagnostic(stderr, exitUsage, "help accepts at most one command")
		}

		switch args[1] {
		case "help":
			return writeOutput(stdout, stderr, helpText)
		case "version":
			return writeOutput(stdout, stderr, "Usage: xunhen version\nShow version and build information.\n")
		case "inspect":
			return writeOutput(stdout, stderr, inspectHelp)
		case "show", "diff", "browse":
			return writeOutput(stdout, stderr, args[1]+" is planned and not available in this build.\n")
		default:
			return diagnostic(stderr, exitUsage, "unknown command; see 'xunhen --help'")
		}

	case "version", "--version":
		if len(args) != 1 {
			return diagnostic(stderr, exitUsage, "version does not accept extra arguments")
		}
		return writeOutput(stdout, stderr, versionText())

	case "inspect":
		return inspect(ctx, args[1:], stdout, stderr)

	case "show", "diff", "browse":
		// Future command arguments are deliberately not parsed or opened yet.
		return diagnostic(stderr, exitFailure, args[0]+" is not available in this build")

	default:
		// Unknown arguments can contain terminal controls. Do not echo them.
		return diagnostic(stderr, exitUsage, "unknown command or option; see 'xunhen --help'")
	}
}

func writeOutput(stdout, stderr io.Writer, text string) int {
	written, err := io.WriteString(stdout, text)
	if err != nil || written != len(text) {
		return diagnostic(stderr, exitFailure, "cannot write output")
	}

	return exitSuccess
}

func diagnostic(stderr io.Writer, status int, message string) int {
	const prefix = "xunhen: "
	safe, err := termtext.Escape(context.Background(), message, limits.Default().OutputBytes-len(prefix+"\n"))
	if err != nil {
		safe = "diagnostic exceeds output byte budget"
		status = exitFailure
	}

	text := prefix + safe + "\n"
	written, err := io.WriteString(stderr, text)
	if err != nil || written != len(text) {
		return exitFailure
	}

	return status
}
