package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestReadRegularRejectsChangedInput(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		mutate func(*testing.T, string)
	}{
		{name: "size changed", mutate: func(t *testing.T, path string) {
			if err := os.WriteFile(path, []byte("longer"), 0600); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "mtime changed", mutate: func(t *testing.T, path string) {
			stamp := time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC)
			if err := os.Chtimes(path, stamp, stamp); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "base")
			if err := os.WriteFile(path, []byte("a"), 0600); err != nil {
				t.Fatal(err)
			}
			err := readRegular(t.Context(), path, "base", 1024, func(file *os.File) error {
				if _, err := io.ReadAll(file); err != nil {
					return err
				}
				tt.mutate(t, path)
				return nil
			})
			if err == nil || !strings.Contains(err.Error(), "changed while reading") {
				t.Fatalf("changed input accepted: %v", err)
			}
		})
	}
}
