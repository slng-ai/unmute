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
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/slng-ai/unmute/internal/target"
)

// FileName is the file a package declares its state in, next to agent.yaml.
const FileName = "state.py"

//go:embed read_state.py
var readScript string

// Reader turns a package's state.py into a Model.
type Reader interface {
	Read(ctx context.Context, dir string, source []byte) (*Model, error)
}

// UV reads state.py by importing it under the pinned Pydantic, through uv.
type UV struct{}

// Recorded reads a report recorded earlier by UV, filed under the digest of
// state.py. It is what tests use, so `make test` needs no Python, and it reads
// the same report through the same checks as UV.
type Recorded struct{ Dir string }

// report is what read_state.py prints.
type report struct {
	Schema      json.RawMessage `json:"schema"`
	BadDefaults []struct {
		Name  string `json:"name"`
		Error string `json:"error"`
	} `json:"bad_defaults"`
	Aliased []string `json:"aliased"`
	Frozen  []string `json:"frozen"`
}

// recording is one fixture file under Recorded.Dir.
type recording struct {
	Source string          `json:"source"`
	Digest string          `json:"digest"`
	Report json.RawMessage `json:"report"`
}

// Digest names one state.py read with the current pins. Moving a pin changes
// every digest, which is what makes a stale recording visible without Python.
func Digest(source []byte) string {
	hash := sha256.New()
	for _, name := range slices.Sorted(maps.Keys(target.StatePins)) {
		fmt.Fprintf(hash, "%s==%s\n", name, target.StatePins[name])
	}
	hash.Write([]byte{0})
	hash.Write(source)
	return hex.EncodeToString(hash.Sum(nil))
}

// Args is the uv command line that reads one state.py, after `uv`.
func Args(path string) []string {
	args := []string{"run", "--no-project", "--no-config", "--quiet", "--python", target.StatePython}
	for _, name := range slices.Sorted(maps.Keys(target.StatePins)) {
		args = append(args, "--with", name+"=="+target.StatePins[name])
	}
	return append(args, "python", "-I", "-c", readScript, path)
}

// Run executes read_state.py on one state.py and returns its raw report.
func (UV) Run(ctx context.Context, path string) ([]byte, error) {
	uv, err := exec.LookPath("uv")
	if err != nil {
		return nil, errors.New("state.py: reading its fields needs uv, which is not on PATH; install it from https://docs.astral.sh/uv/")
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, uv, Args(path)...)
	cmd.Env = append(os.Environ(), "PYTHONDONTWRITEBYTECODE=1")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return nil, importError(stderr.String(), err)
	}
	return stdout.Bytes(), nil
}

// Read implements Reader.
func (u UV) Read(ctx context.Context, dir string, source []byte) (*Model, error) {
	path, err := filepath.Abs(filepath.Join(dir, FileName))
	if err != nil {
		return nil, err
	}
	out, err := u.Run(ctx, path)
	if err != nil {
		return nil, err
	}
	return fromReport(out, source)
}

// Read implements Reader.
func (r Recorded) Read(_ context.Context, dir string, source []byte) (*Model, error) {
	digest := Digest(source)
	data, err := os.ReadFile(filepath.Join(r.Dir, digest+".json"))
	if err != nil {
		return nil, fmt.Errorf("%s has no recorded schema for this content; run `go test ./internal/stateschema -run TestRecord -update` (needs uv)",
			filepath.Join(dir, FileName))
	}
	var rec recording
	if err := json.Unmarshal(data, &rec); err != nil {
		return nil, fmt.Errorf("recorded schema %s: %w", digest, err)
	}
	return fromReport(rec.Report, source)
}

var tracebackLine = regexp.MustCompile(`File "[^"]*state\.py", line (\d+)`)

// importError turns a traceback into the one line an author acts on: where in
// state.py it failed and what Python said.
func importError(stderr string, runErr error) error {
	lines := strings.Split(strings.TrimSpace(stderr), "\n")
	last := strings.TrimSpace(lines[len(lines)-1])
	if strings.Contains(stderr, "No solution found") || strings.Contains(stderr, "Failed to download") {
		return fmt.Errorf("state.py: uv could not install the pinned Pydantic (the first run needs network; later runs use uv's cache): %s", last)
	}
	if last == "" {
		return fmt.Errorf("state.py: reading it failed: %w", runErr)
	}
	if found := tracebackLine.FindAllStringSubmatch(stderr, -1); len(found) > 0 {
		return fmt.Errorf("state.py:%s: does not import: %s", found[len(found)-1][1], last)
	}
	return fmt.Errorf("state.py: does not import: %s", last)
}

// fromReport holds a report to the rules no schema can express, then reads the
// schema itself.
func fromReport(data, source []byte) (*Model, error) {
	var rep report
	if err := json.Unmarshal(data, &rep); err != nil {
		return nil, fmt.Errorf("state.py: the report is not JSON: %w", err)
	}
	if len(rep.BadDefaults) > 0 {
		bad := rep.BadDefaults[0]
		return nil, fmt.Errorf("state.py: State.%s has a default its own type refuses (%s); "+
			"declare it `| None = None`, or give it a default that fits", bad.Name, bad.Error)
	}
	if len(rep.Aliased) > 0 {
		return nil, fmt.Errorf("state.py: %s sets an alias; a state field is read and saved by its own name, so drop alias=",
			rep.Aliased[0])
	}
	if len(rep.Frozen) > 0 {
		return nil, fmt.Errorf("state.py: %s is frozen; the call saves into the state, so drop frozen=True", rep.Frozen[0])
	}
	model, err := Parse(rep.Schema)
	if err != nil {
		return nil, fmt.Errorf("state.py: %w", err)
	}
	extra, err := ExtraTypes(source)
	if err != nil {
		return nil, err
	}
	model.Digest, model.ExtraTypes = Digest(source), extra
	return model, nil
}

// allowedExtraTypes are the pydantic_extra_types modules a state.py may use.
// Each needs a dependency the emitted project installs, so the set is closed.
var allowedExtraTypes = []string{"currency_code", "language_code", "phone_numbers"}

var extraImport = regexp.MustCompile(`(?m)^\s*(?:from|import)\s+pydantic_extra_types\.(\w+)`)

// ExtraTypes lists the pydantic_extra_types modules state.py imports, and
// refuses one outside the allowed set.
func ExtraTypes(source []byte) ([]string, error) {
	var out []string
	for _, match := range extraImport.FindAllSubmatch(source, -1) {
		module := string(match[1])
		if !slices.Contains(allowedExtraTypes, module) {
			return nil, fmt.Errorf("state.py imports pydantic_extra_types.%s; a state may use %s",
				module, strings.Join(allowedExtraTypes, ", "))
		}
		if !slices.Contains(out, module) {
			out = append(out, module)
		}
	}
	slices.Sort(out)
	return out, nil
}
