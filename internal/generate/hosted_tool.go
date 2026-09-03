package generate

import (
	"sort"
	"strings"

	"github.com/slng-ai/unmute/internal/spec"
)

// Lowering an SLNG-hosted tool. Shared by both code drivers, because the two
// things that make it work are the same on each: the module travels into the
// generated project, and the call goes through the platform's own contract
// rather than through a guess about the file's shape.
//
// A mirrored module is written as tools/<name>.py inside build/<target>/, while
// the package spells it tools/<name>.slng.py. Not an inconsistency: a dot is
// not legal in a Python module name, so the emitted project could not import
// the package's spelling. And the infix's job is to answer "may I edit this
// file", which is a question about a package a person reads rather than about a
// directory the compiler rewrites on every run.

// hostedEntryPoint is SLNG's contract for a hosted code tool: the module
// defines an `Input` model and a `handler` taking one.
//
// Recovered from a real platform response rather than assumed. That is also why
// nothing here parses the module: the platform introspected it, published the
// result as `arg_schema`, and the mirror carries that into ir.Tool.Input.
const hostedEntryPoint = "handler"

// hostedInputModel is the pydantic model the entry point takes. pydantic is
// already in both generated projects, as a hard dependency of livekit-agents
// and of pipecat, so a mirrored module importing it adds nothing to install.
const hostedInputModel = "Input"

// hostedHeaderLiteral renders the fixed headers the platform stores on a
// request tool as a Python dict literal, or "" when it stores none.
//
// Sorted by name, so two compiles of one package produce one byte sequence.
func hostedHeaderLiteral(headers []spec.MirrorHeader) string {
	if len(headers) == 0 {
		return ""
	}
	sorted := append([]spec.MirrorHeader(nil), headers...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })
	parts := make([]string, 0, len(sorted))
	for _, header := range sorted {
		parts = append(parts, pyQuote(header.Name)+": "+pyQuote(header.Value))
	}
	return "{" + strings.Join(parts, ", ") + "}"
}
