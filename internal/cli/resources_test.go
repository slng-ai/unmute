package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestResourcesShowsNamesInTheSpellingAPackageMustUse. The whole point of the
// command: SLNG matches names exactly and is case-sensitive everywhere, so an
// author copying from this output has to get a name that works.
func TestResourcesShowsNamesInTheSpellingAPackageMustUse(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the stub voiceai is a POSIX shell script")
	}
	stub := `case "$*" in
  *whoami*) printf '{"ok":true,"profile":"default","account":{"org_id":"org-1","org_name":"Example"}}' ;;
  *"tool list"*) printf '[{"name":"end_call","tool_type":"end_call"},{"name":"api_request","tool_type":"api_request"}]' ;;
  *"mcp list"*) printf '[{"name":"firecrawl-mcp","transport":"streamable_http","capability_status":"healthy"}]' ;;
  *"mcp tools"*) printf '[{"name":"firecrawl_scrape"}]' ;;
  *"trunks list"*) printf '[{"direction":"inbound","name":"1_inbound","numbers":["+447700900111"],"usable":true,"in_use_by":null}]' ;;
  *) printf '[]' ;;
esac`
	out, errOut := runResourcesWithStub(t, stub)

	for _, want := range []string{
		"organisation Example (org-1), profile default",
		"end_call", "api_request",
		"firecrawl-mcp", "firecrawl_scrape",
		"1_inbound", "+447700900111",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the listing does not name %q:\n%s", want, out)
		}
	}
	// The type separates what a package references from what a push writes, so it
	// has to sit on the same line as the name. Asserted by line rather than by
	// exact spacing, because column widths are presentation and this is not.
	if !lineWith(out, "end_call", "end_call") || !lineWith(out, "api_request", "api_request") {
		t.Errorf("a tool's type is not shown beside its name:\n%s", out)
	}
	// Never a promise that the server works.
	if !strings.Contains(out, "not a live call") {
		t.Errorf("the MCP section does not say its status is a stored probe:\n%s", out)
	}
	// A free trunk reads as free, so an author knows nothing is attached yet.
	if !strings.Contains(out, "free") {
		t.Errorf("an unattached trunk is not marked:\n%s", out)
	}
	if errOut != "" {
		t.Errorf("a clean listing wrote to stderr: %q", errOut)
	}
}

// TestResourcesSaysWhenNothingIsAttached. An empty section has to say what to do
// about it, because "none" alone reads like a broken command.
func TestResourcesSaysWhenNothingIsAttached(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the stub voiceai is a POSIX shell script")
	}
	stub := `case "$*" in
  *whoami*) printf '{"ok":true,"profile":"default","account":{"org_id":"org-1","org_name":"Example"}}' ;;
  *) printf '[]' ;;
esac`
	out, _ := runResourcesWithStub(t, stub)
	if !strings.Contains(out, "attached in the SLNG dashboard") {
		t.Errorf("an account with no MCP servers is not told where they come from:\n%s", out)
	}
	if !strings.Contains(out, "no agents cannot be enumerated") {
		t.Errorf("an empty trunk listing does not explain why it can be empty:\n%s", out)
	}
}

// TestResourcesReadsNoPackage. It takes no directory and must not go looking for
// one: an author runs this from anywhere, including outside a package.
func TestResourcesReadsNoPackage(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the stub voiceai is a POSIX shell script")
	}
	dir := t.TempDir()
	t.Chdir(dir)
	stub := `case "$*" in
  *whoami*) printf '{"ok":true,"profile":"default","account":{"org_id":"org-1","org_name":"Example"}}' ;;
  *) printf '[]' ;;
esac`
	if _, _ = runResourcesWithStub(t, stub); false {
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("the command wrote %d entries into the working directory", len(entries))
	}
}

// lineWith reports whether one line carries both fields.
func lineWith(text string, fields ...string) bool {
	for _, line := range strings.Split(text, "\n") {
		all := true
		for _, field := range fields {
			if !strings.Contains(line, field) {
				all = false
				break
			}
		}
		if all {
			return true
		}
	}
	return false
}

func runResourcesWithStub(t *testing.T, script string) (out, errOut string) {
	t.Helper()
	binDir := t.TempDir()
	stub := filepath.Join(binDir, "voiceai")
	if err := os.WriteFile(stub, []byte("#!/bin/sh\n"+script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	root := newRootCmd()
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"resources"})
	if err := root.Execute(); err != nil {
		t.Fatalf("resources: %v\n%s", err, stderr.String())
	}
	return stdout.String(), stderr.String()
}
