package docsite

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// exampleRoot is the second of the four surfaces in CLAUDE.md. An example's own
// README is what somebody reads before they run the package, and it was the
// surface with nothing walking it: four stale transcripts survived this change's
// first sweep of the other two, which is why it is listed here.
const exampleRoot = "../../examples"

// retiredOutput names a line shape the CLI used to print and does not any more,
// with what a page should say instead.
//
// Every entry here was, at some point, a hand-copied transcript on a page. That
// is how they all outlived the code: nothing failed when the code stopped
// printing them, so the pages kept teaching output a reader would never see, and
// a reader who copies a sample and waits for it has no way to tell a stale doc
// from a broken install.
type retiredOutput struct {
	// needles must all appear on one line for it to match, so a shape can be
	// pinned tightly enough not to catch ordinary prose about the same subject.
	needles []string
	instead string
}

var retiredOutputs = []retiredOutput{
	{
		needles: []string{"supported frameworks:"},
		instead: "`unmute --version` prints one line. The supported framework versions are in the docs, not in the binary's output",
	},
	{
		needles: []string{": binding ", "provider="},
		instead: "compile prints the generated file list only; each resolved binding is in build/<target>/compile-report.json under `bindings`",
	},
	{
		needles: []string{"(forwarded as-is, not validated)"},
		instead: "the model string is still forwarded without being checked, and compile-report.json records it, but no line says so on the terminal",
	},
	{
		needles: []string{": sizing ", "["},
		instead: "each derived number is in compile-report.json under `sizing`, with the same status tag and basis",
	},
	{
		// provider= is what makes this the printed line rather than the
		// validation error that refuses a transfer, which says "telephony
		// warm_transfer: telephony route (pipecat, cloud-websocket, twilio)
		// does not ..." and is still very much printed.
		needles: []string{": telephony route ", "provider="},
		instead: "the resolved route is in compile-report.json under `telephony`",
	},
	{
		needles: []string{": telephony evidence ", "docs="},
		instead: "route maturity is in compile-report.json under `telephony`, in the same evidence shape",
	},
	{
		needles: []string{": telephony endpoint "},
		instead: "the public endpoints are in compile-report.json under `telephony`",
	},
	{
		needles: []string{": telephony required env "},
		instead: "the required variables are in compile-report.json under `required_env`",
	},
	{
		needles: []string{": turn pace ", "closes at"},
		instead: "the emitted build/<target>/README.md names the floor and the ceiling, and compile-report.json carries the same line under `notes`",
	},
	{
		needles: []string{"turn role lowers to"},
		instead: "the emitted runbook explains where the turn detector ends up; compile-report.json has it under `notes`",
	},
	{
		needles: []string{"document(s), embed "},
		instead: "compile-report.json records what each knowledge base carries, under `notes`",
	},
	// The eight advisory capability warnings, deleted from the table rather than
	// hidden at the print. Four were driver TODOs addressed to a maintainer and
	// four stated a fact about a framework, and all eight fired on every run of a
	// package with nothing wrong with it, which taught readers to skip the block
	// that also carries the undeclared secret and the tool call that 400s.
	{
		needles: []string{"LiveKit turn placement is a preference"},
		instead: "LiveKit places the turn detector itself; teach that as a fact about the target on the turn-taking page",
	},
	{
		needles: []string{"LiveKit TaskGroup is experimental"},
		instead: "task groups stand on a LiveKit beta; say so in prose where somebody is choosing the shape",
	},
	{
		needles: []string{"has no greeting block: the agent opens with a model-written line"},
		instead: "document what omitting a greeting does on the conversation page",
	},
	{
		needles: []string{"driver must range-check inactivity durations"},
		instead: "nothing: this was a note to a maintainer that had leaked into the author's terminal",
	},
	{
		needles: []string{"driver must verify a max-duration cap"},
		instead: "nothing: this was a note to a maintainer that had leaked into the author's terminal",
	},
}

// TestNoPageQuotesRetiredCLIOutput fails when a reader-facing surface quotes a
// line the CLI no longer prints.
//
// The pages and the shipped skill are held to the same rule, for the same
// reason: a coding agent reads the skill the way a person reads a page, and a
// package written against a sample that does not exist is wrong before anybody
// runs it.
//
// This is the gate the four surfaces in CLAUDE.md were missing. A change to what
// the CLI prints now fails here until every surface that quoted it is updated,
// which is cheaper than finding out from somebody who copied a doc.
func TestNoPageQuotesRetiredCLIOutput(t *testing.T) {
	for _, root := range []string{siteRoot, skillRefs, exampleRoot} {
		err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if skipSnippets(path, entry) {
				return filepath.SkipDir
			}
			if entry.IsDir() {
				// build/ under an example is generated output rather than a
				// surface somebody maintains, and it is rewritten on every
				// compile.
				if entry.Name() == "build" {
					return filepath.SkipDir
				}
				return nil
			}
			switch filepath.Ext(path) {
			case ".mdx", ".md":
			default:
				return nil
			}
			// Generated from GitHub Releases, and every entry was accurate when
			// it was written. Rewriting history to match a later decision makes
			// it a worse record, not a better one.
			if filepath.Base(path) == "changelog.mdx" {
				return nil
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			for i, line := range strings.Split(string(raw), "\n") {
				for _, retired := range retiredOutputs {
					if !allPresent(line, retired.needles) {
						continue
					}
					t.Errorf("%s:%d quotes output the CLI no longer prints.\n\tline:    %s\n\tinstead: %s",
						path, i+1, strings.TrimSpace(line), retired.instead)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

func allPresent(line string, needles []string) bool {
	for _, needle := range needles {
		if !strings.Contains(line, needle) {
			return false
		}
	}
	return true
}
