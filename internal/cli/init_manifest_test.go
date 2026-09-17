package cli

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/manifest"
)

// noInput fails the test if the command reads stdin. `init` without
// --from-manifest asks nothing, so a read is the bug this file guards.
type noInput struct{ t *testing.T }

func (r noInput) Read([]byte) (int, error) { r.t.Fatal("init read stdin"); return 0, io.EOF }

func manifestTestStore(t *testing.T) manifest.Store {
	t.Helper()
	previous := manifestStore
	store := manifest.Store{Root: t.TempDir()}
	manifestStore = func() (manifest.Store, error) { return store, nil }
	t.Cleanup(func() { manifestStore = previous })
	return store
}
func initManifestCommand(t *testing.T, in io.Reader, args ...string) (string, error) {
	t.Helper()
	cmd := newRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetIn(in)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

func TestFromManifestErrorsWriteNothing(t *testing.T) {
	empty := manifestTestStore(t)
	dir := filepath.Join(t.TempDir(), "agent")
	if _, err := initManifestCommand(t, noInput{t}, "init", dir, "--from-manifest"); err == nil || !strings.Contains(err.Error(), "no saved manifests") {
		t.Fatalf("empty library accepted: %v", err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("empty library wrote destination")
	}
	if _, err := empty.Create("chosen", []byte("manifest: Company\nversion: 1\n")); err != nil {
		t.Fatal(err)
	}
	dir = filepath.Join(t.TempDir(), "agent")
	if _, err := initManifestCommand(t, noInput{t}, "init", dir, "--from-manifest", "chosen"); err == nil || !strings.Contains(err.Error(), "takes no name") {
		t.Fatalf("second name accepted: %v", err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("rejected arguments wrote destination")
	}
	path, err := empty.Path("chosen")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("invalid: ["), 0600); err != nil {
		t.Fatal(err)
	}
	dir = filepath.Join(t.TempDir(), "agent")
	if _, err := initManifestCommand(t, noInput{t}, "init", dir, "--from-manifest"); err == nil {
		t.Fatal("invalid saved manifest accepted")
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("invalid saved manifest wrote destination")
	}
}

func TestFromManifestCopiesTheContractAndCancelWritesNothing(t *testing.T) {
	store := manifestTestStore(t)
	raw := []byte("manifest: chosen\nversion: 1\ntargets:\n  allow: [livekit]\n")
	if _, err := store.Create("chosen", raw); err != nil {
		t.Fatal(err)
	}
	for _, save := range []bool{true, false} {
		dir := filepath.Join(t.TempDir(), "agent")
		// 1 picks the only saved manifest, then 8 cancels the agent menu.
		input := "1\n8\n"
		if save {
			input = "1\n7\n\n"
		}
		out, err := initManifestCommand(t, strings.NewReader(input), "init", dir, "--from-manifest")
		if err != nil {
			t.Fatalf("%v\n%s", err, out)
		}
		if save {
			b, err := os.ReadFile(filepath.Join(dir, "manifest"))
			if err != nil || !bytes.Equal(b, raw) {
				t.Fatal("wrong contract")
			}
		} else if _, err := os.Stat(dir); !os.IsNotExist(err) {
			t.Fatal("cancel wrote package")
		}
	}
}
