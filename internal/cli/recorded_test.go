package cli

import (
	"context"
	"testing"

	"github.com/slng-ai/unmute/internal/stateschema"
)

// recordedCtx is the context a test hands a command so a package with a
// state.py is read from the recordings in the repo instead of by running uv.
func recordedCtx(t testing.TB) context.Context {
	t.Helper()
	return stateschema.WithReader(t.Context(), stateschema.RecordedInRepo())
}
