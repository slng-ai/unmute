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
