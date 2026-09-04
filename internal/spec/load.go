package spec

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
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
		Documents:   make(map[string][]byte),
		Mirrors:     make(map[string]Mirror),
		MirrorBytes: make(map[string][]byte),
		files:       make(map[string][]byte),
	}
	if err := pkg.readYAML("agent.yaml", &pkg.Agent); err != nil {
		return nil, err
	}
	pkg.readVariableOrder()

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
		// A hosted tool's definition travels with the package too, for the same
		// reason: the code targets run the mirrored module, and every target
		// checks the pin with no network.
		if tool.Slng != nil {
			if err := pkg.readMirror(name); err != nil {
				return nil, err
			}
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

	if err := pkg.flattenTasks(); err != nil {
		return nil, err
	}

	paths := make(map[string]bool)
	for _, agent := range pkg.Agent.Agents {
		paths[agent.Instructions] = true
	}
	for _, task := range pkg.Tasks {
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
	if err := pkg.readKnowledge(); err != nil {
		return nil, err
	}
	return pkg, nil
}

// flattenTasks fills the two derived maps from the nested shape.
//
// The nesting is for the reader: an agent's block says what that agent can do
// without a jump to a catalog. Every check downstream wants the opposite view, a
// name resolved to one definition, so the two views are built once here rather
// than walked from fifteen places.
//
// Agent-name order, then authored order within each agent, so a package with a
// duplicate name always names the same two agents.
func (p *Package) flattenTasks() error {
	p.Tasks = make(map[string]Task)
	p.Callables = make(map[string]Callable)
	owners := make(map[string]string)
	for _, agent := range slices.Sorted(maps.Keys(p.Agent.Agents)) {
		for _, item := range p.Agent.Agents[agent].Tasks {
			if item.Task == nil {
				continue // a bare name is a reference, resolved by ir.Build
			}
			task := *item.Task
			if owner, taken := owners[task.Name]; taken {
				return fmt.Errorf(
					"%s: task %q is defined by agent %q and again by agent %q. A task name is one name across the package: keep one definition and let the other agent name it, %q",
					p.Location("agent.yaml", "name: "+task.Name), task.Name, owner, agent, "- "+task.Name)
			}
			owners[task.Name] = agent
			p.Tasks[task.Name] = task
			if task.When == "" {
				continue // a definition only, valid solely as a task group step
			}
			p.Callables[task.Name] = Callable{
				Task:     task.Name,
				When:     task.When,
				Announce: task.Announce,
				Requires: task.Requires,
				Assign:   task.Assign,
			}
		}
	}
	for _, agent := range slices.Sorted(maps.Keys(p.Agent.Agents)) {
		for _, name := range p.Agent.Agents[agent].TaskGroups {
			group := p.Agent.TaskGroups[name]
			p.Callables[name] = Callable{
				Group: name, When: group.When, Announce: group.Announce, Requires: group.Requires,
			}
		}
	}
	return nil
}

// KnowledgeExtensions are the document types a knowledge base reads. The emitted
// project reads them with LlamaIndex's SimpleDirectoryReader, which handles all
// three; anything else in the folder is not an input, so it is neither read nor
// copied into the artifact.
var KnowledgeExtensions = []string{".txt", ".md", ".pdf"}

// readKnowledge reads every supported document in every declared knowledge base
// into Documents, keyed by its artifact path.
//
// A folder that does not exist, or that holds no supported file, is left empty
// here rather than reported here: ir.Validate owns that message (FR-009), and it
// names the folder and the base. Load stopping first would put an authoring rule
// in the wrong package and produce a worse message.
func (p *Package) readKnowledge() error {
	root, err := os.OpenRoot(p.Root)
	if err != nil {
		return fmt.Errorf("package path: %w", err)
	}
	defer func() { _ = root.Close() }()
	for name, base := range p.Agent.Knowledge {
		if base.Documents == "" {
			continue // required-field message belongs to validation
		}
		entries, err := fs.ReadDir(root.FS(), filepath.ToSlash(base.Documents))
		if err != nil {
			continue // missing or unreadable folder: FR-009, reported by validation
		}
		for _, entry := range entries {
			if entry.IsDir() || !slices.Contains(KnowledgeExtensions, strings.ToLower(filepath.Ext(entry.Name()))) {
				continue
			}
			content, err := readWithin(p.Root, filepath.Join(base.Documents, entry.Name()))
			if err != nil {
				return fmt.Errorf("knowledge base %q: %w", name, err)
			}
			p.Documents[path.Join("knowledge", name, entry.Name())] = content
		}
	}
	return nil
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
// readMirror reads one hosted tool's committed mirror: the sidecar and, for a
// `code` tool, the module beside it.
//
// A mirror that is not there is left absent rather than reported here, the way
// a missing knowledge folder is: ir.Validate owns that message, and it is the
// first one an author hits, because `slng: {}` before the first pull is exactly
// this state. Load stopping first would put an authoring rule in the wrong
// package and produce a worse message.
//
// A mirror that IS there and cannot be parsed is a different thing and is
// reported, because nobody wrote it by hand and a corrupt one is not a
// migration.
func (p *Package) readMirror(name string) error {
	sidecar, module := MirrorPaths(name)
	content, err := readWithin(p.Root, sidecar)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}
	var mirror Mirror
	if err := json.Unmarshal(content, &mirror); err != nil {
		return fmt.Errorf("%s: %w: it is written by `unmute pull` and not by hand, so run the pull again", sidecar, err)
	}
	// The pin covers the sidecar and the module together, so the bytes both were
	// read from are kept rather than re-encoded. A round trip through the struct
	// would hash a normalisation of the file instead of the file.
	pinned := content
	if code, err := readWithin(p.Root, module); err == nil {
		mirror.Code = string(code)
		pinned = append(append([]byte{}, content...), code...)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	p.Mirrors[name] = mirror
	p.MirrorBytes[name] = pinned
	return nil
}

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
		// A pair list item knows its line but not its file, so the two are joined
		// here. Everything else keeps goccy's own excerpt, which is better than
		// anything we would write: it shows the line, its neighbours and a caret.
		var pair *PairError
		if errors.As(err, &pair) {
			return fmt.Errorf("%s:%d: %s", name, pair.Line, pair.Msg)
		}
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
