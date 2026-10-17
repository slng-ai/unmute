package stateschema

import (
	"bytes"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"time"

	"github.com/slng-ai/unmute/internal/target"
)

//go:embed read_tools.py
var readToolsScript string

// ToolRef names one local tool to read: its handler and precheck as
// `file.py:function`, and the bytes of the files they live in.
type ToolRef struct {
	Name     string `json:"name"`
	Handler  string `json:"handler"`
	Precheck string `json:"precheck"`
	Source   []byte `json:"-"`
}

// ToolTypes is what one handler declares: its description from the
// docstring, its input JSON Schema from the signature, and its output JSON
// Schema from the return type's Pydantic model.
type ToolTypes struct {
	Description string         `json:"description"`
	Input       map[string]any `json:"input"`
	Output      map[string]any `json:"output"`
}

// toolReport is what read_tools.py prints.
type toolReport struct {
	Tools map[string]struct {
		ToolTypes
		Error string `json:"error"`
	} `json:"tools"`
}

// ToolDigest names one read of a package's tools with the current pins. It
// covers state.py too, because a handler may return a model state.py declares.
//
// ponytail: it does not cover modules a handler imports from beside it, so an
// edit to one needs a re-record by hand. Hash the whole package when that bites.
func ToolDigest(state []byte, tools []ToolRef) string {
	hash := sha256.New()
	fmt.Fprintf(hash, "unmute-core==%s\n", target.CorePin)
	for _, name := range slices.Sorted(maps.Keys(target.StatePins)) {
		fmt.Fprintf(hash, "%s==%s\n", name, target.StatePins[name])
	}
	hash.Write([]byte{0})
	hash.Write(state)
	for _, tool := range tools {
		fmt.Fprintf(hash, "\x00%s\x00%s\x00%s\x00", tool.Name, tool.Handler, tool.Precheck)
		hash.Write(tool.Source)
	}
	return hex.EncodeToString(hash.Sum(nil))
}

// ToolArgs is the uv command line that reads a package's tools, after `uv`.
func ToolArgs(root, statePath string, tools []ToolRef) ([]string, error) {
	refs, err := json.Marshal(tools)
	if err != nil {
		return nil, err
	}
	args := []string{"run", "--no-project", "--no-config", "--quiet", "--python", target.StatePython,
		"--with", "unmute-core==" + target.CorePin}
	for _, name := range slices.Sorted(maps.Keys(target.StatePins)) {
		args = append(args, "--with", name+"=="+target.StatePins[name])
	}
	return append(args, "python", "-I", "-c", readToolsScript, root, statePath, string(refs)), nil
}

// RunTools executes read_tools.py on a package and returns its raw report.
func (UV) RunTools(ctx context.Context, dir string, hasState bool, tools []ToolRef) ([]byte, error) {
	uv, err := exec.LookPath("uv")
	if err != nil {
		return nil, errors.New("tools: reading local tool types needs uv, which is not on PATH; install it from https://docs.astral.sh/uv/")
	}
	root, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	statePath := ""
	if hasState {
		statePath = filepath.Join(root, FileName)
	}
	args, err := ToolArgs(root, statePath, tools)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, uv, args...)
	cmd.Env = append(os.Environ(), "PYTHONDONTWRITEBYTECODE=1")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("tools: a local tool handler does not import: %w", importError(stderr.String(), err))
	}
	return stdout.Bytes(), nil
}

// ReadTools implements Reader.
func (u UV) ReadTools(ctx context.Context, dir string, state []byte, tools []ToolRef) (map[string]ToolTypes, error) {
	out, err := u.RunTools(ctx, dir, state != nil, tools)
	if err != nil {
		return nil, err
	}
	return fromToolReport(out)
}

// ReadTools implements Reader.
func (r Recorded) ReadTools(_ context.Context, dir string, state []byte, tools []ToolRef) (map[string]ToolTypes, error) {
	digest := ToolDigest(state, tools)
	data, err := os.ReadFile(filepath.Join(r.ToolDir(), digest+".json"))
	if err != nil {
		return nil, fmt.Errorf("%s has no recorded tool types for this content; run `go test ./internal/spec -run TestRecordTools -update` (needs uv)", dir)
	}
	var rec recording
	if err := json.Unmarshal(data, &rec); err != nil {
		return nil, fmt.Errorf("recorded tool types %s: %w", digest, err)
	}
	return fromToolReport(rec.Report)
}

// ToolDir is where tool reports are recorded, beside the state reports.
func (r Recorded) ToolDir() string { return filepath.Join(filepath.Dir(r.Dir), "recorded_tools") }

// fromToolReport turns a report into types, and a handler local_tool refused
// into an error naming the tool file.
func fromToolReport(data []byte) (map[string]ToolTypes, error) {
	var rep toolReport
	if err := json.Unmarshal(data, &rep); err != nil {
		return nil, fmt.Errorf("tools: the report is not JSON: %w", err)
	}
	out := make(map[string]ToolTypes, len(rep.Tools))
	for _, name := range slices.Sorted(maps.Keys(rep.Tools)) {
		tool := rep.Tools[name]
		if tool.Error != "" {
			return nil, fmt.Errorf("tools/%s.yaml: its handler cannot be a local tool: %s", name, tool.Error)
		}
		out[name] = tool.ToolTypes
	}
	return out, nil
}

// RecordedToolsInRepo is where this repository's tool recordings live.
func RecordedToolsInRepo() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "testdata", "recorded_tools")
}
