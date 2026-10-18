//go:build e2e

// Package e2e compiles each scenario package for every code target it names
// and runs its scenarios as whole calls on the real model, through
// unmute-core's own runner. It is the proof that a compiled agent says what
// the hand-written one in unmute-core says, word for word, on the same number
// of model requests.
//
// It needs uv, network, and the keys each package uses in the repository's
// .env, so it never runs in `make test` or on a pull request:
//
//	make e2e                          # every package, every target
//	make e2e PKG=kestrel FW=pipecat   # one package on one target
//
// A package with a key unset is skipped with a line that names the key.
package e2e

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

// packagesDirs hold every package with a scenarios/ folder: the agents ported
// from unmute-core's examples, with the scenarios they pass there, and this
// repo's own examples.
var packagesDirs = []string{"internal/voice-agents-tests", "examples"}

// entrypoints is the module the runner drives on each code target.
var entrypoints = map[string]string{"livekit": "agent", "pipecat": "bot"}

func TestScenarios(t *testing.T) {
	root := repoRoot(t)
	loadDotEnv(t, filepath.Join(root, ".env"))
	unmute := buildUnmute(t, root)
	for _, pkg := range scenarioPackages(t, root) {
		t.Run(filepath.Base(pkg), func(t *testing.T) {
			for _, framework := range targetsOf(t, pkg) {
				t.Run(framework, func(t *testing.T) {
					runScenarios(t, unmute, pkg, framework)
				})
			}
		})
	}
}

// runScenarios compiles one package for one target in a scratch copy, installs
// the project, and runs every scenario through unmute_core.testing.
func runScenarios(t *testing.T, unmute, pkg, framework string) {
	work := filepath.Join(t.TempDir(), filepath.Base(pkg))
	if err := os.CopyFS(work, os.DirFS(pkg)); err != nil {
		t.Fatalf("copy %s: %v", pkg, err)
	}
	run(t, work, unmute, "compile", ".", "--target", framework)
	project := filepath.Join(work, "build", framework)
	if missing := missingKeys(t, project); len(missing) > 0 {
		t.Skipf("set %s in .env to run it", strings.Join(missing, ", "))
	}
	run(t, project, "uv", "sync", "--quiet")
	if core := os.Getenv("UNMUTE_E2E_CORE"); core != "" {
		// An unreleased unmute-core, from a local checkout, over the pinned one.
		run(t, project, "uv", "pip", "install", "--quiet", "--python", ".venv", "--editable", core)
	}
	scenarios, _ := filepath.Glob(filepath.Join(pkg, "scenarios", "*.json"))
	args := append([]string{"run", "--no-sync", "python", "-I", "-m", "unmute_core.testing",
		"converse", framework, entrypoints[framework]}, scenarios...)
	run(t, project, "uv", args...)
}

// run executes one command in dir and fails the test with its output when it
// fails. Its output is printed either way, because a passing run is the
// transcript a reader checks the words against.
func run(t *testing.T, dir, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	// The knowledge module's nltk refuses a hard-linked corpus file: see uvCommand in
	// internal/generate.
	cmd.Env = append(os.Environ(), "UV_LINK_MODE=copy")
	out, err := cmd.CombinedOutput()
	t.Logf("%s %s\n%s", name, strings.Join(args, " "), out)
	if err != nil {
		t.Fatalf("%s %s: %v", name, args[0], err)
	}
}

// missingKeys lists the variables the compiled project needs that are unset.
func missingKeys(t *testing.T, project string) []string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(project, "compile-report.json"))
	if err != nil {
		t.Fatalf("read compile-report.json: %v", err)
	}
	var report struct {
		RequiredEnv []string `json:"required_env"`
	}
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatalf("compile-report.json: %v", err)
	}
	return slices.DeleteFunc(report.RequiredEnv, func(name string) bool { return os.Getenv(name) != "" })
}

// scenarioPackages lists the packages that carry scenarios.
func scenarioPackages(t *testing.T, root string) []string {
	t.Helper()
	var found []string
	for _, dir := range packagesDirs {
		matches, err := filepath.Glob(filepath.Join(root, dir, "*", "scenarios"))
		if err != nil {
			t.Fatal(err)
		}
		found = append(found, matches...)
	}
	if len(found) == 0 {
		t.Fatalf("no package under %s has a scenarios/ folder", strings.Join(packagesDirs, " or "))
	}
	packages := make([]string, len(found))
	for i, dir := range found {
		packages[i] = filepath.Dir(dir)
	}
	return packages
}

// targetsOf lists the code targets a package's targets.yaml names, by
// provider, in a fixed order.
func targetsOf(t *testing.T, pkg string) []string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(pkg, "targets.yaml"))
	if err != nil {
		t.Fatalf("read targets.yaml: %v", err)
	}
	var out []string
	for _, framework := range []string{"livekit", "pipecat"} {
		if bytes.Contains(data, []byte("provider: "+framework)) {
			out = append(out, framework)
		}
	}
	return out
}

// buildUnmute builds this checkout's CLI into a scratch folder.
func buildUnmute(t *testing.T, root string) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "unmute")
	cmd := exec.Command("go", "build", "-o", bin, ".")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	return bin
}

// loadDotEnv sets each KEY=value in the file that the environment does not
// already set. A missing file sets nothing. Values are never logged.
func loadDotEnv(t *testing.T, path string) {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		return
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		key, value, ok := strings.Cut(strings.TrimPrefix(line, "export "), "=")
		if !ok || strings.HasPrefix(line, "#") || os.Getenv(key) != "" {
			continue
		}
		t.Setenv(strings.TrimSpace(key), strings.Trim(strings.TrimSpace(value), `"'`))
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..")
}
