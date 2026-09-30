package generate

import (
	"fmt"
	"strings"
)

// pythonCheckers renders the tail of every emitted pyproject.toml: the pinned
// checkers and the rules they hold the emitted code to. It lives in one place
// so the three code targets cannot drift apart, and so the ruff pin has one
// home.
//
// ruff is pinned, not floated: 0.16 widened its default rule selection, so an
// unpinned `ruff check .` turned every ruff release into a failure of an
// unchanged generator. Bump it deliberately, the way the compiler pins
// jsonschema-go.
//
// The rule list is the readability bar. PERF, SIM, C4 and FURB catch a loop
// that should be a comprehension or a built-in; BLE catches a bare
// `except Exception` with no reason given; D (Google style) and ANN catch
// a function without a docstring or type hints. tools/ and logic/ are the
// author's own code, copied as written, so they skip the docstring and
// annotation rules and keep the rest.
//
// RUF001-003 flag a character that looks like an ASCII one, such as a curly
// apostrophe. The generated code carries the author's own words in strings,
// docstrings and comments (prompts, announcements, tool descriptions), and a
// curly quote there is correct prose, not a typo to rewrite.
//
// ty reads python-version from here and not from requires-python, so the
// caller passes the same oldest version its requires-python names.
func pythonCheckers(python string) string {
	return fmt.Sprintf(pythonCheckersFormat, python)
}

const pythonCheckersFormat = `[dependency-groups]
dev = ["ruff==0.15.7", "ty"]

[tool.ruff.lint]
select = ["E", "F", "W", "I", "UP", "B", "SIM", "C4", "PERF", "FURB", "RUF", "BLE", "D", "ANN"]
ignore = ["E501", "ANN401", "RUF001", "RUF002", "RUF003"]

[tool.ruff.lint.pydocstyle]
convention = "google"

[tool.ruff.lint.per-file-ignores]
"tools/*" = ["D", "ANN"]
"logic/*" = ["D", "ANN"]

[tool.ty.environment]
python-version = "%s"
`

// pyLineLimit is ruff's default line length, which the emitted pyproject keeps.
const pyLineLimit = 88

// wrapLongImports wraps every top-level `from x import a, b` line longer than
// pyLineLimit into one name per line, the layout ruff's import sorter writes.
// The generator does not format (compile does, when ruff is installed), but
// an import's wrapping is part of rule I001, so the raw output has to carry it
// or a project compiled on a machine without ruff fails its own lint.
func wrapLongImports(src []byte) []byte {
	lines := strings.Split(string(src), "\n")
	for i, line := range lines {
		head, names, found := strings.Cut(line, " import ")
		if !found || len(line) <= pyLineLimit || !strings.HasPrefix(line, "from ") || strings.Contains(names, "(") {
			continue
		}
		names, comment, _ := strings.Cut(names, "  #")
		var b strings.Builder
		b.WriteString(head + " import (")
		if comment != "" {
			b.WriteString("  #" + comment)
		}
		for _, name := range strings.Split(names, ",") {
			b.WriteString("\n    " + strings.TrimSpace(name) + ",")
		}
		b.WriteString("\n)")
		lines[i] = b.String()
	}
	return []byte(strings.Join(lines, "\n"))
}
