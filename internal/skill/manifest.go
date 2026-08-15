package skill

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// manifestName is what makes a second install honest. A destination with no
// manifest is treated as not installed by Unmute, and is never overwritten
// without --force, because it is someone else's directory.
const manifestName = ".unmute-manifest.json"

// Manifest records what the last install wrote into one destination.
//
// Paths are forward-slashed in the file so a manifest written on Windows still
// matches on macOS. Conversion happens at the filesystem boundary and nowhere
// else.
type Manifest struct {
	Version string            `json:"version"`
	Files   map[string]string `json:"files"` // path -> lowercase hex SHA-256
}

// readManifest loads a destination's manifest. A missing manifest is not an
// error: it is the normal state before the first install, and the caller needs
// to tell it apart from a corrupt one.
func readManifest(dir string) (Manifest, bool, error) {
	raw, err := os.ReadFile(filepath.Join(dir, manifestName))
	if errors.Is(err, os.ErrNotExist) {
		return Manifest{Files: map[string]string{}}, false, nil
	}
	if err != nil {
		return Manifest{Files: map[string]string{}}, false, fmt.Errorf("reading %s: %w", filepath.Join(dir, manifestName), err)
	}
	var manifest Manifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return Manifest{Files: map[string]string{}}, false, fmt.Errorf("reading %s: %w", filepath.Join(dir, manifestName), err)
	}
	if manifest.Files == nil {
		manifest.Files = map[string]string{}
	}
	return manifest, true, nil
}

// writeManifest writes a destination's manifest, indented so a reader can diff
// it. The manifest never lists itself.
func writeManifest(dir string, manifest Manifest) error {
	delete(manifest.Files, manifestName)
	raw, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("writing %s: %w", filepath.Join(dir, manifestName), err)
	}
	raw = append(raw, '\n')
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating %s: %w", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, manifestName), raw, 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", filepath.Join(dir, manifestName), err)
	}
	return nil
}
