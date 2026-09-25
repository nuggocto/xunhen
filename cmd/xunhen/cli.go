package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"

	"github.com/nuggocto/xunhen/internal/history"
	"github.com/nuggocto/xunhen/internal/termtext"
)

// Outside the browser, an interrupt keeps its default disposition, so the
// kernel ends the process and the shell reports status 130 without a code
// path here. The browser catches signals to restore the terminal first and
// then exits with the same 128+signal status.
const (
	exitSuccess   = 0
	exitFailure   = 1
	exitUsage     = 2
	exitInterrupt = 130
)

const helpText = `xunhen - seek traces in Neovim's saved undo history

Usage:
  xunhen [--help | --version]
  xunhen help [command]
  xunhen version
  xunhen inspect --undo PATH
  xunhen show --undo PATH --base PATH --node ID [--raw --final-newline=include|omit]
  xunhen diff --undo PATH --base PATH --from ID --to ID
  xunhen browse --undo PATH --base PATH

Each command can also find the history from the source file instead:
  xunhen inspect --source PATH --undo-dir DIR...
  xunhen show --source PATH --undo-dir DIR... --node ID
  xunhen diff --source PATH --undo-dir DIR... --from ID --to ID
  xunhen browse --source PATH --undo-dir DIR...

Available commands:
  help       Show help
  version    Show version and build information
  inspect    Describe an undo history
  show       Reconstruct a retained state
  diff       Compare two retained states
  browse     Explore a history in the terminal

Run 'xunhen help COMMAND' for a command's options.
`

const versionHelp = "Usage: xunhen version\nShow version and build information.\n"

// commandHelp answers "xunhen help COMMAND". Each command also accepts --help
// on its own.
var commandHelp = map[string]string{
	"help":    helpText,
	"version": versionHelp,
	"inspect": inspectHelp,
	"show":    showHelp,
	"diff":    diffHelp,
	"browse":  browseHelp,
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
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

		if text, ok := commandHelp[args[1]]; ok {
			return writeOutput(stdout, stderr, text)
		}
		return diagnostic(stderr, exitUsage, "unknown command; see 'xunhen --help'")

	case "version", "--version":
		if len(args) != 1 {
			return diagnostic(stderr, exitUsage, "version does not accept extra arguments")
		}
		return writeOutput(stdout, stderr, versionText())

	case "inspect":
		return inspect(ctx, args[1:], stdout, stderr)
	case "show":
		return show(ctx, args[1:], stdout, stderr)
	case "diff":
		return compareStates(ctx, args[1:], stdout, stderr)

	case "browse":
		return browse(ctx, args[1:], os.Stdin, stdout, stderr)

	default:
		// Unknown arguments can contain terminal controls. Do not echo them.
		return diagnostic(stderr, exitUsage, "unknown command or option; see 'xunhen --help'")
	}
}

func writeOutput(stdout, stderr io.Writer, text string) int {
	// A short write always returns an error, so err alone covers partial output.
	if _, err := io.WriteString(stdout, text); err != nil {
		return diagnostic(stderr, exitFailure, "cannot write output")
	}

	return exitSuccess
}

func diagnostic(stderr io.Writer, status int, message string) int {
	if _, err := io.WriteString(stderr, "xunhen: "+termtext.Escape(message)+"\n"); err != nil {
		return exitFailure
	}

	return status
}

// helpRequested reports whether a command's only argument asks for its help.
func helpRequested(args []string) bool {
	return len(args) == 1 && (args[0] == "--help" || args[0] == "-h")
}

// newFlags returns a command flag set whose errors become diagnostics rather
// than printed usage.
func newFlags(command string) *flag.FlagSet {
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	return flags
}

// parseFlags rejects --help mixed with other arguments. helpRequested handles
// help on its own before parsing.
func parseFlags(flags *flag.FlagSet, args []string) error {
	err := flags.Parse(args)
	if errors.Is(err, flag.ErrHelp) {
		return fmt.Errorf("%s --help must be used alone", flags.Name())
	}

	return err
}

// output streams a command's result through a buffer and keeps the first
// write error, so rendering runs straight through and a failure is reported
// once. Commands finish everything that can fail for another reason, such as
// reading, replaying, validating, or comparing, before the first write, so
// only an output failure can leave partial output behind.
type output struct {
	w   *bufio.Writer
	err error
}

func newOutput(w io.Writer) *output {
	return &output{w: bufio.NewWriterSize(w, 64<<10)}
}

func (o *output) printf(format string, args ...any) {
	if o.err == nil {
		_, o.err = fmt.Fprintf(o.w, format, args...)
	}
}

func (o *output) text(s string) {
	if o.err == nil {
		_, o.err = o.w.WriteString(s)
	}
}

// finish flushes the buffer and reports any write failure as the command's
// status.
func (o *output) finish(stderr io.Writer) int {
	if o.err == nil {
		o.err = o.w.Flush()
	}
	if o.err != nil {
		return diagnostic(stderr, exitFailure, "cannot write output")
	}

	return exitSuccess
}

// pathFlag registers a path flag that may be given once and not empty.
func pathFlag(flags *flag.FlagSet, name string, path *string) {
	flags.Func(name, name+" path", once(name, func(value string) error {
		if value == "" {
			return fmt.Errorf("--%s requires a path", name)
		}
		*path = value
		return nil
	}))
}

// nodeFlag registers a node selector that may be given once. It accepts plain
// decimal digits only: no sign, prefix, or separator.
func nodeFlag(flags *flag.FlagSet, name string, id *history.NodeID, set *bool) {
	flags.Func(name, "node ID", once(name, func(value string) error {
		n, err := strconv.ParseUint(value, 10, 31)
		if errors.Is(err, strconv.ErrRange) {
			return fmt.Errorf("--%s exceeds the supported ID range", name)
		}
		if err != nil {
			return fmt.Errorf("--%s requires a non-negative decimal ID", name)
		}

		*id, *set = history.NodeID(n), true
		return nil
	}))
}

// once wraps a flag setter so that repeating the flag is a usage error.
func once(name string, set func(string) error) func(string) error {
	seen := false
	return func(value string) error {
		if seen {
			return fmt.Errorf("--%s must be supplied only once", name)
		}
		seen = true
		return set(value)
	}
}
