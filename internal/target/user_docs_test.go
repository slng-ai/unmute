package target

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestUserDocsCodeTargetParity(t *testing.T) {
	root := filepath.Join("..", "..", "docs", "user")
	checks := []struct {
		path  string
		terms []string
	}{
		{"README.md", []string{"targets/livekit.md", "targets/pipecat.md", "agent.py", "bot.py"}},
		{"start/first-agent.md", []string{"provider: livekit", "provider: pipecat"}},
		{"concepts/how-targets-run-your-agent.md", []string{"AgentTask", "TaskGroup", "worker", "Flow"}},
		{"learn/08-going-live.md", []string{"LiveKit and Pipecat have shipped code", "derived sizing line"}},
	}
	for _, check := range checks {
		raw, err := os.ReadFile(filepath.Join(root, check.path))
		if err != nil {
			t.Fatal(err)
		}
		for _, term := range check.terms {
			if !strings.Contains(string(raw), term) {
				t.Errorf("%s does not document %q", check.path, term)
			}
		}
	}

	stale := []string{
		"only the Pipecat driver ships today",
		"compile the one whose driver is ready (Pipecat today)",
		"Pipecat is the target these docs build toward",
		"This documentation focuses on Pipecat",
		"most complete one today",
	}
	if err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || filepath.Ext(path) != ".md" {
			return err
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, phrase := range stale {
			if strings.Contains(string(raw), phrase) {
				t.Errorf("%s contains stale claim %q", path, phrase)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestUserDocsRelativeLinksResolve(t *testing.T) {
	root := filepath.Join("..", "..", "docs", "user")
	link := regexp.MustCompile(`\]\(([^)]+)\)`)
	if err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || filepath.Ext(path) != ".md" {
			return err
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, match := range link.FindAllStringSubmatch(string(raw), -1) {
			target := strings.Trim(strings.Fields(match[1])[0], "<>")
			if strings.HasPrefix(target, "#") || strings.Contains(target, "://") ||
				strings.HasPrefix(target, "mailto:") {
				continue
			}
			target, _, _ = strings.Cut(target, "#")
			if target == "" {
				continue
			}
			if _, err := os.Stat(filepath.Join(filepath.Dir(path), filepath.FromSlash(target))); err != nil {
				t.Errorf("%s links to missing %s", path, target)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestTelephonyDocsContract(t *testing.T) {
	root := filepath.Join("..", "..")
	checks := map[string][]string{
		"README.md": {"## Telephony", "connections/<name>.yaml", "pipecat_twilio", "TELEPHONY.md"},
		"docs/SCHEMA.md": {
			"peak_starts_per_second",
			"(orchestrator, transport, carrier)",
			"connections/primary_phone.yaml",
			"any number of named targets and Connections",
		},
		"CONTEXT.md": {"**Telephony route**", "**Coordination mode**"},
		"docs/TELEPHONY.md": {
			"## Route matrix and package cardinality",
			"reject every telephony target",
			"## Credentials",
			"TWILIO_ACCOUNT_SID",
			"TELNYX_API_KEY",
			"PLIVO_AUTH_ID",
			"EXOTEL_API_KEY",
			"LIVEKIT_API_KEY",
			"LIVEKIT_SIP_URI",
			"TWILIO_SIP_ADDRESS",
			"TELNYX_SIP_ADDRESS",
			"PLIVO_SIP_ADDRESS",
			"No generated adapter",
		},
		"docs/user/learn/07-phone-calls.md": {
			"## Choose a supported carrier route", "## Configure multiple carriers",
			"## Configure telephony by orchestrator", "Pipecat does not use your carrier's SIP trunk",
			"### Configure self-hosted LiveKit SIP", "LIVEKIT_SIP_INBOUND_TRUNK", "10000-10100",
		},
		"docs/user/reference/targets-yaml.md": {"## Multiple telephony routes", "pipecat_twilio", "livekit_telnyx"},
		"docs/user/reference/providers.md":    {"## Use several providers in one target", "Telephony uses **carrier**"},
		"docs/user/targets/pipecat.md":        {"### Telephony carrier integrations", "TELNYX_CONNECTION_ID", "PLIVO_AUTH_ID"},
		"docs/user/targets/livekit.md": {
			"TELNYX_SIP_ADDRESS", "PLIVO_SIP_ADDRESS", "LIVEKIT_SIP_URI", "not an audio buffer", "No emitted adapter", "10000-10100",
		},
	}
	for path, terms := range checks {
		raw, err := os.ReadFile(filepath.Join(root, path))
		if err != nil {
			t.Fatal(err)
		}
		for _, term := range terms {
			if !strings.Contains(string(raw), term) {
				t.Errorf("%s does not document %q", path, term)
			}
		}
	}
}
