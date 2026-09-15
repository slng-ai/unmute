package scaffold

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/goccy/go-yaml"
	"github.com/slng-ai/unmute/internal/spec"
)

// WriteDraft creates an intentionally incomplete package without choosing any
// runtime or model. Normal Write and Preflight retain their complete defaults.
func WriteDraft(dir string, manifest []byte) ([]string, error) {
	name := AgentNameFrom(filepath.Base(filepath.Clean(dir)))
	if name == "" {
		return nil, fmt.Errorf("%q cannot be an agent name; use letters, digits and hyphens", dir)
	}
	if _, err := spec.ParseManifest(manifest); err != nil {
		return nil, err
	}
	if entries, err := os.ReadDir(dir); err == nil {
		if len(entries) > 0 {
			return nil, fmt.Errorf("%s: %w", dir, ErrExists)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	agent := spec.AgentFile{
		Version: 1, Name: name, Manifest: "manifest", EntryAgent: "assistant",
		Agents:   map[string]spec.AgentDef{"assistant": {Instructions: "instructions.md"}},
		Channels: map[string]spec.Channel{},
	}
	agentYAML, err := yaml.Marshal(agent)
	if err != nil {
		return nil, err
	}
	targetsYAML, err := yaml.Marshal(spec.TargetsFile{Targets: map[string]spec.Target{}})
	if err != nil {
		return nil, err
	}
	ignore, err := templates.ReadFile("templates/gitignore.tmpl")
	if err != nil {
		return nil, err
	}
	files := []struct {
		name string
		data []byte
	}{
		{"agent.yaml", agentYAML}, {"targets.yaml", targetsYAML},
		{"instructions.md", []byte(DefaultInstructions + "\n")}, {".gitignore", ignore},
		{".env.example", []byte{}}, {"manifest", manifest},
	}
	// Stage the complete file set so a failed write cannot leave a partial draft.
	parent := filepath.Dir(filepath.Clean(dir))
	if err := os.MkdirAll(parent, 0755); err != nil {
		return nil, err
	}
	staging, err := os.MkdirTemp(parent, ".unmute-draft-")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(staging) }()
	var created []string
	for _, file := range files {
		if err := os.WriteFile(filepath.Join(staging, file.name), file.data, 0644); err != nil {
			return nil, err
		}
		created = append(created, filepath.Join(dir, file.name))
	}
	// Windows cannot rename a directory onto an existing empty directory.
	// Remove only an empty destination; a concurrent populated one is refused.
	existing, err := os.Lstat(dir)
	if err == nil {
		if !existing.IsDir() {
			return nil, fmt.Errorf("%s: %w", dir, ErrExists)
		}
		if err := os.Remove(dir); err != nil {
			return nil, fmt.Errorf("%s: %w", dir, err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if err := os.Rename(staging, dir); err != nil {
		if existing != nil {
			if restoreErr := os.Mkdir(dir, existing.Mode().Perm()); restoreErr != nil {
				return nil, errors.Join(err, restoreErr)
			}
		}
		return nil, fmt.Errorf("write draft: %w", err)
	}
	slices.Sort(created)
	return created, nil
}
