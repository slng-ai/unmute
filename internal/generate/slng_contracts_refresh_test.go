//go:build contracts

package generate

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The drift check, behind the `contracts` build tag and out of the PR gate.
//
// Story 6's whole purpose is noticing the day this repository and the SLNG
// backend disagree. A vendored copy with no refresh check cannot notice
// anything, and fetching during `make test` would break the offline rule that
// `go test -race ./...` depends on. So this follows the shape the repository
// already uses once: `make smoke` needs Python, `make contracts` needs the
// network. Both are opt-in and neither is in the PR gate.
//
// It needs no credentials. The conformance fixtures are published, and
// contracts/shared_tools/v1/README.md says they must stay byte-identical across
// repositories, which is exactly what a digest compares.
//
// This file is the only place in the slng target's source that opens a socket,
// and it is a *test* that fetches a fixture, not a compiler that talks to the
// agents API. internal/ir's TestNoSlngFileOpensASocket skips _test.go files for
// that reason and would otherwise fail on this one.

// slngContractsBase is where the published fixtures live. A constant rather than
// a knob: comparing against anything but the published branch would make a green
// run mean nothing, and the one person who wants a different branch can edit
// this line while they are in here reading the diff anyway.
const slngContractsBase = "https://raw.githubusercontent.com/slng-ai/backend/develop/contracts/shared_tools/v1/"

func TestSlngContractsHaveNotDrifted(t *testing.T) {
	root := filepath.Join("testdata", "slng", "contracts", "v1")
	digests, err := os.ReadFile(filepath.Join(root, "digests.txt"))
	if err != nil {
		t.Fatalf("no digests.txt to compare against: %v", err)
	}
	client := &http.Client{Timeout: 30 * time.Second}
	checked := 0
	for _, line := range strings.Split(strings.TrimSpace(string(digests)), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			t.Fatalf("digests.txt line is not a sha256 and a path: %q", line)
		}
		want, relative := fields[0], fields[1]
		t.Run(relative, func(t *testing.T) {
			response, err := client.Get(slngContractsBase + relative)
			if err != nil {
				t.Fatalf("fetch %s: %v", relative, err)
			}
			defer func() { _ = response.Body.Close() }()
			if response.StatusCode != http.StatusOK {
				t.Fatalf("fetch %s: %s. A 404 means the fixture moved or was renamed upstream, which is drift worth reading rather than a broken test", relative, response.Status)
			}
			content, err := io.ReadAll(response.Body)
			if err != nil {
				t.Fatalf("read %s: %v", relative, err)
			}
			sum := sha256.Sum256(content)
			if got := hex.EncodeToString(sum[:]); got != want {
				t.Errorf("%s drifted upstream.\n  vendored %s\n  upstream %s\nRe-vendor the file, read the diff, and fix the mapping the diff changes before updating digests.txt.", relative, want, got)
			}
		})
		checked++
	}
	// A green run over nothing is the failure mode this whole target exists to
	// avoid, so say so rather than pass.
	if checked == 0 {
		t.Fatal("digests.txt is empty, so this check compared nothing")
	}
}
