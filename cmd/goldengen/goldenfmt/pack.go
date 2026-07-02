package goldenfmt

import (
	"os"
	"path/filepath"
	"sort"

	"github.com/avangerus/kalita/internal/dsl"
)

// LoadPack compiles every *.dsl file in dir as one pack — the same convention
// cmd/kalita's own loadPack uses for a genesis pack directory (a directory of
// sources, not a single file). It is duplicated here in miniature rather than
// imported from cmd/kalita because cmd/kalita is a package main and cannot be
// imported; keeping it in goldenfmt lets both the generator and the replay
// gate share one implementation.
func LoadPack(dir string) (*dsl.Model, []*dsl.Error, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil, err
	}
	files := map[string]string{}
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".dsl" {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, nil, err
		}
		files[e.Name()] = string(raw)
	}
	model, errs := dsl.Compile(files)
	return model, errs, nil
}

// WritePack writes a pack's sources to dir, one file per map entry — the
// generator's genesis pack, committed alongside journal.jsonl so replay never
// depends on anything outside testdata/golden/<ver>/ (LLD-03 §3: "самодостаточный
// тестовый пак внутри testdata"). Definitions the scenario evolves later live
// only inside the journal itself (DefinitionProposed/Applied payloads carry
// the full pack source) — this genesis pack is only the seed, matching how a
// real node's --pack directory works (internal/engine/journal.go).
func WritePack(dir string, files map[string]string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(files[name]), 0o644); err != nil {
			return err
		}
	}
	return nil
}
