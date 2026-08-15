package spec

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
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
		// The shape check runs before the decoder so an old flat file reads as a
		// migration instruction instead of "unknown field" (V2).
		file := filepath.Join("tools", name+".yaml")
		content, err := pkg.readFile(file)
		if err != nil {
			return nil, err
		}
		block, err := checkToolShape(file, content)
		if err != nil {
			return nil, err
		}
		var tool Tool
		if err := pkg.decode(file, content, &tool); err != nil {
			return nil, err
		}
		if err := checkToolBlockBody(file, block, tool); err != nil {
			return nil, err
		}
		pkg.Tools[name] = tool
		// A local tool's handler travels with the package (code targets copy
		// it into the generated project), so load it like instructions.
		if tool.Local != nil {
			handler := tool.Local.Handler
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
	content, err := p.readFile(name)
	if err != nil {
		return err
	}
	return p.decode(name, content, out)
}

// readFile reads a package file and records it for Location lookups.
func (p *Package) readFile(name string) ([]byte, error) {
	content, err := readWithin(p.Root, name)
	if err != nil {
		return nil, err
	}
	p.files[name] = content
	return content, nil
}

func (p *Package) decode(name string, content []byte, out any) error {
	if err := yaml.UnmarshalWithOptions(content, out, yaml.Strict()); err != nil {
		return fmt.Errorf("%s: %s", name, redactSourceValues(err.Error()))
	}
	return nil
}

// sourceLine matches one line of the decoder's source excerpt: an optional `>`
// marker, the line number, the pipe, and then the author's own text.
var sourceLine = regexp.MustCompile(`^(\s*>?\s*\d+ \| )(.*)$`)

// redactSourceValues hides scalar values in the decoder's source excerpt.
//
// The excerpt is the best thing about these errors — it shows the line, its
// neighbours, and a caret under the column — and it was also the one place in
// the whole compiler that printed the author's file back at them. A package
// file is never supposed to hold a value, but the mistake that produces a YAML
// error is very often exactly that mistake:
//
//	secrets:
//	  OPENAI_API_KEY: sk-live-...
//
// which is a map where a sequence belongs, and which used to print both keys
// and both values to stderr, into CI logs, and into any bug report pasted from
// either (Wave C, 2026-08-15).
//
// Keys, indentation, and structure survive, because those are what a shape
// error is about. Values do not, because they are never what it is about.
func redactSourceValues(message string) string {
	lines := strings.Split(message, "\n")
	for i, line := range lines {
		match := sourceLine.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		lines[i] = match[1] + redactScalar(match[2])
	}
	return strings.Join(lines, "\n")
}

// redactScalar replaces the value half of one authored line, keeping the key,
// the indentation, and any list marker.
func redactScalar(text string) string {
	const hidden = "[value hidden]"
	body := strings.TrimLeft(text, " \t-")
	indent := text[:len(text)-len(body)]
	key, value, found := strings.Cut(body, ":")
	if !found {
		if strings.TrimSpace(body) == "" {
			return text
		}
		return indent + hidden // a bare sequence item is a value on its own
	}
	if strings.TrimSpace(value) == "" {
		return text // `key:` opening a block holds nothing to hide
	}
	return indent + key + ": " + hidden
}

func readWithin(root, name string) ([]byte, error) {
	packageRoot, err := os.OpenRoot(root)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	defer func() { _ = packageRoot.Close() }()
	file, err := packageRoot.Open(name)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	defer func() { _ = file.Close() }()
	content, err := io.ReadAll(file)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	return content, nil
}
