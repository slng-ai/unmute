package generate

import "fmt"

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
// ty reads python-version from here and not from requires-python, so the
// caller passes the same oldest version its requires-python names.
func pythonCheckers(python string) string {
	return fmt.Sprintf(pythonCheckersFormat, python)
}

const pythonCheckersFormat = `[dependency-groups]
dev = ["ruff==0.15.7", "ty"]

[tool.ruff.lint]
select = ["E", "F", "W", "I", "UP", "B", "SIM", "C4", "PERF", "FURB", "RUF", "BLE", "D", "ANN"]
ignore = ["E501", "ANN401"]

[tool.ruff.lint.pydocstyle]
convention = "google"

[tool.ruff.lint.per-file-ignores]
"tools/*" = ["D", "ANN"]
"logic/*" = ["D", "ANN"]

[tool.ty.environment]
python-version = "%s"
`
