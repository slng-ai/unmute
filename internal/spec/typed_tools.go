package spec

import (
	"fmt"
	"maps"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/slng-ai/unmute/internal/stateschema"
)

// SplitHandler splits a `file.py:function` reference.
func SplitHandler(ref string) (file, function string) {
	file, function, _ = strings.Cut(ref, ":")
	return file, function
}

var handlerRef = regexp.MustCompile(`^[^:]+\.py:[A-Za-z_][A-Za-z0-9_]*$`)

// checkTools holds every tool file to the forms unmute-core reads.
func (p *Package) checkTools() error {
	for _, name := range slices.Sorted(maps.Keys(p.Tools)) {
		if err := p.checkTool(name, p.Tools[name]); err != nil {
			return err
		}
	}
	return nil
}

// checkTool holds one tool file to the forms the library reads.
func (p *Package) checkTool(name string, tool Tool) error {
	file := filepath.Join("tools", name+".yaml")
	if tool.Precheck != "" {
		if tool.Local == nil {
			return fmt.Errorf("%s: precheck runs before a local handler, and %s is a %s tool; remove precheck",
				p.Location(file, "precheck:"), name, tool.ExecutionKind())
		}
		if !handlerRef.MatchString(tool.Precheck) {
			return fmt.Errorf("%s: precheck %q must be written file.py:function, as tools/%s.py:check_%s",
				p.Location(file, "precheck:"), tool.Precheck, name, name)
		}
	}
	if tool.Local == nil {
		return nil
	}
	if !handlerRef.MatchString(tool.Local.Handler) {
		// Suggest the file the author already named, because one file may hold
		// many tools: an old handler is a file whose function is the tool's name.
		suggest := "tools/" + name + ".py"
		if handlerFile, _ := SplitHandler(tool.Local.Handler); strings.HasSuffix(handlerFile, ".py") {
			suggest = handlerFile
		}
		return fmt.Errorf("%s: handler %q must be written file.py:function, as %s:%s; its signature and return type are the tool's types",
			p.Location(file, "handler:"), tool.Local.Handler, suggest, name)
	}
	for _, key := range []string{"input", "output"} {
		if (key == "input" && tool.Input != nil) || (key == "output" && tool.Output != nil) {
			return fmt.Errorf("%s: a local tool takes no %s:; the handler's signature declares its input and its return type its output, so remove the %s: block",
				p.Location(file, key+":"), key, key)
		}
	}
	return nil
}

// localToolRefs lists the local tools to read through Python, by name, each
// with the bytes of the files its handler and precheck live in.
func (p *Package) localToolRefs() []stateschema.ToolRef {
	var out []stateschema.ToolRef
	for _, name := range slices.Sorted(maps.Keys(p.Tools)) {
		tool := p.Tools[name]
		if tool.Local == nil {
			continue
		}
		handlerFile, _ := SplitHandler(tool.Local.Handler)
		source := []byte(p.Handlers[handlerFile])
		if precheckFile, _ := SplitHandler(tool.Precheck); precheckFile != "" && precheckFile != handlerFile {
			source = append(source, p.Handlers[precheckFile]...)
		}
		out = append(out, stateschema.ToolRef{Name: name, Handler: tool.Local.Handler, Precheck: tool.Precheck, Source: source})
	}
	return out
}
