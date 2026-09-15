package manifest

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestSavedManifests(t *testing.T) {
	s := Store{Root: t.TempDir()}
	if got, err := s.Default(); err != nil || got != nil {
		t.Fatalf("empty default: %v %v", got, err)
	}
	data := []byte("manifest: Acme\nversion: 1\n")
	path, err := s.Create("acme", data)
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.Default()
	if err != nil || got.Name != "acme" || string(got.Data) != string(data) || got.Path != path {
		t.Fatalf("default: %#v %v", got, err)
	}
	if _, err := s.Create("acme", data); err == nil {
		t.Fatal("overwrote manifest")
	}
	if _, err := s.Create("../escape", data); err == nil {
		t.Fatal("accepted path")
	}
	if _, err := s.Create("bad", []byte("manifest: bad\nversion: 0\n")); err == nil {
		t.Fatal("accepted invalid revision")
	}
	if _, err := s.Create("second", data); err != nil {
		t.Fatal(err)
	}
	got, _ = s.Default()
	if got.Name != "acme" {
		t.Fatal("replaced default")
	}
	if err := s.Use("second"); err != nil {
		t.Fatal(err)
	}
	list, err := s.List()
	if err != nil || len(list) != 2 {
		t.Fatalf("list: %v %v", list, err)
	}
	if err := os.Remove(filepath.Join(s.Root, "manifests", "second", "manifest")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Default(); err == nil {
		t.Fatal("silently ignored missing default")
	}
	if err := s.Use("missing"); err == nil {
		t.Fatal("accepted missing manifest")
	}
}

func TestConcurrentCreateNeverOverwrites(t *testing.T) {
	s := Store{Root: t.TempDir()}
	var wg sync.WaitGroup
	successes := make(chan bool, 2)
	for range 2 {
		wg.Go(func() { _, err := s.Create("same", []byte("manifest: Acme\nversion: 1\n")); successes <- err == nil })
	}
	wg.Wait()
	close(successes)
	count := 0
	for ok := range successes {
		if ok {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("successful creates: %d", count)
	}
	if _, err := s.Default(); err != nil {
		t.Fatal(err)
	}
}

func TestUpdateBacksUpExactBytesAndCanRepairInvalidYAML(t *testing.T) {
	s := Store{Root: t.TempDir()}
	original := []byte("# hand-written contract\nmanifest: Acme\nversion: 7\n")
	path, err := s.Create("acme", original)
	if err != nil {
		t.Fatal(err)
	}
	replacement := []byte("manifest: Acme\nversion: 7\nlanguages:\n  allow:\n    - es\n")
	got, backup, err := s.Update("acme", replacement)
	if err != nil || got != path || backup == "" {
		t.Fatalf("update: %s %s %v", got, backup, err)
	}
	old, err := os.ReadFile(backup)
	if err != nil || string(old) != string(original) {
		t.Fatalf("backup: %q %v", old, err)
	}
	if _, backup, err := s.Update("acme", replacement); err != nil || backup != "" {
		t.Fatalf("unchanged update: %s %v", backup, err)
	}
	if _, _, err := s.Update("acme", []byte("invalid: true\n")); err == nil {
		t.Fatal("invalid update accepted")
	}
	kept, err := os.ReadFile(path)
	if err != nil || string(kept) != string(replacement) {
		t.Fatal("invalid update changed saved file")
	}
	broken := []byte("manifest: [broken")
	if err := os.WriteFile(path, broken, 0600); err != nil {
		t.Fatal(err)
	}
	_, backup, err = s.Update("acme", replacement)
	if err != nil {
		t.Fatal(err)
	}
	old, err = os.ReadFile(backup)
	if err != nil || string(old) != string(broken) {
		t.Fatal("repair lost invalid original")
	}
	selected, err := s.Default()
	if err != nil || selected.Name != "acme" {
		t.Fatal("editing changed default")
	}
	if _, _, err := s.Update("missing", replacement); err == nil {
		t.Fatal("edit created missing manifest")
	}
	if _, _, err := s.Update("../escape", replacement); err == nil {
		t.Fatal("edit escaped library")
	}
}

func TestBackupFailureLeavesManifestUntouched(t *testing.T) {
	s := Store{Root: t.TempDir()}
	original := []byte("manifest: Acme\nversion: 1\n")
	path, err := s.Create("acme", original)
	if err != nil {
		t.Fatal(err)
	}
	directory := filepath.Dir(path)
	if err := os.Chmod(directory, 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(directory, 0700) })
	if _, _, err := s.Update("acme", []byte("manifest: Acme\nversion: 2\n")); err == nil {
		t.Fatal("expected backup failure in unwritable directory")
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != string(original) {
		t.Fatal("backup failure changed manifest")
	}
}
