package target

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestTelephonyDocsContract holds the telephony vocabulary in the two documents
// that own it. A route, a credential name, or a cardinality rule that changes in
// code has to change here too, and a document that loses one of these terms is a
// red build rather than a page a reader follows into a dead end.
//
// Narrowed 2026-08-14: the root README and the retired docs/user/ site used to be
// in this map. The README is the front page, not a reference, and the public
// pages under docs-site/ carry their own tests. Repeating the vocabulary in a
// third and fourth place only gave it more places to rot.
func TestTelephonyDocsContract(t *testing.T) {
	root := filepath.Join("..", "..")
	checks := map[string][]string{
		"docs/SCHEMA.md": {
			"peak_starts_per_second",
			"(orchestrator, transport, carrier)",
			"connections/primary_phone.yaml",
			"any number of named targets and Connections",
		},
		"docs/TELEPHONY.md": {
			"## Route matrix and package cardinality",
			"gated routes with no adapter",
			"## Credentials",
			"TWILIO_ACCOUNT_SID",
			"TELNYX_API_KEY",
			"PLIVO_AUTH_ID",
			"EXOTEL_API_KEY",
			"LIVEKIT_API_KEY",
			// All three carriers now use one set of names on the SIP route:
			// these are standard SIP trunk settings, not one vendor's, and the
			// same emitted code dials through any of them (SCHEMA N33).
			"SIP_TRUNK_HOSTNAME",
			"SIP_AUTH_USERNAME",
			"SIP_FROM_NUMBER",
			"No generated adapter",
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
