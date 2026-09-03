package style

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// CLAUDE.md has said colour has one owner since before this test existed, and in
// the meantime dev_web.go grew a hardcoded bold-green ▸ that was gated on
// neither NO_COLOR nor a terminal, so it wrote escapes into every pipe. The rule
// was real and nothing failed on it. This is the gate.
//
// String literals only, read through the AST, because a comment quoting ruff's
// coloured output is documentation and not a colour decision. Cursor control is
// not colour either: the SGR pattern requires the trailing `m` that the
// spinner's erase-line \033[2K does not have.
var (
	sgrLiteral = regexp.MustCompile(`\x1b\[[0-9;]*m`)
	hexLiteral = regexp.MustCompile(`#[0-9A-Fa-f]{6}\b`)
)

// skipDir names trees with no Go this rule governs: emitted artifacts, test
// fixtures that are not compiled, and the checkout's own plumbing.
var skipDir = map[string]bool{
	".git":         true,
	"build":        true,
	"node_modules": true,
	"testdata":     true,
	"vendor":       true,
}

func TestNoColorLiteralOutsideThisPackage(t *testing.T) {
	self, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	var offenders []string

	walkErr := filepath.WalkDir(filepath.Join("..", ".."), func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if skipDir[d.Name()] {
				return fs.SkipDir
			}
			abs, err := filepath.Abs(path)
			if err != nil {
				return err
			}
			if abs == self {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".go") {
			return nil
		}
		file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		ast.Inspect(file, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			value, err := strconv.Unquote(lit.Value)
			if err != nil {
				return true
			}
			if match := sgrLiteral.FindString(value); match != "" {
				offenders = append(offenders, fset.Position(lit.Pos()).String()+
					": SGR escape "+strconv.Quote(match))
			}
			if match := hexLiteral.FindString(value); match != "" {
				offenders = append(offenders, fset.Position(lit.Pos()).String()+
					": hex colour "+match)
			}
			return true
		})
		return nil
	})
	if walkErr != nil {
		t.Fatal(walkErr)
	}
	if len(offenders) > 0 {
		t.Errorf("colour is defined outside internal/style in %d place(s); "+
			"add a token here and render it through style.For(w) so NO_COLOR and a "+
			"non-terminal writer both drop it:\n  %s",
			len(offenders), strings.Join(offenders, "\n  "))
	}
}
