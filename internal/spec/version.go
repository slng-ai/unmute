package spec

import (
	"fmt"
	"maps"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"

	"github.com/slng-ai/unmute/internal/stateschema"
)

// Version2 is the agent.yaml version whose keys map one to one onto
// unmute-core's typed concepts. A field tagged `since:"2"` is legal only there.
const Version2 = 2

// SplitHandler splits a `file.py:function` reference. A version 1 handler is a
// file alone, so its function is empty.
func SplitHandler(ref string) (file, function string) {
	file, function, _ = strings.Cut(ref, ":")
	return file, function
}

var handlerRef = regexp.MustCompile(`^[^:]+\.py:[A-Za-z_][A-Za-z0-9_]*$`)

// checkVersionKeys refuses a version 2 key in any other version, and holds a
// version 2 local tool to its handler form. One reflection walk over the
// `since:"2"` tags, so a new key cannot be added and forgotten here.
func (p *Package) checkVersionKeys() error {
	if p.Agent.Version != Version2 {
		if key := firstV2Key(reflect.ValueOf(p.Agent)); key != "" {
			return fmt.Errorf("%s: %s: is a version 2 key; write version: 2", p.Location("agent.yaml", key+":"), key)
		}
		for _, name := range slices.Sorted(maps.Keys(p.Tools)) {
			file := filepath.Join("tools", name+".yaml")
			tool := p.Tools[name]
			if key := firstV2Key(reflect.ValueOf(tool)); key != "" {
				return fmt.Errorf("%s: %s: is a version 2 key; write version: 2", p.Location(file, key+":"), key)
			}
			if tool.Local != nil && strings.Contains(tool.Local.Handler, ":") {
				return fmt.Errorf("%s: handler %s names a function, which is a version 2 form; write version: 2, or name the file alone",
					p.Location(file, "handler:"), tool.Local.Handler)
			}
		}
		return nil
	}
	for _, name := range slices.Sorted(maps.Keys(p.Tools)) {
		if err := p.checkV2Tool(name, p.Tools[name]); err != nil {
			return err
		}
	}
	return nil
}

// checkV2Tool holds one version 2 tool file to the forms the library reads.
func (p *Package) checkV2Tool(name string, tool Tool) error {
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
		return fmt.Errorf("%s: handler %q must be written file.py:function, as tools/%s.py:%s; its signature and return type are the tool's types",
			p.Location(file, "handler:"), tool.Local.Handler, name, name)
	}
	for _, key := range []string{"input", "output"} {
		if (key == "input" && tool.Input != nil) || (key == "output" && tool.Output != nil) {
			return fmt.Errorf("%s: a version 2 local tool takes no %s:; the handler's signature declares its input and its return type its output, so remove the %s: block",
				p.Location(file, key+":"), key, key)
		}
	}
	return nil
}

// firstV2Key returns the YAML name of the first set `since:"2"` field under v.
func firstV2Key(v reflect.Value) string {
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface:
		if !v.IsNil() {
			return firstV2Key(v.Elem())
		}
	case reflect.Slice, reflect.Array:
		for i := range v.Len() {
			if key := firstV2Key(v.Index(i)); key != "" {
				return key
			}
		}
	case reflect.Map:
		for _, key := range v.MapKeys() {
			if found := firstV2Key(v.MapIndex(key)); found != "" {
				return found
			}
		}
	case reflect.Struct:
		for i := range v.NumField() {
			field := v.Type().Field(i)
			if !field.IsExported() {
				continue
			}
			if field.Tag.Get("since") == "2" && !v.Field(i).IsZero() {
				return strings.Split(field.Tag.Get("yaml"), ",")[0]
			}
			if key := firstV2Key(v.Field(i)); key != "" {
				return key
			}
		}
	}
	return ""
}

// localToolRefs lists the version 2 local tools to read through Python, by
// name, each with the bytes of the files its handler and precheck live in.
func (p *Package) localToolRefs() []stateschema.ToolRef {
	if p.Agent.Version != Version2 {
		return nil
	}
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
