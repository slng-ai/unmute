package spec

import (
	"errors"
	"fmt"
	"io/fs"
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
		Root:        root,
		Tools:       make(map[string]Tool),
		Connections: make(map[string]Connection),
		Markdown:    make(map[string]string),
		Handlers:    make(map[string]string),
		files:       make(map[string][]byte),
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
	if err := pkg.readConnections(); err != nil {
		return nil, err
	}

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

func (p *Package) readConnections() error {
	root, err := os.OpenRoot(p.Root)
	if err != nil {
		return fmt.Errorf("package path: %w", err)
	}
	defer func() { _ = root.Close() }()
	entries, err := fs.ReadDir(root.FS(), "connections")
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("connections: %w", err)
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".yaml" {
			continue
		}
		name := strings.TrimSuffix(entry.Name(), ".yaml")
		var connection Connection
		if err := p.readYAML(filepath.Join("connections", entry.Name()), &connection); err != nil {
			return err
		}
		p.Connections[name] = connection
	}
	return nil
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
	packageRoot, err := os.OpenRoot(root)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	defer func() { _ = packageRoot.Close() }()
	content, err := packageRoot.ReadFile(name)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	return content, nil
}
