package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"syscall"
	"unicode/utf8"
	"unsafe"

	"github.com/nuggocto/xunhen/internal/history"
	"github.com/nuggocto/xunhen/internal/limits"
	"github.com/nuggocto/xunhen/internal/termtext"
)

const showHelp = `Usage: xunhen show --undo PATH --base PATH --node ID [--raw --final-newline=include|omit]
       xunhen show --source PATH --undo-dir DIR... --node ID [--raw ...]

Reconstruct a retained buffer state using a matching base file.
  --undo PATH             Undo-file path
  --base PATH             Matching source text (read-only)
  --source PATH           Find the history by this source file's path; the
                          file is also the base
  --undo-dir DIR          Undo directory to search; repeat for more (at most 32)
  --node ID               Retained sequence number; 0 is the root (required)
  --raw                   Write UTF-8/LF bytes to redirected stdout
  --final-newline POLICY  Required with --raw: include or omit
  -h, --help              Show this help

With --source, exactly one history in the supplied directories must match the
source text; docs/discovery.md describes the names searched. Otherwise nothing
is reconstructed, and the diagnostic lists every candidate and the explicit
--undo and --base form to use instead.

Base files must be UTF-8/LF text without a BOM, NUL, or CRLF. Raw export also
rejects a selected state with invalid UTF-8 or NUL, which retained edits can
hold. A lone CR is literal line content. Historical encoding and final-newline
settings are unknown; the chosen policy does not restore original file bytes.
An empty buffer is one empty line, so include writes a single LF and omit
writes nothing.

Default output shows tabs, quotes, backslashes, and printable Unicode as they
are, and escapes terminal controls and invalid bytes. Use --raw for exact text.
Raw output is refused when stdout is a terminal. Inputs are never modified.
Exit status: 0 success, 1 input/output failure, 2 invalid arguments, 130 interrupt.
`

type showOptions struct {
	in           *inputs
	node         history.NodeID
	hasNode      bool
	raw          bool
	finalNewline string
}

func show(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if helpRequested(args) {
		return writeOutput(stdout, stderr, showHelp)
	}

	lim := limits.Default()
	options, err := parseShowArgs(args, lim)
	if err != nil {
		return diagnostic(stderr, exitUsage, err.Error())
	}

	if options.raw {
		terminal, err := outputIsTerminal(stdout)
		if err != nil {
			return operationError(stderr, err)
		}
		if terminal {
			return diagnostic(stderr, exitFailure, "raw output requires redirected stdout")
		}
	}

	states, err := recoverStates(ctx, options.in, []history.NodeID{options.node}, lim)
	if err != nil {
		return operationError(stderr, err)
	}
	lines := states[0].Lines()

	// Check the whole state before the first write, so an unsupported raw
	// export never leaves partial output.
	if options.raw {
		for _, line := range lines {
			if err := checkRawLine(line); err != nil {
				return operationError(stderr, err)
			}
		}
	}

	out := newOutput(stdout)
	writeState(out, lines, options)
	return out.finish(stderr)
}

func parseShowArgs(args []string, lim limits.Limits) (showOptions, error) {
	var options showOptions

	flags := newFlags("show")
	flags.BoolVar(&options.raw, "raw", false, "raw export")

	options.in = registerInputs(flags, true)
	nodeFlag(flags, "node", &options.node, &options.hasNode)
	flags.Func("final-newline", "include or omit", once("final-newline", func(value string) error {
		if value != "include" && value != "omit" {
			return errors.New("--final-newline must be include or omit")
		}
		options.finalNewline = value
		return nil
	}))

	if err := parseFlags(flags, args); err != nil {
		return showOptions{}, err
	}

	if flags.NArg() != 0 {
		return showOptions{}, usage("show", true)
	}
	if err := options.in.check("show", true, lim); err != nil {
		return showOptions{}, err
	}

	switch {
	case !options.hasNode:
		return showOptions{}, errors.New("show requires --node ID; see 'xunhen show --help'")
	case options.raw && options.finalNewline == "":
		return showOptions{}, errors.New("--raw requires --final-newline=include|omit")
	case !options.raw && options.finalNewline != "":
		return showOptions{}, errors.New("--final-newline requires --raw")
	}

	return options, nil
}

// TCGETS checks the descriptor, so /dev/null is not mistaken for a terminal.
func outputIsTerminal(output io.Writer) (bool, error) {
	file, ok := output.(interface{ Fd() uintptr })
	if !ok {
		return false, nil
	}

	var attrs syscall.Termios
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, file.Fd(), syscall.TCGETS, uintptr(unsafe.Pointer(&attrs)))
	switch errno {
	case 0:
		return true, nil
	case syscall.ENOTTY:
		return false, nil
	default:
		return false, fmt.Errorf("check output terminal: %w", errno)
	}
}

// writeState streams a reconstructed state. Raw lines have already passed
// checkRawLine, so nothing but the write itself can fail here.
func writeState(out *output, lines []string, options showOptions) {
	for i, line := range lines {
		if !options.raw {
			line = termtext.EscapeDisplay(line)
		}
		out.text(line)

		last := i == len(lines)-1
		if !last || !options.raw || options.finalNewline == "include" {
			out.text("\n")
		}
	}
}

// checkRawLine rejects text that UTF-8/LF export cannot represent faithfully.
// Lines never hold LF: the decoder stores a serialized LF as NUL, and base
// verification rejects LF.
func checkRawLine(line string) error {
	switch {
	case !utf8.ValidString(line):
		return errors.New("selected state contains invalid UTF-8; raw export unsupported")
	case strings.Contains(line, "\x00"):
		return errors.New("selected state contains NUL; raw export unsupported")
	}

	return nil
}
