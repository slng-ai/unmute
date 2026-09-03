package generate

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"sort"
	"text/template"

	"github.com/slng-ai/unmute/internal/ir"
)

// The SLNG *target* driver. Next door, slng_router.go is the SLNG *model
// vendor*: the Context Router a package binds a think model to, on any target.
// The word does three jobs in this repository and this file is only the third
// one, which is why every message it produces opens with "slng target".
//
// Unlike the other two drivers this one emits no runnable project. It writes a
// deployment body for a platform that runs the agent, one tool body per tool
// that needs one, and a runbook. Nothing here opens a socket: unmute writes
// files and the voiceai CLI pushes them, which is the boundary the whole design
// rests on and internal/ir's TestNoSlngFileOpensASocket is the gate under it.
//
//go:embed templates/slng_v1/*.tmpl
var slngV1Templates embed.FS

// GenerateSlng writes build/<target>/: agent.json, tools/<name>.json for every
// tool that needs a body, and README.md.
func GenerateSlng(agent *ir.Agent, tgt ir.Target) (Artifact, error) {
	built, err := buildSlng(agent, tgt)
	if err != nil {
		return Artifact{}, err
	}
	body, err := marshalSlng(built.Body)
	if err != nil {
		return Artifact{}, fmt.Errorf("agent body: %w", err)
	}
	// One file, and the reason there is only one is the whole shape of this
	// target now: the body references tools SLNG already owns and creates none,
	// so there is no tool body to write beside it.
	files := []File{{Path: "agent.json", Content: body}}
	runbook, err := renderSlngRunbook(built.Runbook)
	if err != nil {
		return Artifact{}, err
	}
	files = append(files, File{Path: "README.md", Content: runbook})
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return Artifact{
		Kind:     BodyTarget,
		Files:    files,
		Notes:    GenerateReport{Notes: built.Notes, Warnings: built.Warnings},
		Requires: built.Requires,
	}, nil
}

// marshalSlng writes one JSON document. Indented and newline-terminated because
// a person reads these before pushing them, and HTML escaping off because a
// prompt containing `&` or `<` is prose, not markup, and & in a system
// prompt is a change to what the agent was told.
func marshalSlng(value any) ([]byte, error) {
	var out bytes.Buffer
	encoder := json.NewEncoder(&out)
	encoder.SetIndent("", "  ")
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// marshalSortedMap encodes a map with its keys in sorted order. encoding/json
// already sorts map keys, so this exists for the other half: a nil map of a
// named type encodes as {} rather than null, which matters because SLNG reads
// the declared variable set from the union of two maps' keys and null there
// says something different from empty.
func marshalSortedMap[V any](values map[string]V) ([]byte, error) {
	if values == nil {
		return []byte("{}"), nil
	}
	return json.Marshal(map[string]V(values))
}

func renderSlngRunbook(data slngRunbook) ([]byte, error) {
	tmpl, err := template.New("README.md.tmpl").ParseFS(slngV1Templates, "templates/slng_v1/README.md.tmpl")
	if err != nil {
		return nil, fmt.Errorf("slng runbook template: %w", err)
	}
	var out bytes.Buffer
	if err := tmpl.Execute(&out, data); err != nil {
		return nil, fmt.Errorf("slng runbook: %w", err)
	}
	return out.Bytes(), nil
}
