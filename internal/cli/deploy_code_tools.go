package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/slng-ai/unmute/internal/generate"
	"github.com/slng-ai/unmute/internal/target"
)

// codeToolBody is the part of build/slng/tools/<name>.json deploy reads.
type codeToolBody struct {
	Name         string   `json:"name"`
	ToolType     string   `json:"tool_type"`
	Description  string   `json:"description"`
	CodeSrc      string   `json:"code_src"`
	Dependencies []string `json:"dependencies"`
}

// liveCodeTool is what `voiceai tool get` says about a tool deploy may update.
type liveCodeTool struct {
	ToolType         string `json:"tool_type"`
	Description      string `json:"description"`
	CodeSrc          string `json:"code_src"`
	LatestVersion    *int   `json:"latest_version"`
	IsCurrentVersion bool   `json:"is_current_version"`
}

// publishCodeTools makes each code tool the build carries current on SLNG: it
// creates or updates the tool from its Python, runs it once on its sample, and
// publishes it. It runs before the account is read, so resolution finds each
// tool published and attaches it by name like any tool the organisation holds.
//
// A tool whose code and description already match a published version is left
// alone. A dry run changes nothing, says what a real run would do, and returns
// the tools it would write: until they are published there is nothing to
// resolve them to.
func publishCodeTools(out io.Writer, runner *voiceaiRunner, targetName string, files []generate.File, dryRun bool) (pending []string, err error) {
	bodies, samples := map[string]codeToolBody{}, map[string][]byte{}
	for _, file := range files {
		switch {
		case strings.HasPrefix(file.Path, "tools/") && strings.HasSuffix(file.Path, ".json"):
			var body codeToolBody
			if err := json.Unmarshal(file.Content, &body); err != nil {
				return nil, fmt.Errorf("%s: %w", file.Path, err)
			}
			if body.ToolType == "code" {
				bodies[body.Name] = body
			}
		case strings.HasPrefix(file.Path, "samples/"):
			samples[strings.TrimSuffix(strings.TrimPrefix(file.Path, "samples/"), ".json")] = file.Content
		}
	}
	if len(bodies) == 0 {
		return nil, nil
	}
	var listed []slngAccountTool
	if err := runner.read(target.SlngToolList, &listed); err != nil {
		return nil, err
	}
	scratch, err := os.MkdirTemp("", "unmute-code-tools-")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(scratch) }() // best effort; the OS owns its temp directory too

	for _, name := range slices.Sorted(maps.Keys(bodies)) {
		body := bodies[name]
		exists := slices.ContainsFunc(listed, func(tool slngAccountTool) bool {
			return tool.Name == name && tool.Scope == "organisation"
		})
		if exists {
			var live liveCodeTool
			if err := runner.read(target.SlngToolGet.With(name), &live); err != nil {
				return nil, err
			}
			if live.ToolType != "code" {
				return nil, fmt.Errorf("slng target: tools/%s.yaml is a local tool, and this organisation already has a %s tool named %s: rename the tool file", name, live.ToolType, name)
			}
			if live.CodeSrc == body.CodeSrc && live.Description == body.Description && live.IsCurrentVersion && live.LatestVersion != nil {
				fmt.Fprintf(out, "%s: code tool %s is current (v%d)\n", targetName, name, *live.LatestVersion)
				continue
			}
		}
		if dryRun {
			action := "create"
			if exists {
				action = "update"
			}
			fmt.Fprintf(out, "%s: would %s code tool %s, run its sample and publish it\n", targetName, action, name)
			pending = append(pending, name)
			continue
		}
		version, err := publishCodeTool(runner, scratch, body, samples[name], exists)
		if err != nil {
			return nil, fmt.Errorf("slng target: code tool %s: %w", name, err)
		}
		fmt.Fprintf(out, "%s: published code tool %s v%d\n", targetName, name, version)
	}
	return pending, nil
}

// publishCodeTool writes, runs and publishes one code tool, and returns the
// published version.
func publishCodeTool(runner *voiceaiRunner, scratch string, body codeToolBody, sample []byte, exists bool) (int, error) {
	source := filepath.Join(scratch, body.Name+".py")
	input := filepath.Join(scratch, body.Name+".sample.json")
	if err := os.WriteFile(source, []byte(body.CodeSrc), 0o600); err != nil {
		return 0, err
	}
	if err := os.WriteFile(input, sample, 0o600); err != nil {
		return 0, err
	}
	command := target.SlngToolCreate.With(source, "--name", body.Name, "--description", body.Description)
	if exists {
		command = target.SlngToolUpdate.With(body.Name, "--file", source, "--description", body.Description)
	}
	for _, pin := range body.Dependencies {
		command = command.With("--dependency", pin)
	}
	var written any
	if err := runner.read(command, &written); err != nil {
		return 0, err
	}
	var run struct {
		Status string `json:"status"`
		Error  any    `json:"error"`
	}
	if err := runner.read(target.SlngToolRun.With(body.Name, "--input", input, "--confirm-side-effects"), &run); err != nil {
		return 0, err
	}
	if run.Status != "succeeded" {
		return 0, fmt.Errorf("its sample run %s (%v): fix the handler or tools/%s.sample.json, then deploy again", run.Status, run.Error, body.Name)
	}
	var published struct {
		Published bool `json:"published"`
		Version   *int `json:"version_number"`
	}
	if err := runner.read(target.SlngToolPublish.With(body.Name), &published); err != nil {
		return 0, err
	}
	if !published.Published || published.Version == nil {
		return 0, fmt.Errorf("SLNG did not publish it: run `%s` to see which check failed", target.SlngToolGet.With(body.Name))
	}
	return *published.Version, nil
}
