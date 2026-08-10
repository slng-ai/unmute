package generate

import (
	"fmt"

	"github.com/slng/unmute/internal/ir"
)

// webhookAuth is one lowered auth scheme, shared by both code drivers: the
// templates ask it for a Python header expression, so neither template branches
// per scheme (driver-livekit T24 / driver-pipecat T28).
type webhookAuth struct {
	Kind string // bearer | api_key
	// Expr is the Python expression producing the request headers.
	Expr string
}

// loweredAuth builds the template view of a tool's auth, or nil when the tool
// posts unauthenticated.
func loweredAuth(auth *ir.ToolAuth) *webhookAuth {
	if auth == nil {
		return nil
	}
	switch auth.Type {
	case ir.ToolAuthBearer:
		return &webhookAuth{Kind: string(auth.Type), Expr: fmt.Sprintf("_bearer(%s)", pyQuote(auth.TokenEnv))}
	case ir.ToolAuthAPIKey:
		return &webhookAuth{
			Kind: string(auth.Type),
			Expr: fmt.Sprintf("_api_key(%s, %s)", pyQuote(auth.Header), pyQuote(auth.TokenEnv)),
		}
	}
	return nil
}

// authKindSet records which schemes an emitted project actually uses, so each
// helper emits iff used (SCHEMA §5.3: no dead code).
type authKindSet struct {
	Bearer bool
	APIKey bool
}

func (s *authKindSet) add(kind string) {
	switch kind {
	case string(ir.ToolAuthBearer):
		s.Bearer = true
	case string(ir.ToolAuthAPIKey):
		s.APIKey = true
	}
}

// Any reports whether any helper emits at all.
func (s authKindSet) Any() bool { return s.Bearer || s.APIKey }
