package scaffold

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/slng-ai/unmute/internal/generate"
	"github.com/slng-ai/unmute/internal/ir"
	"github.com/slng-ai/unmute/internal/spec"
	targetcap "github.com/slng-ai/unmute/internal/target"
)

// PreflightReport is the review data produced by the real compiler pipeline.
type PreflightReport struct {
	TargetName  string
	Warnings    []string
	RequiredEnv []string
	Bindings    []ir.ForwardedBinding
}

// Preflight renders into an isolated temporary directory, then runs the same
// strict load/build/generate path as compile. It never touches the destination.
func Preflight(data Data) (PreflightReport, error) {
	root, err := os.MkdirTemp("", "unmute-init-")
	if err != nil {
		return PreflightReport{}, fmt.Errorf("create temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(root) }() // best effort; the OS also owns its temp directory

	data = data.withDefaults()
	dir := filepath.Join(root, "agent")
	if _, err := Write(dir, data); err != nil {
		return PreflightReport{}, err
	}
	pkg, err := spec.Load(dir)
	if err != nil {
		return PreflightReport{}, err
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		return PreflightReport{}, err
	}
	if len(agent.Targets) != 1 {
		return PreflightReport{}, fmt.Errorf("preflight expected one target, got %d", len(agent.Targets))
	}
	var resolved ir.Target
	for _, resolved = range agent.Targets {
	}
	artifact, err := generate.Generate(agent, resolved, targetcap.Default())
	if err != nil {
		return PreflightReport{}, err
	}
	return PreflightReport{
		TargetName:  resolved.Name,
		Warnings:    artifact.Notes.Warnings,
		RequiredEnv: data.RequiredEnv(),
		Bindings:    artifact.Notes.ForwardedBindings,
	}, nil
}
