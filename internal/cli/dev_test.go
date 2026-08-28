package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/generate"
	"github.com/slng-ai/unmute/internal/scaffold"
)

func TestParseDotenv(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
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

func TestDevChildEnv_readsDotenvLocal(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".env.local"), []byte("SLNG_API_KEY=sk-secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	env := packageEnv(dir, &bytes.Buffer{})
	if !contains(env, "SLNG_API_KEY=sk-secret") {
		t.Errorf(".env.local value not passed to the child env; env = %v", env)
	}
}

func TestV14DevChildEnvReadsWorkingDirectoryThenPackageDotenv(t *testing.T) {
	repo := t.TempDir()
	root := filepath.Join(repo, "examples", "agent")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".env"), []byte(
		"UNMUTE_TEST_REPO_ENV=repo\nUNMUTE_TEST_CWD_LOCAL_WINS=repo-env\nUNMUTE_TEST_SHARED_ENV=repo-env\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".env.local"), []byte(
		"UNMUTE_TEST_REPO_LOCAL_ENV=repo-local\nUNMUTE_TEST_CWD_LOCAL_WINS=repo-local\nUNMUTE_TEST_PACKAGE_WINS=repo-local\nUNMUTE_TEST_SHARED_ENV=repo-local\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".env"), []byte(
		"UNMUTE_TEST_PACKAGE_ENV=package\nUNMUTE_TEST_PACKAGE_LOCAL_WINS=package-env\nUNMUTE_TEST_PACKAGE_WINS=package-env\nUNMUTE_TEST_SHARED_ENV=package-env\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".env.local"), []byte(
		"UNMUTE_TEST_PACKAGE_LOCAL_ENV=package-local\nUNMUTE_TEST_PACKAGE_LOCAL_WINS=package-local\nUNMUTE_TEST_SHARED_ENV=package-local\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("UNMUTE_TEST_SHARED_ENV", "shell")
	t.Chdir(repo)

	env := packageEnv(root, &bytes.Buffer{})
	for _, want := range []string{
		"UNMUTE_TEST_REPO_ENV=repo",
		"UNMUTE_TEST_REPO_LOCAL_ENV=repo-local",
		"UNMUTE_TEST_CWD_LOCAL_WINS=repo-local",
		"UNMUTE_TEST_PACKAGE_ENV=package",
		"UNMUTE_TEST_PACKAGE_LOCAL_ENV=package-local",
		"UNMUTE_TEST_PACKAGE_WINS=package-env",
		"UNMUTE_TEST_PACKAGE_LOCAL_WINS=package-local",
		"UNMUTE_TEST_SHARED_ENV=package-local",
	} {
		if !contains(env, want) {
			t.Errorf("dev child env missing %q", want)
		}
	}
}

// Every local run has to say it is local, or its Coval traces are labelled as
// deployed ones and the two cannot be told apart. The marker goes on after the
// dotenv files for the same reason `UNMUTE_DEV_METRICS` does: a stale `0` left
// in a `.env` would otherwise silently turn it off for the whole run.
func TestDevChildEnv_marksTheRunAsLocal(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".env"),
		[]byte(generate.LocalRunEnv+"=0\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(t.TempDir())

	env := packageEnv(root, &bytes.Buffer{})
	if !contains(env, generate.LocalRunEnv+"=1") {
		t.Errorf("a local dev run does not set %s, so its Coval traces would be labelled as deployed", generate.LocalRunEnv)
	}
	if contains(env, generate.LocalRunEnv+"=0") {
		t.Errorf("a stale %s on disk beat the dev loop", generate.LocalRunEnv)
	}
}

func TestDevChildEnv_missingFilesAreFine(t *testing.T) {
	var warn bytes.Buffer
	env := packageEnv(t.TempDir(), &warn) // no .env or .env.local present
	if len(env) == 0 {
		t.Fatal("expected the ambient environment when .env is absent")
	}
	if warn.Len() != 0 {
		t.Errorf("missing dotenv files must not warn; got %q", warn.String())
	}
}

func TestDevChildEnv_readsPackageRootFilesOnce(t *testing.T) {
	dir := t.TempDir()
	tooLong := strings.Repeat("x", 64*1024+1)
	for _, name := range []string{".env", ".env.local"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(tooLong), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(dir)

	var warn bytes.Buffer
	packageEnv(dir, &warn)
	for _, name := range []string{".env", ".env.local"} {
		prefix := "warning: reading " + filepath.Join(dir, name) + ":"
		if got := strings.Count(warn.String(), prefix); got != 1 {
			t.Errorf("%s warned %d times, want once:\n%s", name, got, warn.String())
		}
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
	for _, want := range []string{"--no-open", "--bot-port", "--target", "--var", "UNMUTE_DEV_PORT", "talk to it"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("dev --help missing %q; got:\n%s", want, out.String())
		}
	}
}

func TestComposePreflightFailsClearlyWhenDockerIsMissing(t *testing.T) {
	restore := composeLookPath
	composeLookPath = func(string) (string, error) { return "", errors.New("not found") }
	t.Cleanup(func() { composeLookPath = restore })
	err := preflightComposeCore(context.Background(), os.Environ(), devWebMissingDockerHint)
	if err == nil || !strings.Contains(err.Error(), "docker compose is required") || !strings.Contains(err.Error(), "Docker Desktop") {
		t.Fatalf("preflight error = %v", err)
	}
}

func TestComposePreflightFailsClearlyWhenDaemonIsUnavailable(t *testing.T) {
	dir := t.TempDir()
	fake := filepath.Join(dir, "docker")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\nif [ \"$1\" = fail ]; then echo daemon-down; exit 1; fi\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	restoreLook, restoreCommand := composeLookPath, composeCommand
	composeLookPath = func(string) (string, error) { return fake, nil }
	composeCommand = func(ctx context.Context, _ string, args ...string) *exec.Cmd {
		mode := "ok"
		if len(args) > 0 && args[0] == "info" {
			mode = "fail"
		}
		return exec.CommandContext(ctx, fake, mode)
	}
	t.Cleanup(func() { composeLookPath, composeCommand = restoreLook, restoreCommand })
	err := preflightComposeCore(context.Background(), os.Environ(), devWebMissingDockerHint)
	if err == nil || !strings.Contains(err.Error(), "docker daemon is unavailable") || !strings.Contains(err.Error(), "daemon-down") {
		t.Fatalf("preflight error = %v", err)
	}
}

func TestComposePlanIsProjectScopedAndPreservesVolumes(t *testing.T) {
	project := composeProjectName("/tmp/My Agent!", "LiveKit Main")
	if !strings.HasPrefix(project, "unmute-my-agent--livekit-main-") {
		t.Fatalf("project = %q", project)
	}
	up := strings.Join(composeArgs("compose.dev.yaml", project, "up", "--build", "--detach", "--wait"), " ")
	if !strings.Contains(up, "--project-name "+project) || !strings.Contains(up, "up --build --detach --wait") {
		t.Fatalf("up args = %q", up)
	}
	down := strings.Join(composeArgs("compose.dev.yaml", project, "down", "--remove-orphans"), " ")
	if strings.Contains(down, "--volumes") || !strings.Contains(down, "--project-name "+project) {
		t.Fatalf("down args = %q", down)
	}
}

func TestSelectDevTargetAutoSelectsSoleInstance(t *testing.T) {
	data := scaffold.Data{Name: "agent"}
	data.SetTarget("livekit")
	dir := filepath.Join(t.TempDir(), "agent")
	if _, err := scaffold.Write(dir, data); err != nil {
		t.Fatal(err)
	}
	cmd := newRootCmd()
	cmd.SetIn(strings.NewReader(""))
	cmd.SetOut(&bytes.Buffer{})
	name, err := selectDevTarget(cmd, dir, "")
	if err != nil {
		t.Fatal(err)
	}
	if name != "livekit" {
		t.Fatalf("selected target = %q", name)
	}
}

func TestSelectDevTargetRequiresNameForMultipleWithoutTTY(t *testing.T) {
	dir := copySafeCore(t)
	cmd := newRootCmd()
	cmd.SetIn(strings.NewReader(""))
	cmd.SetOut(&bytes.Buffer{})
	_, err := selectDevTarget(cmd, dir, "")
	if err == nil || !strings.Contains(err.Error(), "multiple targets declared; pass --target <name>") || !strings.Contains(err.Error(), "pipecat (pipecat)") {
		t.Fatalf("selectDevTarget() error = %v", err)
	}
}

// TestDevConsoleRemoved: the flag is gone, and passing it explains itself.
// It stays registered and hidden precisely so this message is possible: an
// author with `--console` in their shell history gets told the mode moved,
// not cobra's bare "unknown flag".
// TestDevConsoleRemoved pins that --console is gone from the command surface.
//
// It used to assert the opposite of what it asserts now. The flag was kept
// registered and hidden, rejecting with a bespoke "--console was removed"
// message, on the reasoning that cobra's bare "unknown flag" would leave an
// author unsure whether they had misremembered the name. Feature 018 retired
// that tombstone: the mode has been gone long enough that a registered flag
// whose only behaviour is announcing its own absence is upkeep with no reader.
// Cobra's own error names the flag, which is the information that was missing.
func TestDevConsoleRemoved(t *testing.T) {
	dir := copySafeCore(t)
	_, err := run(t, "dev", dir, "--target", "pipecat", "--console")
	if err == nil {
		t.Fatal("--console must fail; it was removed")
	}
	// Cobra names the flag, so the author still learns which flag was wrong.
	if !strings.Contains(err.Error(), "console") {
		t.Errorf("removed-flag error %q must still name the flag", err)
	}
	if !strings.Contains(err.Error(), "unknown flag") {
		t.Errorf("expected cobra's unknown-flag error now that the tombstone is gone, got: %v", err)
	}
}

// Every flag that drove a local telephony run is gone from the command surface.
//
// They were briefly kept registered and hidden, rejecting with a bespoke
// "was removed" message. Feature 018 retired that shape for --console, on the
// reasoning that a flag whose only behaviour is announcing its own absence is
// upkeep with no reader, and cobra's own error already names the flag. The same
// rule applies here rather than an exception for these five.
func TestDevLocalTelephonyFlagsAreRemoved(t *testing.T) {
	dir := copySafeCore(t)
	for _, flag := range [][]string{
		{"--telephony"}, {"--carrier"}, {"--public-url", "https://example.test"},
		{"--to", "+15550001111"}, {"--no-webhook"},
	} {
		t.Run(flag[0], func(t *testing.T) {
			_, err := run(t, append([]string{"dev", dir, "--target", "pipecat"}, flag...)...)
			if err == nil {
				t.Fatalf("%s must fail; it was removed", flag[0])
			}
			// Cobra names the flag, so the author still learns which one was wrong.
			if !strings.Contains(err.Error(), "unknown flag") || !strings.Contains(err.Error(), flag[0]) {
				t.Errorf("removed-flag error %q must be cobra's unknown-flag error naming %s", err, flag[0])
			}
		})
	}
}

// `dev` used to refuse the managed target with "its dev runner is not
// implemented". Both managed targets are retired, so every declared target now
// has a dev runner and there is nothing to refuse. An undeclared instance is
// still refused — see TestSelectDevTargetRejectsUnknownInstance below.

func TestSelectDevTargetRejectsUnknownInstance(t *testing.T) {
	dir := copySafeCore(t)
	cmd := newRootCmd()
	cmd.SetIn(strings.NewReader(""))
	cmd.SetOut(&bytes.Buffer{})
	_, err := selectDevTarget(cmd, dir, "missing")
	if err == nil || !strings.Contains(err.Error(), `target instance "missing" is not declared`) {
		t.Fatalf("selectDevTarget() error = %v", err)
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

// dev takes the same optional package directory as validate and compile
// (contracts C7 and C8). These prove the resolution without starting Docker:
// both assertions land on checks that run immediately after the directory is
// resolved, so reaching them at all means the zero-argument form worked.

func TestDevWithNoArgumentResolvesTheCurrentDirectory(t *testing.T) {
	t.Chdir(copyPackage(t, "remy"))
	cmd := newRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	// An undeclared target is rejected just after the directory is resolved, so
	// this error means dev accepted the zero-argument form.
	cmd.SetArgs([]string{"dev", "--target", "nope"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), `target instance "nope" is not declared`) {
		t.Fatalf("dev with no directory did not reach target selection: %v", err)
	}
}

func TestDevWithNoArgumentOutsideAPackageExplainsItself(t *testing.T) {
	t.Chdir(t.TempDir())
	cmd := newRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"dev"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("dev outside a package must fail")
	}
	if !strings.Contains(err.Error(), "agent.yaml") || strings.Contains(err.Error(), "accepts 1 arg") {
		t.Fatalf("dev must explain itself, not print the cobra arity error: %v", err)
	}
}
