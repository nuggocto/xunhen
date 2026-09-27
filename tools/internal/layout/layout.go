// Package layout names the release artifacts and lists what the binary
// archive holds. The release builder writes this layout and the artifact
// verifier checks it, so both read it from here.
package layout

import "strings"

// BinaryDocs are the files the binary archive carries beside the
// executable, named as in the repository. Nothing else goes in: no
// fixtures, caches, or development tools.
var BinaryDocs = []string{
	"CHANGELOG.md",
	"LICENSE",
	"README.md",
	"THIRD_PARTY_NOTICES.txt",
	"docs/install.md",
	"docs/troubleshooting.md",
	"docs/usage.md",
}

// Executable is the executable's name inside the binary archive.
const Executable = "xunhen"

// BinaryArchive is the top directory of the binary archive for a version
// such as "1.0.0" or "v1.0.0"; the archive file adds ".tar.gz".
func BinaryArchive(version string) string {
	return "xunhen_" + strings.TrimPrefix(version, "v") + "_linux_amd64"
}

// SourceArchive is the top directory of the source archive.
func SourceArchive(version string) string {
	return "xunhen_" + strings.TrimPrefix(version, "v") + "_source"
}
