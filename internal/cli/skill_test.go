package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// runSkillInstallCommand drives the real command tree with output captured, so
// these tests fail on anything a user would see, including the wording.
func runSkillInstallCommand(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	cmd := newRootCmd()
	cmd.Version = "test"
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs(append([]string{"skill", "install"}, args...))
	err := cmd.Execute()
	return stdout.String(), stderr.String(), err
}

func canonicalDir(project string) string {
	return filepath.Join(project, ".agents", "skills", "unmute")
}

func pointerDir(project string) string {
	return filepath.Join(project, ".claude", "skills", "unmute")
}

func TestSkillInstallWritesBothDestinations(t *testing.T) {
	project := t.TempDir()
	stdout, stderr, err := runSkillInstallCommand(t, "--dir", project)
	if err != nil {
		t.Fatal(err)
	}
	if stderr != "" {
		t.Errorf("a clean install wrote to stderr: %q", stderr)
	}
	for _, want := range []string{
		".agents/skills/unmute/",
		".claude/skills/unmute/",
		"SKILL.md",
		"written",
		"Commit these files",
		"Next: ask your assistant",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout missing %q:\n%s", want, stdout)
		}
	}
	for _, dir := range []string{canonicalDir(project), pointerDir(project)} {
		if _, err := os.Stat(filepath.Join(dir, "SKILL.md")); err != nil {
			t.Errorf("%s: %v", dir, err)
		}
		if _, err := os.Stat(filepath.Join(dir, ".unmute-manifest.json")); err != nil {
			t.Errorf("%s: %v", dir, err)
		}
	}
}

// TestSkillInstallTouchesNothingElse holds the contract's "two destinations
// only" guarantee. A command that quietly edits AGENTS.md or .github/ is not
// the command that was documented.
func TestSkillInstallTouchesNothingElse(t *testing.T) {
	project := t.TempDir()
	if _, _, err := runSkillInstallCommand(t, "--dir", project); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(project)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	if strings.Join(names, ",") != ".agents,.claude" {
		t.Errorf("the install created %v, want only .agents and .claude", names)
	}
}

func TestSkillInstallSecondRunSaysSo(t *testing.T) {
	project := t.TempDir()
	if _, _, err := runSkillInstallCommand(t, "--dir", project); err != nil {
		t.Fatal(err)
	}
	stdout, _, err := runSkillInstallCommand(t, "--dir", project)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout, "left alone") {
		t.Errorf("a second run must be distinguishable from a fresh install:\n%s", stdout)
	}
	if strings.Contains(stdout, "written") {
		t.Errorf("a second run reported a write:\n%s", stdout)
	}
}

func TestSkillInstallReportsTheVersionItUpgradedFrom(t *testing.T) {
	project := t.TempDir()

	// A previous install, at a version this binary is not.
	old := newRootCmd()
	old.Version = "0.0.1"
	var buf bytes.Buffer
	old.SetOut(&buf)
	old.SetErr(&buf)
	old.SetArgs([]string{"skill", "install", "--dir", project})
	if err := old.Execute(); err != nil {
		t.Fatal(err)
	}

	stdout, _, err := runSkillInstallCommand(t, "--dir", project)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout, "upgraded from 0.0.1") {
		t.Errorf("the upgrade was silent:\n%s", stdout)
	}
}

func TestSkillInstallRefusesLocallyChangedFiles(t *testing.T) {
	project := t.TempDir()
	if _, _, err := runSkillInstallCommand(t, "--dir", project); err != nil {
		t.Fatal(err)
	}
	edited := filepath.Join(canonicalDir(project), "SKILL.md")
	if err := os.WriteFile(edited, []byte("mine now\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, _, err := runSkillInstallCommand(t, "--dir", project)
	if err == nil {
		t.Fatal("the install overwrote a hand-edited file")
	}
	for _, want := range []string{".agents/skills/unmute/SKILL.md", "--force"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not mention %q:\n%s", want, err)
		}
	}
	raw, readErr := os.ReadFile(edited)
	if readErr != nil || string(raw) != "mine now\n" {
		t.Errorf("the local edit was lost: %q %v", raw, readErr)
	}
}

func TestSkillInstallForceOverwrites(t *testing.T) {
	project := t.TempDir()
	if _, _, err := runSkillInstallCommand(t, "--dir", project); err != nil {
		t.Fatal(err)
	}
	edited := filepath.Join(canonicalDir(project), "SKILL.md")
	if err := os.WriteFile(edited, []byte("mine now\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := runSkillInstallCommand(t, "--dir", project, "--force"); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(edited)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) == "mine now\n" {
		t.Error("--force did not overwrite")
	}
}

func TestSkillInstallAgentCodexWritesOnlyTheCanonicalDestination(t *testing.T) {
	project := t.TempDir()
	stdout, _, err := runSkillInstallCommand(t, "--dir", project, "--agent", "codex")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(canonicalDir(project)); err != nil {
		t.Errorf("the canonical destination is missing: %v", err)
	}
	if _, err := os.Stat(pointerDir(project)); !os.IsNotExist(err) {
		t.Error(".claude was written for --agent codex")
	}
	if !strings.Contains(stdout, "for codex.") {
		t.Errorf("the summary does not name what was asked for:\n%s", stdout)
	}
}

func TestSkillInstallDeduplicatesSharedDestinations(t *testing.T) {
	project := t.TempDir()
	stdout, _, err := runSkillInstallCommand(t, "--dir", project, "--agent", "codex,cursor,copilot")
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(stdout, ".agents/skills/unmute/"); got != 1 {
		t.Errorf("three assistants sharing one directory printed it %d times:\n%s", got, stdout)
	}
}

func TestSkillInstallUnknownAgentFailsAndLists(t *testing.T) {
	project := t.TempDir()
	_, _, err := runSkillInstallCommand(t, "--dir", project, "--agent", "emacs")
	if err == nil {
		t.Fatal("an unknown assistant was accepted")
	}
	if !strings.Contains(err.Error(), "emacs") {
		t.Errorf("the error does not name the value it rejected:\n%s", err)
	}
	for _, name := range []string{"all", "claude", "codex", "copilot", "cursor"} {
		if !strings.Contains(err.Error(), name) {
			t.Errorf("the error does not list the supported name %q:\n%s", name, err)
		}
	}
	if _, statErr := os.Stat(canonicalDir(project)); !os.IsNotExist(statErr) {
		t.Error("a rejected --agent still wrote files: it must never fall back to all")
	}
}

func TestSkillInstallUnwritableDirectoryFailsWithThePath(t *testing.T) {
	project := t.TempDir()
	// A file where .agents must be a directory. The path is unwritable for a
	// reason the user can see and fix.
	if err := os.WriteFile(filepath.Join(project, ".agents"), []byte("not a directory\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, _, err := runSkillInstallCommand(t, "--dir", project)
	if err == nil {
		t.Fatal("the install succeeded with a file in .agents's place")
	}
	if !strings.Contains(err.Error(), ".agents") {
		t.Errorf("the error does not name the path:\n%s", err)
	}
}

// TestSkillWithNoSubcommandPrintsHelp holds the contract line that bare
// `unmute skill` behaves like the root does with no arguments.
func TestSkillWithNoSubcommandPrintsHelp(t *testing.T) {
	cmd := newRootCmd()
	cmd.Version = "test"
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stdout)
	cmd.SetArgs([]string{"skill"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "install") {
		t.Errorf("bare `unmute skill` did not print its help:\n%s", stdout.String())
	}
}
