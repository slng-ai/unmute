package scaffold_test

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// This is the gate for the one failure mode no tool in this repository can
// catch: a Go method reached only from a text/template.
//
// Deleting one compiles, passes `go vet`, passes golangci-lint, and passes
// `deadcode -test ./...`, because none of them can see through
// template.Execute's reflection. It then fails at run time, the first time an
// author runs `unmute init`.
//
// This is not hypothetical. An over-engineering audit of this tree listed six
// such methods as unreachable and proposed deleting all six: AgentTools,
// FallbacksFor, TaskTools, ModelDescription, VoiceDescription and VoiceProfile.
// Every one of them is called from agent.yaml.tmpl.
//
// So: every capitalised name a template calls on a value must resolve to a
// declaration somewhere in internal/. The check is deliberately loose about
// which type owns the method — matching a template action to a Go receiver
// needs the type information the template does not carry. A name that exists
// nowhere in the tree is unambiguously wrong, and that is the case worth
// catching.

var (
	// A template action, and inside it the capitalised segments of a field or
	// method chain: {{ .Something }}, {{ .Foo.Bar }}, {{ range .Items }}.
	//
	// The action boundary matters. These templates emit Python, and Python
	// attribute access (rtc.AudioFrame, aiohttp.ClientSession) looks identical
	// to a template field if you scan the whole file. Only what is between the
	// delimiters is Go.
	// Two delimiter styles are in use. internal/generate templates emit Python
	// and use the default {{ }}; internal/scaffold templates emit YAML, where
	// {{ }} would collide with the syntax, so scaffold.go sets Delims("[[","]]").
	//
	// Scanning only one style is how this gate first shipped, and it silently
	// covered none of the six methods it was written to protect: they all live
	// behind [[ ]] in agent.yaml.tmpl. Both styles, or this checks nothing.
	templateAction = regexp.MustCompile(`(?s)(?:\{\{-?(.*?)-?\}\}|\[\[-?(.*?)-?\]\])`)
	templateField  = regexp.MustCompile(`\.([A-Z][A-Za-z0-9_]*)`)
	// A template comment is an action, and these comments quote the Python they
	// are about ("llm.FallbackAdapter around its fallback chain"). Prose, not
	// calls, so it comes out before the fields are read.
	templateComment = regexp.MustCompile(`(?s)/\*.*?\*/`)
	// func (r Recv) Name( — or func Name(
	goDeclaration = regexp.MustCompile(`(?m)^func (?:\([^)]*\) )?([A-Z][A-Za-z0-9_]*)\(`)
	// Name string `yaml:...` — a struct field is just as reachable from a
	// template as a method, and just as invisible to static analysis.
	goField = regexp.MustCompile(`(?m)^\t([A-Z][A-Za-z0-9_]*)\s+[\[\]\*\w\.]`)
)

func TestTemplatesOnlyCallSymbolsThatExist(t *testing.T) {
	root := filepath.Join("..", "..")

	declared := map[string]bool{}
	templates := map[string][]string{} // symbol -> templates naming it

	err := filepath.Walk(filepath.Join(root, "internal"), func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		switch {
		case strings.HasSuffix(path, ".go"):
			raw, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			for _, m := range goDeclaration.FindAllStringSubmatch(string(raw), -1) {
				declared[m[1]] = true
			}
			for _, m := range goField.FindAllStringSubmatch(string(raw), -1) {
				declared[m[1]] = true
			}
		case strings.HasSuffix(path, ".tmpl"):
			raw, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			rel, _ := filepath.Rel(root, path)
			for _, action := range templateAction.FindAllStringSubmatch(string(raw), -1) {
				body := templateComment.ReplaceAllString(action[1]+action[2], "")
				for _, m := range templateField.FindAllStringSubmatch(body, -1) {
					templates[m[1]] = append(templates[m[1]], rel)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(templates) == 0 {
		t.Fatal("found no template symbols at all; this gate has stopped checking anything")
	}

	var missing []string
	for symbol, files := range templates {
		if declared[symbol] {
			continue
		}
		sort.Strings(files)
		missing = append(missing, symbol+" (called from "+strings.Join(unique(files), ", ")+")")
	}
	sort.Strings(missing)
	for _, m := range missing {
		t.Errorf("template calls %s, but nothing in internal/ declares it.\n"+
			"A template-reached symbol is invisible to the compiler, go vet, the linter and deadcode. "+
			"If it was deleted as unused, it was not unused: restore it, or change the template in the same commit.", m)
	}
}

func unique(in []string) []string {
	out := in[:0:0]
	seen := map[string]bool{}
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}
