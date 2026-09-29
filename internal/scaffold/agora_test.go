package scaffold

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAgoraStarterRefusesBeforeWriting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "new-agent")
	_, err := Write(path, Data{Name: "new-agent", Target: "agora"})
	if err == nil || !strings.Contains(err.Error(), "examples/agora-voice") {
		t.Fatalf("want example guidance, got %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("refusal wrote a partial package: %v", err)
	}
}
