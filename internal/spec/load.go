package spec

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/goccy/go-yaml"
)

// Load strictly decodes a v1 package without resolving references.
func Load(dir string) (*Package, error) {
	root, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("package path: %w", err)
	}
	pkg := &Package{
		Root:     root,
		Tools:    make(map[string]Tool),
		Markdown: make(map[string]string),
		Handlers: make(map[string]string),
		files:    make(map[string][]byte),
	}
	if err := pkg.readYAML("agent.yaml", &pkg.Agent); err != nil {
		return nil, err
	}

	for _, name := range pkg.Agent.Tools {
		if filepath.Base(name) != name {
			return nil, fmt.Errorf("agent.yaml: invalid tool name %q", name)
		}
		var tool Tool
		if err := pkg.readYAML(filepath.Join("tools", name+".yaml"), &tool); err != nil {
			return nil, err
		}
		pkg.Tools[name] = tool
		// A local tool's handler travels with the package (code targets copy
		// it into the generated project), so load it like instructions.
		if tool.Execution == "local" {
			handler := tool.Handler
			if handler == "" {
				handler = filepath.Join("tools", name+".py")
			}
			content, err := readWithin(root, handler)
			if err != nil {
				return nil, err
			}
			pkg.Handlers[handler] = string(content)
		}
	}

	var targets TargetsFile
	if err := pkg.readYAML("targets.yaml", &targets); err != nil {
		return nil, err
	}
	pkg.Targets = targets.Targets

	paths := make(map[string]bool)
	for _, agent := range pkg.Agent.Agents {
		paths[agent.Instructions] = true
	}
	for _, task := range pkg.Agent.Tasks {
		paths[task.Instructions] = true
	}
	for path := range paths {
		if path == "" {
			continue
		}
		content, err := readWithin(root, path)
		if err != nil {
			return nil, err
		}
		pkg.Markdown[path] = string(content)
	}
	return pkg, nil
}

func (p *Package) readYAML(name string, out any) error {
	content, err := readWithin(p.Root, name)
	if err != nil {
		return err
	}
	p.files[name] = content
	if err := yaml.UnmarshalWithOptions(content, out, yaml.Strict()); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	return nil
}

func readWithin(root, name string) ([]byte, error) {
	path := filepath.Join(root, filepath.Clean(name))
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return nil, fmt.Errorf("%s: path escapes package", name)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	return content, nil
}
