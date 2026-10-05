package main

import (
	"errors"
	"flag"
	"fmt"
	"strings"

	"github.com/nuggocto/xunhen/internal/limits"
)

// inputs are the input arguments shared by every command. Either undo (with
// base, for reconstruction) names the history explicitly, or source and
// undoDirs find it. dirsFromEnv records that undoDirs came from
// XUNHEN_UNDO_DIR rather than --undo-dir, for diagnostics.
type inputs struct {
	undo, base, source string
	undoDirs           []string
	dirsFromEnv        bool
}

// undoDirEnv names the environment variable listing the undo directories to
// search, separated by colons, when a source comes without --undo-dir. It
// saves typing the same --undo-dir on every command, and it is still the
// user's own choice: xunhen never guesses where the editor keeps history.
const undoDirEnv = "XUNHEN_UNDO_DIR"

// registerInputs adds the input flags to a command. withBase adds --base, which
// only reconstructing commands accept.
func registerInputs(flags *flag.FlagSet, withBase bool) *inputs {
	in := &inputs{}
	pathFlag(flags, "undo", &in.undo)
	if withBase {
		pathFlag(flags, "base", &in.base)
	}
	pathFlag(flags, "source", &in.source)

	// Each --undo-dir is one literal path. Commas are filename characters,
	// not separators as in 'undodir'; repeat the flag for more directories.
	flags.Func("undo-dir", "undo directory to search", func(value string) error {
		if value == "" {
			return errors.New("--undo-dir requires a directory path")
		}
		in.undoDirs = append(in.undoDirs, value)
		return nil
	})

	return in
}

// parseInputs parses a command's arguments: its flags and at most one FILE
// argument, which means --source FILE. Flags may come before or after FILE,
// and every argument after "--" is a FILE, so a source whose name starts
// with a dash still works. A source without --undo-dir searches the
// directories XUNHEN_UNDO_DIR lists. getenv reads the environment.
func parseInputs(flags *flag.FlagSet, in *inputs, args []string, command string, withBase bool, lim limits.Limits, getenv func(string) string) error {
	files, err := parseArgs(flags, args)
	if err != nil {
		return err
	}

	switch {
	case len(files) > 1:
		return usage(command, withBase)
	case len(files) == 1 && in.source != "":
		return errors.New("give the source file once, as FILE or with --source")
	case len(files) == 1 && (in.undo != "" || in.base != ""):
		return errors.New("a FILE argument cannot be combined with --undo or --base")
	case len(files) == 1 && files[0] == "":
		return errors.New("FILE cannot be empty")
	case len(files) == 1:
		in.source = files[0]
	}

	if in.source != "" && len(in.undoDirs) == 0 {
		in.undoDirs, in.dirsFromEnv = splitUndoDirs(getenv(undoDirEnv)), true
	}

	return in.check(command, withBase, lim)
}

// splitUndoDirs reads XUNHEN_UNDO_DIR: directories separated by colons, as
// in PATH. Empty entries are skipped, so an unset or empty variable lists
// nothing. A directory whose name holds a colon needs --undo-dir.
func splitUndoDirs(value string) []string {
	var dirs []string
	for dir := range strings.SplitSeq(value, ":") {
		if dir != "" {
			dirs = append(dirs, dir)
		}
	}

	return dirs
}

// parseArgs parses flags that may come before and after positional
// arguments, and returns the positional ones. It splits the arguments
// itself, because a FlagSet stops at the first argument that is not a flag.
// A flag without "=" that takes a value consumes the next argument, even
// one that looks like a flag or is "--"; "--" in a flag's position ends
// the flags, and every argument after it is positional. So
// "--undo-dir -- FILE" names the directory "--" and a FILE, as it would for
// the flag package alone.
func parseArgs(flags *flag.FlagSet, args []string) ([]string, error) {
	var named, positional []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--":
			positional = append(positional, args[i+1:]...)
			i = len(args)
		case len(arg) < 2 || arg[0] != '-':
			// "-" alone is a positional argument, as for the flag package.
			positional = append(positional, arg)
		default:
			named = append(named, arg)
			name, _, attached := strings.Cut(strings.TrimLeft(arg, "-"), "=")
			if !attached && takesValue(flags, name) && i+1 < len(args) {
				i++
				named = append(named, args[i])
			}
		}
	}

	if err := parseFlags(flags, named); err != nil {
		return nil, err
	}
	if flags.NArg() != 0 {
		// Every positional argument was set aside above; the flag package
		// leaves one only for input it cannot read as flags.
		return nil, fmt.Errorf("unexpected argument %q", flags.Arg(0))
	}
	return positional, nil
}

// takesValue reports whether a defined flag needs a value. Unknown flags
// take none, so parseFlags reports them by name.
func takesValue(flags *flag.FlagSet, name string) bool {
	f := flags.Lookup(name)
	if f == nil {
		return false
	}
	boolean, ok := f.Value.(interface{ IsBoolFlag() bool })
	return !ok || !boolean.IsBoolFlag()
}

// usage is the error for an invocation that names no usable input form.
func usage(command string, withBase bool) error {
	explicit := "--undo PATH"
	if withBase {
		explicit += " --base PATH"
	}

	return fmt.Errorf("expected %s FILE, %s %s, or %s --source PATH --undo-dir DIR; see 'xunhen %s --help'",
		command, command, explicit, command, command)
}

// hintError is a usage error that explains how to fix itself, one
// diagnostic line per entry.
type hintError struct {
	lines []string
}

func (e *hintError) Error() string {
	return strings.Join(e.lines, " ")
}

// noUndoDirs is the usage error for a source with no directory to search.
var noUndoDirs = &hintError{lines: []string{
	"no undo directory to search for the source's history",
	"  Pass --undo-dir DIR, or set " + undoDirEnv + " once, for example in your shell's startup file:",
	"    export " + undoDirEnv + "=$HOME/.local/state/nvim/undo",
	"  Neovim prints its undo directory with :echo &undodir. Separate several with a colon.",
}}

// check applies the input rules before any file is opened.
func (in *inputs) check(command string, withBase bool, lim limits.Limits) error {
	switch {
	case in.source != "" && (in.undo != "" || in.base != ""):
		if withBase {
			return errors.New("--source cannot be combined with --undo or --base")
		}
		return errors.New("--source cannot be combined with --undo")
	case in.source != "" && len(in.undoDirs) == 0:
		return noUndoDirs
	case len(in.undoDirs) > lim.SearchDirs && in.dirsFromEnv:
		return fmt.Errorf("%s lists %d directories; at most %d can be searched", undoDirEnv, len(in.undoDirs), lim.SearchDirs)
	case len(in.undoDirs) > lim.SearchDirs:
		return fmt.Errorf("at most %d --undo-dir directories can be searched", lim.SearchDirs)
	case in.source == "" && len(in.undoDirs) != 0:
		return errors.New("--undo-dir requires --source")
	case in.source == "" && (in.undo == "" || (withBase && in.base == "")):
		return usage(command, withBase)
	}

	return nil
}
