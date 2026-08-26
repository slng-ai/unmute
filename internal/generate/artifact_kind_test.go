package generate

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/ir"
	targetcap "github.com/slng-ai/unmute/internal/target"
)

// TestEveryProviderHasAHandledArtifactKind is the gate under FR-015, and under
// the `default` arm added to internal/cli/compile.go's kind switch.
//
// That switch had no default arm. An artifact kind nobody handled produced a
// complete file list in memory, wrote none of it, and reported success, which is
// the quietest failure in the compiler. The default arm makes it an error — and
// the arm is unreachable while artifactKind and compile.go agree about the set
// of kinds, which is exactly the agreement worth pinning rather than the arm.
//
// So this checks the two halves that can drift: every provider yields a kind,
// and every kind a provider yields is one internal/cli knows how to write.
func TestEveryProviderHasAHandledArtifactKind(t *testing.T) {
	// The kinds internal/cli/compile.go writes. Read from its source rather than
	// listed here, because a list here would be a third copy of the same fact.
	source, err := os.ReadFile(filepath.Join("..", "cli", "compile.go"))
	if err != nil {
		t.Fatal(err)
	}
	written := string(source)
	if !strings.Contains(written, "default:") {
		t.Fatal("internal/cli/compile.go's artifact-kind switch has no default arm; an unhandled kind would write nothing and report success")
	}

	known := []ArtifactKind{CodeTarget, BodyTarget}
	for _, provider := range targetcap.Providers {
		kind := artifactKind(ir.Provider(provider))
		if kind == "" {
			t.Errorf("provider %q maps to no artifact kind, so compile writes nothing for it", provider)
			continue
		}
		if !slices.Contains(known, kind) {
			t.Errorf("provider %q maps to kind %q, which this test does not know; add it here and to internal/cli/compile.go", provider, kind)
			continue
		}
		// The kind has to be named in the command that writes it. A kind the
		// switch never mentions falls to the default arm and errors, which is
		// loud but is still a package that cannot be compiled.
		if !strings.Contains(written, string(kind)) && !strings.Contains(written, kindConstantName(kind)) {
			t.Errorf("internal/cli/compile.go never names kind %q, so provider %q reaches the default arm and cannot be compiled", kind, provider)
		}
	}
	// An unknown provider still yields nothing, which is what lets ir.Validate
	// refuse it before generate is ever asked.
	if got := artifactKind(ir.Provider("a-provider-that-does-not-exist")); got != "" {
		t.Errorf("an unknown provider maps to kind %q; it must map to none", got)
	}
}

// kindConstantName is how compile.go spells a kind: by its Go constant, not by
// its string value.
func kindConstantName(kind ArtifactKind) string {
	switch kind {
	case CodeTarget:
		return "CodeTarget"
	case BodyTarget:
		return "BodyTarget"
	}
	return ""
}
