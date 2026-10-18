//go:build smoke

package generate

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// slngCodeToolCheck loads one code tool the way SLNG does: it imports the file,
// reads Input, Output and handler by name, and runs handler on the sample.
const slngCodeToolCheck = `import importlib.util, json, sys
spec = importlib.util.spec_from_file_location("tool", sys.argv[1])
tool = importlib.util.module_from_spec(spec)
spec.loader.exec_module(tool)
result = tool.handler(tool.Input(**json.load(open(sys.argv[2]))))
assert isinstance(result, tool.Output), (type(result), tool.Output)
print(result.model_dump_json())
`

// TestSmokeSlngCodeToolsRunOnTheirSamples runs each code tool slng-live compiles
// to, on its own sample, under the Pydantic SLNG runs. A wrapper that builds
// but cannot run would otherwise first fail at deploy, against the account.
func TestSmokeSlngCodeToolsRunOnTheirSamples(t *testing.T) {
	if _, err := exec.LookPath("uv"); err != nil {
		t.Skip("uv not available")
	}
	_, files := compileSlng(t, filepath.Join("..", voiceAgentTestsDir, "slng-live"))
	dir := t.TempDir()
	check := filepath.Join(dir, "check.py")
	if err := os.WriteFile(check, []byte(slngCodeToolCheck), 0o644); err != nil {
		t.Fatal(err)
	}
	ran := 0
	for path, content := range files {
		name, ok := strings.CutPrefix(path, "tools/")
		if !ok {
			continue
		}
		name = strings.TrimSuffix(name, ".json")
		var body struct {
			CodeSrc string `json:"code_src"`
		}
		if err := json.Unmarshal([]byte(content), &body); err != nil {
			t.Fatal(err)
		}
		source, sample := filepath.Join(dir, name+".py"), filepath.Join(dir, name+".sample.json")
		if err := os.WriteFile(source, []byte(body.CodeSrc), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(sample, []byte(files["samples/"+name+".json"]), 0o644); err != nil {
			t.Fatal(err)
		}
		out, err := uvCommand("run", "--quiet", "--no-project", "--with", "pydantic", "python", "-I", check, source, sample).CombinedOutput()
		if err != nil {
			t.Errorf("%s does not run on its sample: %v\n%s", name, err, out)
		}
		ran++
	}
	if ran == 0 {
		t.Fatal("slng-live compiled to no code tool, so this check checked nothing")
	}
}
