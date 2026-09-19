package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

func generate(ctx context.Context, nvim, out string) (err error) {
	nvim, err = resolveProducer(nvim)
	if err != nil {
		return err
	}

	work, err := os.MkdirTemp("", "xunhen-fixtures-")
	if err != nil {
		return err
	}
	defer func() {
		err = errors.Join(err, os.RemoveAll(work))
	}()

	if err := prepareWork(work); err != nil {
		return err
	}
	version, err := readProducerVersion(ctx, nvim, work)
	if err != nil {
		return err
	}
	if err := os.Mkdir(out, 0755); err != nil {
		return fmt.Errorf("create new output directory: %w", err)
	}

	index := corpus{
		Schema: 1,
		Producer: producer{
			Version:           producerVersion,
			VersionOutput:     version,
			BinarySHA256:      producerDigest,
			SourceRevision:    sourceRevision,
			PackagingRevision: packagingRevision,
			Package:           "Arch neovim 0.12.5-1",
			Profile:           "neovim-v3-linux-amd64-le-lp64",
		},
	}

	for _, fixture := range corpusCases() {
		entry, err := generateCase(ctx, nvim, work, out, fixture)
		if err != nil {
			return fmt.Errorf("%s: %w", fixture.Name, err)
		}
		index.Fixtures = append(index.Fixtures, entry)
	}
	return writeJSON(filepath.Join(out, "corpus.json"), index)
}

func generateCase(ctx context.Context, nvim, work, out string, fixture fixtureCase) (fixtureDigest, error) {
	var entry fixtureDigest
	dir := filepath.Join(work, fixture.Name)

	if err := os.Mkdir(dir, 0700); err != nil {
		return entry, err
	}
	if err := prepareWork(dir); err != nil {
		return entry, err
	}
	if err := os.WriteFile(filepath.Join(dir, "source.bin"), []byte(fixture.Initial), 0600); err != nil {
		return entry, err
	}

	recipe := fixtureRequest(fixture)
	if err := runDriver(ctx, nvim, dir, recipe); err != nil {
		return entry, err
	}

	var made created
	if err := readJSON(filepath.Join(dir, "created.json"), &made); err != nil {
		return entry, err
	}
	if err := checkSavedSource(dir, fixture); err != nil {
		return entry, err
	}
	undo, err := readBounded(filepath.Join(dir, "history.undo"), maxFixtureBytes)
	if err != nil {
		return entry, err
	}

	// :wundo can refer to unsaved text. Give the next process the matching base.
	if err := os.WriteFile(filepath.Join(dir, "source.bin"), []byte(fixture.Final), 0600); err != nil {
		return entry, err
	}
	loadRequest := recipe
	loadRequest.Mode = "load"
	loadRequest.Steps = nil

	if err := runDriver(ctx, nvim, dir, loadRequest); err != nil {
		return entry, err
	}
	var reopened loaded
	if err := readJSON(filepath.Join(dir, "loaded.json"), &reopened); err != nil {
		return entry, err
	}

	result := oracle{
		Name:           fixture.Name,
		Purpose:        fixture.Purpose,
		Classification: fixture.classification(),
		Recipe:         recipe,
		Created:        made,
		Loaded:         reopened,
	}
	if err := validateOracle(fixture, result, undo); err != nil {
		return entry, err
	}

	after, err := readBounded(filepath.Join(dir, "history.undo"), maxFixtureBytes)
	if err != nil {
		return entry, err
	}
	if !bytes.Equal(undo, after) {
		return entry, errors.New("oracle replay changed the persisted undo file")
	}
	return saveFixture(out, fixture, result, undo)
}

func fixtureRequest(fixture fixtureCase) request {
	recipe := request{
		Mode:        "create",
		Encoding:    fixture.Encoding,
		FileFormat:  fixture.FileFormat,
		UndoLevels:  fixture.UndoLevels,
		Steps:       fixture.Steps,
		Visit:       fixture.Visit,
		Pruned:      fixture.Pruned,
		Persistence: fixture.Persistence,
	}

	if recipe.Encoding == "" {
		recipe.Encoding = "utf-8"
	}
	if recipe.FileFormat == "" {
		recipe.FileFormat = "unix"
	}
	if recipe.UndoLevels == 0 {
		recipe.UndoLevels = 100
	}
	if recipe.Pruned == nil {
		recipe.Pruned = []int{}
	}
	if recipe.Persistence == "" {
		recipe.Persistence = "automatic"
	}
	return recipe
}

func checkSavedSource(dir string, fixture fixtureCase) error {
	saved, err := readBounded(filepath.Join(dir, "source.bin"), maxFixtureBytes)
	if err != nil {
		return err
	}

	want := fixture.Final
	if fixture.Persistence == "explicit" {
		want = fixture.Initial
	}
	if !bytes.Equal(saved, []byte(want)) {
		return fmt.Errorf("saved bytes = %x, want %x", saved, want)
	}
	return nil
}

func saveFixture(out string, fixture fixtureCase, result oracle, undo []byte) (fixtureDigest, error) {
	entry := fixtureDigest{
		Name:   fixture.Name,
		SHA256: make(map[string]string),
	}

	dir := filepath.Join(out, fixture.Name)
	if err := os.Mkdir(dir, 0755); err != nil {
		return entry, err
	}
	encoded, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return entry, err
	}

	files := map[string][]byte{
		"initial.bin":  []byte(fixture.Initial),
		"base.bin":     []byte(fixture.Final),
		"history.undo": undo,
		"oracle.json":  append(encoded, '\n'),
	}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0644); err != nil {
			return entry, err
		}
		entry.SHA256[name] = digest(data)
	}
	return entry, nil
}
