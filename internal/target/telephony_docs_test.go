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
			"carrier-websocket",
			"daily-sip",
			"no adapter, so this route is refused at validation",
		},
		"docs-site/reference/connections-yaml.mdx": {
			"## The three shapes",
			"## Which environment keys a route accepts",
			"account_sid",
			"sip_address",
			"api_key",
			"auth_id",
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

func TestCarrierlessDailyIsNotDocumentedAsInbound(t *testing.T) {
	root := filepath.Join("..", "..")
	for _, path := range []string{
		"docs-site/telephony/overview.mdx",
		"docs-site/targets/overview.mdx",
		"docs-site/reference/connections-yaml.mdx",
		"internal/generate/templates/pipecat_v1/README.md.tmpl",
		"internal/skill/assets/references/telephony.md",
	} {
		raw, err := os.ReadFile(filepath.Join(root, path))
		if err != nil {
			t.Fatal(err)
		}
		content := string(raw)
		for _, forbidden := range []string{
			"| Pipecat | `daily-sip` | none |",
			"Daily provisions the number",
			"Daily provisioned numbers",
			"This agent answers phone calls over Daily PSTN",
		} {
			if strings.Contains(content, forbidden) {
				t.Errorf("%s presents carrierless daily-sip as an inbound route: %q", path, forbidden)
			}
		}
	}
}
