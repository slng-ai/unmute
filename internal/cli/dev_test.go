package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/slng/unmute/internal/ir"
)

func TestParseDotenv(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env.local")
	content := strings.Join([]string{
		"# a comment",
		"",
		"SLNG_API_KEY=sk-slng",
		`OPENAI_API_KEY="sk-openai"`,
		"export REGION='eu-central'",
		"MALFORMED",
	}, "\n")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := parseDotenv(path)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"SLNG_API_KEY":   "sk-slng",
		"OPENAI_API_KEY": "sk-openai",
		"REGION":         "eu-central",
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("parseDotenv[%q] = %q, want %q", k, got[k], v)
		}
	}
	if _, ok := got["MALFORMED"]; ok {
		t.Error("malformed line should be skipped")
	}
}

func TestDevChildEnv_mapsSecrets(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".env.local"), []byte("MY_SLNG=sk-secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	secrets := ir.EnvSecrets{
		Local:   ir.LocalEnvConfig{EnvFile: ".env.local"},
		Secrets: map[string]ir.SecretRef{"SLNG_API_KEY": {LocalKey: "MY_SLNG"}},
	}

	env, err := devChildEnv(dir, secrets, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	if !contains(env, "SLNG_API_KEY=sk-secret") {
		t.Errorf("runtime env name not mapped from dotenv local key; env = %v", env)
	}
}

func TestDevChildEnv_missingFileWarns(t *testing.T) {
	var warn bytes.Buffer
	secrets := ir.EnvSecrets{Local: ir.LocalEnvConfig{EnvFile: ".env.local"}}
	if _, err := devChildEnv(t.TempDir(), secrets, &warn); err != nil {
		t.Fatalf("missing dotenv must not be a hard error: %v", err)
	}
	if !strings.Contains(warn.String(), "not found") {
		t.Errorf("expected a warning about the missing dotenv, got %q", warn.String())
	}
}

func TestBrowserCommand(t *testing.T) {
	for _, tc := range []struct {
		goos string
		name string
	}{
		{"darwin", "open"},
		{"linux", "xdg-open"},
	} {
		if name, _ := browserCommand(tc.goos, "http://x"); name != tc.name {
			t.Errorf("browserCommand(%q) = %q, want %q", tc.goos, name, tc.name)
		}
	}
}

func TestDev_help(t *testing.T) {
	cmd := newRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"dev", "--help"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"--no-open", "--bot-port", "talk to it"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("dev --help missing %q; got:\n%s", want, out.String())
		}
	}
}

func contains(s []string, want string) bool {
	for _, v := range s {
		if v == want {
			return true
		}
	}
	return false
}
