package main

import (
	"fmt"
	"runtime"
	"runtime/debug"
)

// Release builds may set version with -ldflags "-X main.version=v...".
var version string

func versionText() string {
	name := version
	commit := "unknown"
	modified := false
	if info, ok := debug.ReadBuildInfo(); ok {
		if name == "" && info.Main.Version != "(devel)" {
			name = info.Main.Version
		}
		for _, setting := range info.Settings {
			switch setting.Key {
			case "vcs.revision":
				commit = setting.Value
			case "vcs.modified":
				modified = setting.Value == "true"
			}
		}
	}
	if name == "" {
		name = "devel"
	}
	if modified {
		commit += " (modified)"
	}
	return fmt.Sprintf("xunhen %s\ncommit: %s\ngo: %s\n", name, commit, runtime.Version())
}
