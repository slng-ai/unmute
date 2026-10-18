package generate

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/slng-ai/unmute/internal/ir"
	"github.com/slng-ai/unmute/internal/spec"
)

func loadTypedState(t *testing.T) *ir.Agent {
	t.Helper()
	pkg, err := spec.Load(filepath.Join("..", "testdata", "typed_state"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := buildWithState(t, pkg)
	if err != nil {
		t.Fatal(err)
	}
	return agent
}

// TestCallStateMembersMatchTheTemplate holds ir.CallStateMembers, the names a
// State field may not take, to the methods the template really defines. A
// method the list does not know is a field the compiler would let overwrite it.
func TestCallStateMembersMatchTheTemplate(t *testing.T) {
	want := slices.Clone(ir.CallStateMembers)
	slices.Sort(want)
	if got := CallStateMethods(); !slices.Equal(got, want) {
		t.Errorf("CallState defines %v, but ir.CallStateMembers lists %v", got, want)
	}
}
