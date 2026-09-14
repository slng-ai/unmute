package docsite

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/ir"
	"github.com/slng-ai/unmute/internal/spec"
	"github.com/slng-ai/unmute/internal/target"
)

// Every authored contract key needs its own explanation, including repeated
// entry fields: describing one provider field cannot stand in for another role.
func TestManifestReferenceDocumentsEveryFieldAndClosedValue(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(siteRoot, "reference", "manifest.mdx"))
	if err != nil {
		t.Fatal(err)
	}
	blocks := regexp.MustCompile(`(?s)<ParamField\b([^>]*)>(.*?)</ParamField>`).FindAllStringSubmatch(string(raw), -1)
	fieldPath := regexp.MustCompile(`\b(?:path|body)="([^"]+)"`)
	fields := map[string]string{}
	for _, block := range blocks {
		name := fieldPath.FindStringSubmatch(block[1])
		if len(name) == 0 {
			t.Fatal("Manifest ParamField is missing its path or body field name")
		}
		if _, exists := fields[name[1]]; exists {
			t.Errorf("duplicate ParamField %q", name[1])
		}
		fields[name[1]] = strings.TrimSpace(block[2])
	}
	var visit func(reflect.Type, string)
	visit = func(typ reflect.Type, prefix string) {
		if typ.Kind() == reflect.Pointer {
			typ = typ.Elem()
		}
		if typ.Kind() == reflect.Slice {
			typ = typ.Elem()
			prefix += "[]"
		}
		if typ.Kind() != reflect.Struct {
			return
		}
		for i := 0; i < typ.NumField(); i++ {
			field := typ.Field(i)
			name := strings.Split(field.Tag.Get("yaml"), ",")[0]
			if name == "" || name == "-" {
				continue
			}
			if prefix != "" {
				name = prefix + "." + name
			}
			if strings.TrimSpace(fields[name]) == "" {
				t.Errorf("manifest key %s needs a ParamField with an explanation", name)
			}
			visit(field.Type, name)
		}
	}
	visit(reflect.TypeFor[spec.Manifest](), "")
	providers := make([]string, len(target.Providers))
	for i, provider := range target.Providers {
		providers[i] = string(provider)
	}
	enums := map[string][]string{
		"targets.allow":                  providers,
		"regions.models[].allow":         target.SlngRegions,
		"regions.deployments[].allow":    target.SlngRegions,
		"regions.deployments[].provider": providers,
		"regions.models[].role":          {string(ir.KindListen), string(ir.KindSpeak), string(ir.KindThink)},
		"tools.kinds.allow":              manifestExecutionKinds(t),
		"tracing.allow":                  ir.TracingProviders,
	}
	for path, values := range enums {
		for _, value := range values {
			if !strings.Contains(fields[path], "`"+value+"`") {
				t.Errorf("%s must document allowed value `%s` in its ParamField", path, value)
			}
		}
	}
}

// The loader's tool-kind list is private; inspect its declaration rather than
// copying eight strings that would stop detecting a newly added execution kind.
func manifestExecutionKinds(t *testing.T) []string {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), "../spec/tool_shape.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var values []string
	ast.Inspect(file, func(node ast.Node) bool {
		declaration, ok := node.(*ast.ValueSpec)
		if !ok || len(declaration.Names) != 1 || declaration.Names[0].Name != "executionBlocks" {
			return true
		}
		if len(declaration.Values) != 1 {
			t.Fatal("executionBlocks no longer has one literal initializer")
		}
		list, ok := declaration.Values[0].(*ast.CompositeLit)
		if !ok {
			t.Fatal("executionBlocks no longer uses a list literal")
		}
		for _, item := range list.Elts {
			literal, ok := item.(*ast.BasicLit)
			if !ok || literal.Kind != token.STRING {
				t.Fatal("executionBlocks contains a non-string expression")
			}
			value, err := strconv.Unquote(literal.Value)
			if err != nil {
				t.Fatal(err)
			}
			values = append(values, value)
		}
		return false
	})
	if len(values) == 0 {
		t.Fatal("no executionBlocks values found; update the manifest documentation check")
	}
	return values
}
