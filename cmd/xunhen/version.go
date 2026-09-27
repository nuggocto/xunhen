package main

import (
	"fmt"
	"runtime"
	"runtime/debug"
)

// Release and package builds stamp these with -ldflags "-X main.version=v...
// -X main.commit=...". A stamp wins over what the build recorded itself,
// because a package built from a source archive has no git metadata to
// record, and its commit would otherwise read as unknown.
var (
	version string
	commit  string
)

func versionText() string {
	name, revision := version, commit
	modified := false
	if info, ok := debug.ReadBuildInfo(); ok {
		if name == "" && info.Main.Version != "(devel)" {
			name = info.Main.Version
		}
		for _, setting := range info.Settings {
			switch setting.Key {
			case "vcs.revision":
				if revision == "" {
					revision = setting.Value
				}
			case "vcs.modified":
				modified = setting.Value == "true"
			}
		}
	}
	if name == "" {
		name = "devel"
	}
	if revision == "" {
		revision = "unknown"
	}
	// A tree with uncommitted changes is not the commit it names, stamped
	// or not.
	if modified {
		revision += " (modified)"
	}
	return fmt.Sprintf("xunhen %s\ncommit: %s\ngo: %s\n", name, revision, runtime.Version())
}
