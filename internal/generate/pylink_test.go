package generate

import (
	"slices"
	"strings"
	"testing"
)

func TestPyCodeKeepsOnlyNamesAndFStringExpressions(t *testing.T) {
	src := "x = 'json'  # re\n" +
		"y = f\"{a.b!r} {{c}} {d:>{width}}\"\n" +
		"z = rb'\\d' + \"\"\"doc\nos = 1\n\"\"\"\n"
	got := pyCode(src)
	for _, gone := range []string{"json", "re", "c}", "doc", "os"} {
		if strings.Contains(got, gone) {
			t.Errorf("pyCode kept %q:\n%s", gone, got)
		}
	}
	refs := pyReferences(got)
	for _, want := range []string{"x", "a", "d", "width", "y", "z"} {
		if !refs[want] {
			t.Errorf("reference %q missing from %v", want, refs)
		}
	}
	if refs["b"] || refs["r"] || refs["rb"] {
		t.Errorf("attribute, conversion or prefix read as a reference: %v", refs)
	}
}

func TestPyReferencesSkipsKeywordArgumentsAndLocalImports(t *testing.T) {
	refs := pyReferences(pyCode("def f(timeout=1):\n" +
		"    from pipecat.runner.utils import create_transport\n" +
		"    return call(state=x, other == y, flag = z, t=create_transport)\n"))
	if refs["state"] || refs["flag"] || refs["timeout"] {
		t.Errorf("keyword argument read as a reference: %v", refs)
	}
	if !refs["x"] || !refs["other"] || !refs["z"] {
		t.Errorf("value missing: %v", refs)
	}
	if refs["create_transport"] || refs["pipecat"] {
		t.Errorf("local import read as a module-level reference: %v", refs)
	}
}

func TestPyReferencesLeavesOutWhatAFunctionBindsItself(t *testing.T) {
	refs := pyReferences(pyCode("def refuse(tool: str, *args: Any, **kw: Any) -> str:\n" +
		"    field = [field for field in fields if tool]\n" +
		"    for name, value in pairs:\n" +
		"        use(name, value)\n" +
		"    with open(path) as handle:\n" +
		"        handle.read()\n" +
		"    if (walrus := read()):\n" +
		"        return sorted(items, key=lambda item: item[0])\n" +
		"    return field + walrus\n" +
		"\n" +
		"def other() -> None:\n" +
		"    global COUNT\n" +
		"    COUNT = 1\n" +
		"    _LAST[key] = tool\n" +
		"\n" +
		"class K(Base):\n" +
		"    time: Time\n" +
		"    limit = 1\n" +
		"\n" +
		"    def method(self) -> int:\n" +
		"        return limit\n"))
	for _, local := range []string{"field", "name", "value", "handle", "walrus", "item", "args", "kw", "self", "time"} {
		if refs[local] {
			t.Errorf("%s is bound in its function, but read as module-level", local)
		}
	}
	// tool is a parameter of refuse and a module-level read in other; limit is
	// a class attribute a method cannot see; _LAST is subscripted, not bound.
	for _, global := range []string{"Any", "fields", "pairs", "use", "path", "read", "items", "COUNT", "_LAST", "key", "tool", "Base", "limit"} {
		if !refs[global] {
			t.Errorf("%s is a module-level read, but missing from %v", global, refs)
		}
	}
}

func TestPyDefinitionsReadsTopLevelAndTopLevelBlocks(t *testing.T) {
	code := pyCode("A = 1\n" +
		"B: int = 2\n" +
		"C: dict[str, int]\n" +
		"def f():\n    inner = 1\n" +
		"async def g():\n    pass\n" +
		"class K:\n    attr = 1\n" +
		"if cond:\n    server = 1\nelse:\n    server = 2\n" +
		"X += 1\n" +
		"P = \"\"\"\nQ = 1\n\"\"\"\n")
	got := pyDefinitions(code)
	want := []string{"A", "B", "C", "f", "g", "K", "server", "server", "P"}
	if !slices.Equal(got, want) {
		t.Errorf("pyDefinitions = %v, want %v", got, want)
	}
}

func TestLinkPythonImportsOnlyWhatEachModuleUses(t *testing.T) {
	universe, _ := parseImportBlock(strings.Split("import json\n"+
		"import os\n"+
		"import tools.salon\n"+
		"import tools.other\n"+
		"from typing import Any, cast\n", "\n"))
	files, err := linkPython(universe, []pyModule{
		{path: "settings.py", src: "\"\"\"Settings.\"\"\"\n\nLIMIT = int(os.getenv(\"L\", \"1\"))\n"},
		{path: "session.py", src: "\"\"\"Session.\"\"\"\n\n\ndef save(value: Any) -> str:\n    \"\"\"Save.\"\"\"\n    return json.dumps(value)[:LIMIT]\n"},
		{path: "agents.py", src: "\"\"\"Agents.\"\"\"\n\n\ndef book() -> str:\n    \"\"\"Book.\"\"\"\n    return save(tools.salon.book())\n"},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"settings.py": "\"\"\"Settings.\"\"\"\n\nfrom __future__ import annotations\n\nimport os\n\nLIMIT = int(os.getenv(\"L\", \"1\"))\n",
		"session.py": "\"\"\"Session.\"\"\"\n\nfrom __future__ import annotations\n\nimport json\nfrom typing import Any\n\nfrom settings import LIMIT\n\n\n" +
			"def save(value: Any) -> str:\n    \"\"\"Save.\"\"\"\n    return json.dumps(value)[:LIMIT]\n",
		"agents.py": "\"\"\"Agents.\"\"\"\n\nfrom __future__ import annotations\n\nimport tools.salon\nfrom session import save\n\n\n" +
			"def book() -> str:\n    \"\"\"Book.\"\"\"\n    return save(tools.salon.book())\n",
	}
	for _, f := range files {
		if got := string(f.Content); got != want[f.Path] {
			t.Errorf("%s:\n%s\nwant:\n%s", f.Path, got, want[f.Path])
		}
	}
}

// A quoted type is a string to the reference scan, so the import it needs
// would be silently left out and the module would fail its own ruff gate, or
// raise wherever the annotation is evaluated. Refused instead, by name.
func TestLinkPythonRefusesAQuotedTypeFromAnotherModule(t *testing.T) {
	session := pyModule{path: "session.py", src: "\"\"\"Session.\"\"\"\n\n\nclass Userdata:\n    \"\"\"State.\"\"\"\n"}
	for _, src := range []string{
		"def f(state: \"Userdata | None\") -> None:\n    pass\n",
		"def f(state) -> \"Userdata\":\n    return state\n",
		"def f(state):\n    return cast(\"Userdata\", state)\n",
	} {
		_, err := linkPython(nil, []pyModule{session, {path: "utils/router.py", src: "\"\"\"Router.\"\"\"\n\n\n" + src}})
		if err == nil || !strings.Contains(err.Error(), "names Userdata") {
			t.Errorf("%q: err = %v, want the quoted Userdata refused", src, err)
		}
	}
	// A dictionary's string values are not types.
	if _, err := linkPython(nil, []pyModule{session, {path: "agents.py", src: "\"\"\"Agents.\"\"\"\n\nROLE = {\"role\": \"Userdata\"}\n"}}); err != nil {
		t.Errorf("a string value was read as a type: %v", err)
	}
}

func TestLinkPythonRefusesACycleAndATwiceDefinedName(t *testing.T) {
	_, err := linkPython(nil, []pyModule{
		{path: "a.py", src: "\"\"\"A.\"\"\"\n\n\ndef f():\n    return g()\n"},
		{path: "b.py", src: "\"\"\"B.\"\"\"\n\n\ndef g():\n    return f()\n"},
	})
	if err == nil || !strings.Contains(err.Error(), "a.py -> b.py -> a.py") {
		t.Errorf("cycle: err = %v", err)
	}
	_, err = linkPython(nil, []pyModule{
		{path: "a.py", src: "\"\"\"A.\"\"\"\n\nX = 1\n"},
		{path: "b.py", src: "\"\"\"B.\"\"\"\n\nX = 2\n"},
	})
	if err == nil || !strings.Contains(err.Error(), "both define X") {
		t.Errorf("twice defined: err = %v", err)
	}
}
