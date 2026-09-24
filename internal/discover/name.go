// Package discover finds the undo history for a source file in undo
// directories the user supplies. It derives the names Neovim would give the
// history, looks only at those names, and reports every candidate it
// examined. A name only suggests a candidate: separator encoding is not
// reversible, so a candidate counts as the source's history only after it
// decodes, validates, and matches the source text.
package discover

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// Target is a source path resolved the way Neovim 0.12.5 resolves a buffer's
// file name before naming its undo file. Path is absolute unless its
// directory could not be resolved, in which case it is the path as given,
// exactly as Neovim uses it.
type Target struct {
	Path string
}

// UndoName is the name of the history inside an undo directory: the path with
// every slash replaced by %. Different paths can share a name, such as
// /a%b/c.go and /a/b%c.go, so a name identifies candidates, never an owner.
func (t Target) UndoName() string {
	return strings.ReplaceAll(t.Path, "/", "%")
}

// Dir is the directory holding the source: everything before the last slash.
func (t Target) Dir() string {
	i := strings.LastIndexByte(t.Path, '/')
	switch {
	case i < 0:
		return "."
	case i == 0:
		return "/"
	default:
		return t.Path[:i]
	}
}

// SidecarName is the name Neovim uses beside the source when 'undodir'
// contains ".": the source's name with a leading dot and a .un~ suffix.
func (t Target) SidecarName() string {
	return "." + t.Path[strings.LastIndexByte(t.Path, '/')+1:] + ".un~"
}

// Resolve derives the Target for source. cwd must be the physical working
// directory, as getcwd(2) reports it, because Neovim joins relative names to
// that directory rather than to a logical path through symlinks.
//
// The steps follow the pinned source. FullName_save makes the name absolute
// by resolving its directory with realpath(3) while keeping the last
// component as written. resolve_symlink then follows the last component
// through up to 99 links, joining a relative link to the link's own
// directory, and makes the result absolute the same way. Whenever a step
// fails, Neovim keeps the name it had, and so does Resolve.
func Resolve(source, cwd string) Target {
	full, ok := fullName(source, cwd)
	if !ok {
		full = source
	}
	if resolved, ok := resolveSymlink(full, cwd); ok {
		full = resolved
	}

	return Target{Path: full}
}

// neovimAbsolute is Neovim's path_is_absolute on Unix, which also counts a
// leading ~ as absolute.
func neovimAbsolute(path string) bool {
	return strings.HasPrefix(path, "/") || strings.HasPrefix(path, "~")
}

// fullName follows path_to_absolute with force set. It reports false where
// Neovim's version fails and falls back to the name as written.
func fullName(name, cwd string) (string, bool) {
	// Split before the last slash. A name ending in ".." keeps it in the
	// directory part, so the directory resolves through it.
	p := strings.LastIndexByte(name, '/')
	if (p < 0 && name == "..") || (p >= 0 && name[p+1:] == "..") {
		p = len(name)
	}

	dir, rest := "", name
	switch {
	case p == len(name):
		dir, rest = name, ""
	case p >= 0:
		dir, rest = name[:p+1], name[p+1:]
	}

	base, ok := fullDir(dir, cwd)
	if !ok {
		return "", false
	}

	return appendPath(base, rest), true
}

// fullDir follows path_full_dir_name: the working directory for an empty
// directory, realpath(3) when it resolves, and otherwise a failure for an
// absolute directory or the working directory joined to a relative one.
func fullDir(dir, cwd string) (string, bool) {
	if dir == "" {
		return cwd, true
	}

	absolute := dir
	if !strings.HasPrefix(dir, "/") {
		// realpath(3) resolves a relative name against the working
		// directory. Joining without cleaning lets ".." apply after the
		// components before it resolve, as it does there.
		absolute = cwd + "/" + dir
	}
	if real, err := filepath.EvalSymlinks(absolute); err == nil {
		return real, true
	}

	if neovimAbsolute(dir) {
		return "", false
	}

	return appendPath(cwd, dir), true
}

// appendPath follows Neovim's append_path: nothing is appended for an empty
// name or ".", and a slash separates the parts unless path already ends in
// one.
func appendPath(path, name string) string {
	if name == "" || name == "." {
		return path
	}
	if path != "" && !strings.HasSuffix(path, "/") {
		path += "/"
	}

	return path + name
}

// maxLinks is resolve_symlink's depth limit: the 100th level is a loop.
const maxLinks = 99

// resolveSymlink follows resolve_symlink. It reports false when name is not
// a symlink, when a link cannot be read, and for a loop, all cases where
// Neovim keeps the name unchanged.
func resolveSymlink(name, cwd string) (string, bool) {
	current := name
	for depth := 1; ; depth++ {
		if depth > maxLinks {
			return "", false
		}

		target, err := os.Readlink(current)
		if err != nil {
			// A file that is not a link, or a name that does not exist,
			// ends the chain. At the first level nothing was resolved.
			if errors.Is(err, syscall.EINVAL) || errors.Is(err, syscall.ENOENT) {
				if depth == 1 {
					return "", false
				}
				break
			}
			return "", false
		}

		if neovimAbsolute(target) {
			current = target
		} else {
			current = current[:strings.LastIndexByte(current, '/')+1] + target
		}
	}

	return fullName(current, cwd)
}
