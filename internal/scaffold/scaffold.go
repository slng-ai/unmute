// Package scaffold renders the files `unmute init` writes: a v1 agent package
// (agent.yaml + instructions.md + targets.yaml). Templates live in templates/
// and are embedded; each runs through text/template with [[ ]] delimiters, so
// the voice-agent {{ }} variable syntax in prompts passes through verbatim.
package scaffold

import (
	"bytes"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"text/template"

	"github.com/goccy/go-yaml"
)

//go:embed templates
var templates embed.FS

// ErrExists is returned when the target directory already exists and is not
// empty. init refuses rather than overwrite an existing agent.
var ErrExists = errors.New("directory already exists and is not empty")

const (
	DefaultGreeting     = "Hi, thanks for calling. How can I help you today?"
	DefaultInstructions = "You are a helpful voice assistant. This is a phone call, so keep every answer to one or two short sentences."
)

// Data is the v1 agent configuration rendered by the scaffold templates.
type Data struct {
	Name         string
	Greeting     string
	Instructions string
}

func (d Data) withDefaults() Data {
	if d.Greeting == "" {
		d.Greeting = DefaultGreeting
	}
	if d.Instructions == "" {
		d.Instructions = DefaultInstructions
	}
	return d
}

// Write renders every embedded template into dir/. Returns the created file
// paths in deterministic (lexical) order. Refuses if dir already exists and is
// non-empty (no overwrite, no partial write).
func Write(dir string, d Data) ([]string, error) {
	if entries, err := os.ReadDir(dir); err == nil && len(entries) > 0 {
		return nil, fmt.Errorf("%s: %w", dir, ErrExists)
	}

	var created []string
	d = d.withDefaults()

	// WalkDir visits in lexical order → deterministic output.
	err := fs.WalkDir(templates, "templates", func(p string, e fs.DirEntry, err error) error {
		if err != nil || e.IsDir() {
			return err
		}
		rel := strings.TrimSuffix(strings.TrimPrefix(p, "templates/"), ".tmpl")
		out := filepath.Join(dir, rel)

		raw, err := templates.ReadFile(p)
		if err != nil {
			return err
		}
		tmpl, err := template.New(rel).Funcs(template.FuncMap{
			"quote": strconv.Quote,
			"yaml":  yamlScalar,
		}).Delims("[[", "]]").Parse(string(raw))
		if err != nil {
			return err
		}
		var buf bytes.Buffer
		if err := tmpl.Execute(&buf, d); err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(out, buf.Bytes(), 0o644); err != nil {
			return err
		}
		created = append(created, out)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("scaffold: %w", err)
	}
	return created, nil
}

func yamlScalar(value string) (string, error) {
	var parsed map[string]any
	if err := yaml.Unmarshal([]byte("value: "+value+"\n"), &parsed); err == nil && parsed["value"] == value {
		return value, nil
	}
	encoded, err := yaml.Marshal(value)
	return strings.TrimSpace(string(encoded)), err
}
