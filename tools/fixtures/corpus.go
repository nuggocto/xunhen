package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"syscall"
)

const maxFixtureBytes = 2 << 20

type corpus struct {
	Schema   int             `json:"schema"`
	Producer producer        `json:"producer"`
	Fixtures []fixtureDigest `json:"fixtures"`
}

type producer struct {
	Version           string `json:"version"`
	VersionOutput     string `json:"version_output"`
	BinarySHA256      string `json:"binary_sha256"`
	SourceRevision    string `json:"source_revision"`
	PackagingRevision string `json:"packaging_revision"`
	Package           string `json:"package"`
	Profile           string `json:"profile"`
}

type fixtureDigest struct {
	Name   string            `json:"name"`
	SHA256 map[string]string `json:"sha256"`
}

type oracle struct {
	Name           string  `json:"name"`
	Purpose        string  `json:"purpose"`
	Classification string  `json:"classification"`
	Recipe         request `json:"recipe"`
	Created        created `json:"created"`
	Loaded         loaded  `json:"loaded"`
}

type request struct {
	Mode        string `json:"mode"`
	Encoding    string `json:"encoding"`
	FileFormat  string `json:"fileformat"`
	UndoLevels  int    `json:"undo_levels"`
	Steps       []step `json:"steps"`
	Visit       []int  `json:"visit"`
	Pruned      []int  `json:"pruned"`
	Persistence string `json:"persistence"`
}

type created struct {
	Anchor       snapshot      `json:"anchor"`
	Tree         undoTree      `json:"tree"`
	Observations []observation `json:"observations"`
	SavedHex     string        `json:"saved_hex"`
}

type loaded struct {
	Anchor           snapshot   `json:"anchor"`
	Tree             undoTree   `json:"tree"`
	States           []snapshot `json:"states"`
	MismatchRejected bool       `json:"mismatch_rejected"`
	MismatchMessage  string     `json:"mismatch_message"`
}

type observation struct {
	Op    string   `json:"op"`
	State snapshot `json:"state"`
}

type snapshot struct {
	Seq          int      `json:"seq"`
	LinesHex     []string `json:"lines_hex"`
	EndOfLine    bool     `json:"endofline"`
	FileFormat   string   `json:"fileformat"`
	FileEncoding string   `json:"fileencoding"`
}

type undoTree struct {
	SeqLast  int         `json:"seq_last"`
	SeqCur   int         `json:"seq_cur"`
	TimeCur  int64       `json:"time_cur"`
	SaveLast int         `json:"save_last"`
	SaveCur  int         `json:"save_cur"`
	Synced   int         `json:"synced"`
	Entries  []treeEntry `json:"entries"`
}

type treeEntry struct {
	Seq     int         `json:"seq"`
	Time    int64       `json:"time"`
	Save    int         `json:"save,omitempty"`
	NewHead int         `json:"newhead,omitempty"`
	CurHead int         `json:"curhead,omitempty"`
	Alt     []treeEntry `json:"alt,omitempty"`
}

func digest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func readBounded(path string, limit int64) (data []byte, err error) {
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	defer func() {
		err = errors.Join(err, file.Close())
	}()

	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("expected a regular file")
	}
	if info.Size() > limit {
		return nil, errors.New("fixture file exceeds its size limit")
	}

	data, err = io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, errors.New("fixture file exceeds its size limit")
	}
	return data, nil
}

func readJSON(path string, dst any) error {
	data, err := readBounded(path, maxFixtureBytes)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, dst)
}

func writeJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0600)
}
