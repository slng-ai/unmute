// Package manifest stores reusable organization contracts on this computer.
package manifest

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/slng-ai/unmute/internal/spec"
)

type Store struct{ Root string }
type Saved struct {
	Name, Path string
	Data       []byte
	Rules      *spec.Manifest
}

// Path resolves a local name without parsing the file, so an editor can repair it.
func (s Store) Path(name string) (string, error) {
	if err := ValidateName(name); err != nil {
		return "", err
	}
	return filepath.Join(s.Root, "manifests", name, "manifest"), nil
}

// Update keeps the exact previous bytes before replacing a saved manifest.
func (s Store) Update(name string, data []byte) (path, backup string, err error) {
	path, err = s.Path(name)
	if err != nil {
		return "", "", err
	}
	if _, err := spec.ParseManifest(data); err != nil {
		return "", "", err
	}
	old, err := os.ReadFile(path)
	if err != nil {
		return "", "", fmt.Errorf("read manifest %s: %w", name, err)
	}
	if bytes.Equal(old, data) {
		return path, "", nil
	}
	f, err := os.CreateTemp(filepath.Dir(path), "manifest.backup-*")
	if err != nil {
		return "", "", fmt.Errorf("back up manifest: %w", err)
	}
	backup = f.Name()
	if _, err := f.Write(old); err != nil {
		_ = f.Close()
		_ = os.Remove(backup)
		return "", "", fmt.Errorf("back up manifest: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(backup)
		return "", "", fmt.Errorf("close manifest backup: %w", err)
	}
	if err := atomicWrite(path, data); err != nil {
		return "", backup, fmt.Errorf("backup kept at %s: %w", backup, err)
	}
	return path, backup, nil
}

func DefaultStore() (Store, error) {
	root, err := os.UserConfigDir()
	if err != nil {
		return Store{}, fmt.Errorf("find computer config: %w", err)
	}
	return Store{Root: filepath.Join(root, "unmute")}, nil
}

func ValidateName(name string) error {
	if name == "" {
		return fmt.Errorf("manifest name is required")
	}
	for _, r := range name {
		allowed := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_'
		if !allowed {
			return fmt.Errorf("manifest name %q must contain only letters, digits, hyphens or underscores", name)
		}
	}
	return nil
}

func (s Store) Load(name string) (*Saved, error) {
	if err := ValidateName(name); err != nil {
		return nil, err
	}
	path := filepath.Join(s.Root, "manifests", name, "manifest")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read manifest %s: %w", name, err)
	}
	rules, err := spec.ParseManifest(data)
	if err != nil {
		return nil, fmt.Errorf("manifest %s: %w", name, err)
	}
	return &Saved{Name: name, Path: path, Data: data, Rules: rules}, nil
}

func (s Store) Default() (*Saved, error) {
	data, err := os.ReadFile(filepath.Join(s.Root, "default-manifest"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read default manifest: %w", err)
	}
	saved, err := s.Load(strings.TrimSpace(string(data)))
	if err != nil {
		return nil, fmt.Errorf("configured default: %w", err)
	}
	return saved, nil
}

func (s Store) List() ([]Saved, error) {
	entries, err := os.ReadDir(filepath.Join(s.Root, "manifests"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("list manifests: %w", err)
	}
	var list []Saved
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		saved, err := s.Load(entry.Name())
		if err != nil {
			return nil, err
		}
		list = append(list, *saved)
	}
	return list, nil
}

func (s Store) Create(name string, data []byte) (string, error) {
	if err := ValidateName(name); err != nil {
		return "", err
	}
	if _, err := spec.ParseManifest(data); err != nil {
		return "", fmt.Errorf("manifest %s: %w", name, err)
	}
	parent := filepath.Join(s.Root, "manifests")
	if err := os.MkdirAll(parent, 0700); err != nil {
		return "", fmt.Errorf("create manifest library: %w", err)
	}
	dir := filepath.Join(parent, name)
	if err := os.Mkdir(dir, 0700); err != nil {
		return "", fmt.Errorf("create manifest %s (existing names cannot be overwritten): %w", name, err)
	}
	path := filepath.Join(dir, "manifest")
	if err := atomicWrite(path, data); err != nil {
		_ = os.RemoveAll(dir)
		return "", err
	}
	// A hard link installs the first default without replacing another creator's choice.
	temp, err := os.CreateTemp(s.Root, ".default-*")
	if err != nil {
		return path, fmt.Errorf("create default draft: %w", err)
	}
	defer func() { _ = os.Remove(temp.Name()) }()
	if _, err := temp.WriteString(name + "\n"); err != nil {
		_ = temp.Close()
		return path, fmt.Errorf("write default draft: %w", err)
	}
	if err := temp.Close(); err != nil {
		return path, fmt.Errorf("close default draft: %w", err)
	}
	if err := os.Link(temp.Name(), filepath.Join(s.Root, "default-manifest")); err != nil && !errors.Is(err, os.ErrExist) {
		return path, fmt.Errorf("set first default: %w", err)
	}
	return path, nil
}

func (s Store) Use(name string) error {
	if _, err := s.Load(name); err != nil {
		return err
	}
	return atomicWrite(filepath.Join(s.Root, "default-manifest"), []byte(name+"\n"))
}

func atomicWrite(path string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".manifest-*")
	if err != nil {
		return fmt.Errorf("create draft: %w", err)
	}
	defer func() { _ = os.Remove(f.Name()) }()
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return fmt.Errorf("write draft: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close draft: %w", err)
	}
	if err := os.Rename(f.Name(), path); err != nil {
		return fmt.Errorf("save %s: %w", path, err)
	}
	return nil
}
