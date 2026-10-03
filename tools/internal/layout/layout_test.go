package layout

import (
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// link matches the target of an inline Markdown link.
var link = regexp.MustCompile(`\]\(([^)\s]+)\)`)

// Someone who unpacks the binary archive has only the files in it. A
// relative link from a shipped document must name another shipped file;
// anything else must be a full URL.
func TestShippedDocsLinkOnlyToShippedFiles(t *testing.T) {
	t.Parallel()

	for _, name := range BinaryDocs {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			data, err := os.ReadFile(filepath.Join("..", "..", "..", filepath.FromSlash(name)))
			if err != nil {
				t.Fatal(err)
			}
			for _, m := range link.FindAllStringSubmatch(string(data), -1) {
				target, _, _ := strings.Cut(m[1], "#")
				if target == "" || strings.Contains(target, "://") || strings.HasPrefix(target, "mailto:") {
					continue
				}
				resolved := path.Join(path.Dir(name), target)
				if !slices.Contains(BinaryDocs, resolved) {
					t.Errorf("links to %s, which the binary archive does not hold", m[1])
				}
			}
		})
	}
}
