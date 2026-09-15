package scaffold

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDraftDestinationSafety(t *testing.T) {
	raw := []byte("manifest: acme\nversion: 1\n")
	for _, kind := range []string{"empty", "occupied", "file", "invalid-name", "bad-parent"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			dir := filepath.Join(root, "agent")
			switch kind {
			case "empty", "occupied":
				if err := os.Mkdir(dir, 0755); err != nil {
					t.Fatal(err)
				}
				if kind == "occupied" {
					if err := os.WriteFile(filepath.Join(dir, "keep"), []byte("untouched"), 0600); err != nil {
						t.Fatal(err)
					}
				}
			case "file":
				if err := os.WriteFile(dir, []byte("untouched"), 0600); err != nil {
					t.Fatal(err)
				}
			case "invalid-name":
				dir = filepath.Join(root, "---")
			case "bad-parent":
				if err := os.WriteFile(dir, []byte("untouched"), 0600); err != nil {
					t.Fatal(err)
				}
				dir = filepath.Join(dir, "child")
			}
			_, err := WriteDraft(dir, raw)
			if (err == nil) != (kind == "empty") {
				t.Fatalf("unexpected result: %v", err)
			}
			if kind == "occupied" {
				b, err := os.ReadFile(filepath.Join(dir, "keep"))
				if err != nil || string(b) != "untouched" {
					t.Fatal("occupied changed")
				}
			}
			entries, err := os.ReadDir(root)
			if err != nil {
				t.Fatal(err)
			}
			for _, e := range entries {
				if e.Name() != "agent" {
					t.Fatalf("failed draft left file %s", e.Name())
				}
			}
		})
	}
}
