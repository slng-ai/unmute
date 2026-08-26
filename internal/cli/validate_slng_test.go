package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// L2 coverage for the slng target's refusals, through the real command tree.
//
// The unit tests in internal/ir prove each rule fires. These prove the author
// actually reads it: the message reaches stderr under Errors, the command exits
// non-zero, and nothing is written. A rule that fires into a buffer nobody
// prints is not a refusal.
//
// Spec SC-003 asks for three things in every such message — the behaviour, the
// target, and what to do instead — so all three are asserted rather than just
// the fact of failure.
func copySlngCore(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "agent")
	if err := os.CopyFS(dir, os.DirFS(filepath.Join("..", "testdata", "slng_core"))); err != nil {
		t.Fatal(err)
	}
	return dir
}

func appendToSlngFile(t *testing.T, dir, name, text string) {
	t.Helper()
	path := filepath.Join(dir, name)
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(content, []byte(text)...), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestValidateSlngCorePasses(t *testing.T) {
	stdout, stderr, err := runValidateCommand(t, copySlngCore(t))
	if err != nil {
		t.Fatalf("the slng baseline must validate: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	if !strings.Contains(stdout, "✓ slng (slng)") {
		t.Errorf("stdout does not report the slng target:\n%s", stdout)
	}
	if strings.Contains(stderr, "Errors:") || strings.Contains(stderr, "Warnings:") {
		t.Errorf("the baseline produced output it should not:\n%s", stderr)
	}
}

// The four project-only settings, each written the way an author writes it: one
// more key on the target block. All four used to pass in silence (research R5).
func TestValidateSlngRefusesProjectOnlySettings(t *testing.T) {
	for _, test := range []struct {
		name string
		// key is appended to targets.yaml under the slng target.
		key string
		// behaviour and fix are two of the three parts SC-003 requires; the third,
		// the target, is asserted for every case in assertSlngRefusal.
		behaviour string
		fix       string
	}{
		{"version", "    version: \"1.6.10\"\n", "does not take version", "SLNG owns the runtime version"},
		{"sdk_language", "    sdk_language: python\n", "does not take sdk_language", "remove the field"},
		{"pins", "    pins:\n      livekit-agents: \"1.6.10\"\n", "does not take pins", "remove the field"},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := copySlngCore(t)
			appendToSlngFile(t, dir, "targets.yaml", test.key)
			stdout, stderr, err := runValidateCommand(t, dir)
			if err == nil {
				t.Fatalf("%s was accepted\nstdout=%s\nstderr=%s", test.name, stdout, stderr)
			}
			assertSlngRefusal(t, stdout, stderr, test.behaviour, test.fix)
		})
	}
}

// connection: is the fourth project-only setting, and it never reaches the slng
// pass. Build resolves a connection into a route first, finds slng has no phone
// routes, and refuses with the connection file's own name and line — which is
// strictly better than a target-level message, because it points at the file the
// author has to change.
//
// ir.refuseSlngProjectValues still refuses a Connection, as a backstop for a
// resolved Target assembled some other way, and internal/ir covers that. This
// test pins the ordering, so a future change that moves the connection check and
// loses the position has to say so.
func TestValidateSlngRefusesAConnectionAtBuildWithAPosition(t *testing.T) {
	dir := copySlngCore(t)
	if err := os.MkdirAll(filepath.Join(dir, "connections"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "connections", "primary_phone.yaml"),
		[]byte("transport: carrier-websocket\ncarrier: twilio\nenvironment:\n  account_sid: TWILIO_ACCOUNT_SID\n  auth_token: TWILIO_AUTH_TOKEN\n  from_number: TWILIO_PHONE_NUMBER\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	appendToSlngFile(t, dir, "targets.yaml", "    connection: primary_phone\n")
	_, _, err := runValidateCommand(t, dir)
	if err == nil {
		t.Fatal("a connection on a slng target was accepted")
	}
	for _, want := range []string{"connections/primary_phone.yaml:1", "provider slng", "no phone routes"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not contain %q: %v", want, err)
		}
	}
}

// The refusals that need a whole block rather than one appended line. Each
// rewrites one file, which is closer to what an author actually does than
// patching a single key.
func TestValidateSlngRefusesUnsupportedPackageShapes(t *testing.T) {
	for _, test := range []struct {
		name      string
		file      string
		replace   string
		with      string
		behaviour string
		fix       string
	}{
		{
			name: "region outside the four", file: "targets.yaml",
			replace: "deployment_region: eu-central", with: "deployment_region: eu-west",
			behaviour: `does not deploy to region "eu-west"`, fix: "ap-south",
		},
		{
			name: "two regions", file: "targets.yaml",
			replace: "deployment_region: eu-central", with: "deployment_region: [us-east, eu-central]",
			behaviour: "takes exactly one deployment region", fix: "name one of",
		},
		{
			name: "inactivity", file: "agent.yaml",
			replace: "  interruption:\n    enabled: true", with: "  interruption:\n    enabled: true\n  inactivity:\n    nudge_after: 15s\n    end_after: 45s",
			behaviour: "cannot carry an inactivity window", fix: "remove conversation.inactivity",
		},
		{
			name: "max_duration", file: "agent.yaml",
			replace: "  interruption:\n    enabled: true", with: "  interruption:\n    enabled: true\n  max_duration: 20m",
			behaviour: "no maximum call duration", fix: "cap the call in the SLNG dashboard",
		},
		{
			name: "minimum_words", file: "agent.yaml",
			replace: "    enabled: true", with: "    enabled: true\n    minimum_words: 2",
			behaviour: "no minimum word count", fix: "remove minimum_words",
		},
		{
			name: "no greeting", file: "agent.yaml",
			replace: "  greeting:\n    speaks_first: agent\n    text: \"Hi {{customer_name}}, you have reached Acme Support. How can I help?\"\n",
			with:    "  greeting:\n    speaks_first: user\n",
			// speaks_first: user is the honest way to ask for no greeting, and SLNG
			// requires one, so this is the refusal an author actually meets.
			behaviour: "cannot wait for the caller to speak first", fix: "conversation.greeting.text",
		},
		{
			name: "a turn model", file: "agent.yaml",
			replace: "  listen:\n    transcriber:",
			with: "  turn:\n    vad:\n      description: end-of-turn preference\n      provider: local\n      model: silero\n" +
				"  listen:\n    transcriber:",
			behaviour: "owns its own turn taking", fix: "remove the turn binding",
		},
		{
			name: "langfuse tracing", file: "agent.yaml",
			replace: "channels:", with: "tracing:\n  provider: langfuse\n\nchannels:",
			behaviour: "cannot install a Langfuse exporter", fix: "read the traces in the SLNG dashboard",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := copySlngCore(t)
			path := filepath.Join(dir, test.file)
			content, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			updated := strings.Replace(string(content), test.replace, test.with, 1)
			if updated == string(content) {
				t.Fatalf("the fixture no longer contains %q, so this test edits nothing", test.replace)
			}
			if err := os.WriteFile(path, []byte(updated), 0o644); err != nil {
				t.Fatal(err)
			}
			stdout, stderr, err := runValidateCommand(t, dir)
			if err == nil {
				t.Fatalf("%s was accepted\nstdout=%s\nstderr=%s", test.name, stdout, stderr)
			}
			assertSlngRefusal(t, stdout, stderr, test.behaviour, test.fix)
		})
	}
}

// assertSlngRefusal holds the whole shape of a good refusal in one place: the
// target is marked failed on stdout, the reason is under Errors on stderr, it
// names the target so it cannot be read as a message about the slng model
// vendor, it names the behaviour, and it says what to do instead.
func assertSlngRefusal(t *testing.T, stdout, stderr, behaviour, fix string) {
	t.Helper()
	if !strings.Contains(stdout, "✗ slng (slng)") {
		t.Errorf("stdout does not mark the slng target failed:\n%s", stdout)
	}
	if !strings.Contains(stderr, "Errors:") {
		t.Errorf("stderr carries no Errors section:\n%s", stderr)
	}
	if !strings.Contains(stderr, "slng target") {
		t.Errorf("the message does not name the target, so it reads as a message about the slng model vendor:\n%s", stderr)
	}
	if !strings.Contains(stderr, behaviour) {
		t.Errorf("the message does not name the behaviour %q:\n%s", behaviour, stderr)
	}
	if !strings.Contains(stderr, fix) {
		t.Errorf("the message does not say what to do instead (%q):\n%s", fix, stderr)
	}
}

// Nothing is written, whether validation passes or fails. `validate` is the
// command an author runs to find out before anything happens.
func TestValidateSlngWritesNothing(t *testing.T) {
	for _, broken := range []bool{false, true} {
		dir := copySlngCore(t)
		if broken {
			appendToSlngFile(t, dir, "targets.yaml", "    version: \"1.6.10\"\n")
		}
		before := listSlngFiles(t, dir)
		_, _, _ = runValidateCommand(t, dir)
		if after := listSlngFiles(t, dir); after != before {
			t.Errorf("validate changed the package tree (broken=%v):\nbefore %s\nafter  %s", broken, before, after)
		}
		if _, err := os.Stat(filepath.Join(dir, "build")); !os.IsNotExist(err) {
			t.Errorf("validate wrote a build directory (broken=%v)", broken)
		}
	}
}

func listSlngFiles(t *testing.T, dir string) string {
	t.Helper()
	var found []string
	err := filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() {
			found = append(found, path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return strings.Join(found, "\n")
}
