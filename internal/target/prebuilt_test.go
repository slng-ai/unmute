package target

import "testing"

func TestLookupPrebuiltEndCall(t *testing.T) {
	p, ok := LookupPrebuilt("end_call")
	if !ok {
		t.Fatal("end_call must be a known prebuilt id")
	}
	if p.Effect != "ends_conversation" {
		t.Errorf("end_call effect = %q, want ends_conversation", p.Effect)
	}
	if p.DefaultDescription == "" {
		t.Error("end_call must carry a default description")
	}
}

func TestLookupPrebuiltUnknown(t *testing.T) {
	if _, ok := LookupPrebuilt("teleport"); ok {
		t.Error("unknown prebuilt id must not resolve")
	}
}
