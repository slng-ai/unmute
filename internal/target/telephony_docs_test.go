package target

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestTelephonyDocsContract holds the route vocabulary in the public pages that
// own it. A route, credential key, or cardinality rule that changes in code has
// to change here too.
func TestTelephonyDocsContract(t *testing.T) {
	root := filepath.Join("..", "..")
	checks := map[string][]string{
		"docs-site/telephony/overview.mdx": {
			"peak_starts_per_second",
			"One target selects exactly one route and one connection",
			"cloud-websocket",
			"daily-sip",
			"connector",
			"no adapter, so this route is refused at validation",
		},
		"docs-site/reference/connections-yaml.mdx": {
			"## The two shapes",
			"## Which environment keys a route accepts",
			"account_sid",
			"sip_address",
			"sip_username",
			"from_number",
			"## One target, one connection",
			"not accepted by route",
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

func TestCarrierlessDailyAuthoringIsNotDocumented(t *testing.T) {
	root := filepath.Join("..", "..")
	for _, path := range []string{
		"docs-site/telephony/overview.mdx",
		"docs-site/transfers/overview.mdx",
		"docs-site/reference/connections-yaml.mdx",
		"internal/generate/templates/pipecat_v1/README.md.tmpl",
		"internal/skill/assets/references/telephony.md",
		"internal/skill/assets/references/transfers.md",
	} {
		raw, err := os.ReadFile(filepath.Join(root, path))
		if err != nil {
			t.Fatal(err)
		}
		content := string(raw)
		for _, forbidden := range []string{
			"Carrierless Daily dial-out",
			"carrierless Daily",
			"no-carrier Daily",
			"daily-sip with no carrier",
			"can dial a transfer destination",
			"control that dials a person",
			"Cold, with no carrier",
			"Which carrier**, or none at all?",
		} {
			if strings.Contains(content, forbidden) {
				t.Errorf("%s presents the removed carrierless daily-sip route: %q", path, forbidden)
			}
		}
	}

	checks := map[string][]string{
		"docs-site/transfers/overview.mdx":              {"Pipecat `daily-sip` + Twilio", "active `channels.phone` route"},
		"docs-site/reference/connections-yaml.mdx":      {"| Pipecat | `daily-sip` | `twilio` |"},
		"internal/skill/assets/references/transfers.md": {"Pipecat `daily-sip` + Twilio", "active phone"},
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
