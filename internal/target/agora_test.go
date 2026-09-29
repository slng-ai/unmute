package target

import "testing"

func TestAgoraCapabilitiesRemainClosed(t *testing.T) {
	if IsCode(Agora) || !EmitsProject(Agora) {
		t.Fatal("Agora emits a service project, not a local pipeline")
	}
	if field()[Agora].Tag != "" {
		t.Fatal("Agora must never inherit Core from field defaults")
	}
	table := Default()
	for field, providers := range table.Fields {
		want := Gated
		if field == FieldMaxDuration {
			want = Core
		}
		if providers[Agora].Tag != want {
			t.Errorf("%s: got %s, want %s", field, providers[Agora].Tag, want)
		}
	}
	for _, role := range []Role{Listen, Reason, Speak} {
		if len(DefaultCatalog().Vendors(Agora, role)) == 0 {
			t.Errorf("%s has no vendor allowlist", role)
		}
	}
}
