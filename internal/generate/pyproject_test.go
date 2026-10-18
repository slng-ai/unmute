package generate

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The emitted-python CI job installs ruff so compile formats with it, and that
// has to be the ruff every generated project pins, or the job checks layout a
// user's project would never have.
func TestCIFormatsWithThePinnedRuff(t *testing.T) {
	pin := regexp.MustCompile(`"ruff==([0-9.]+)"`).FindStringSubmatch(pythonCheckersFormat)
	if pin == nil {
		t.Fatal("pythonCheckers pins no ruff version")
	}
	ci, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", "ci.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(ci), "ruff=="+pin[1]) {
		t.Errorf("ci.yml does not install ruff==%s, the version generated projects pin", pin[1])
	}
}
