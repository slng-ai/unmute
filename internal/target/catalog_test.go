package target

import (
	"strings"
	"testing"
)

// TestCatalogInvariants holds every entry to the structural rules that used to
// be scattered guards (driver-pipecat V11's exactly-one-install, the
// import-per-class coupling behind B1, dated verification).
func TestCatalogInvariants(t *testing.T) {
	seen := map[string]bool{}
	for _, e := range DefaultCatalog().Entries() {
		id := string(e.Framework) + "/" + string(e.Role) + "/" + e.Vendor
		if seen[id] {
			t.Errorf("%s: duplicate entry", id)
		}
		seen[id] = true

		if e.Install.Extra != "" && e.Install.Package != "" {
			t.Errorf("%s: Install must set at most one of Extra or Package", id)
		}
		if e.Install.Constraint != "" && e.Install.Package == "" {
			t.Errorf("%s: Constraint without Package", id)
		}
		if e.Verified == "" || e.Docs == "" {
			t.Errorf("%s: entries carry a Verified date and a Docs URL", id)
		}
		if e.RequiresEndpoint && !e.Wildcard() {
			t.Errorf("%s: RequiresEndpoint is for wildcard rows only", id)
		}
		if e.Call == nil {
			continue // managed-target matrix row
		}
		if e.Call.Class == "" {
			t.Errorf("%s: Call without a Class", id)
		}
		// The import line must actually provide the class it constructs:
		// "deepgram.STT" needs "deepgram" imported; "SlngSTTService" itself.
		if e.Import != "" {
			token := e.Call.Class
			if i := strings.Index(token, "."); i > 0 {
				token = token[:i]
			}
			if !strings.Contains(e.Import, token) {
				t.Errorf("%s: import %q does not provide class %q", id, e.Import, e.Call.Class)
			}
		}
		if e.RequiresEndpoint && e.Call.Endpoint.Arg == "" {
			t.Errorf("%s: RequiresEndpoint without an Endpoint slot", id)
		}
		if e.Role == Listen || e.Role == Speak {
			hasSlot := e.Call.Language.Arg != ""
			if hasSlot == e.Call.NoLanguage {
				t.Errorf("%s: speech integration needs exactly one of a language slot or an explicit NoLanguage declaration", id)
			}
		}
		if (e.Call.SettingsArg != "" || e.Call.SettingsClass != "") && e.Call.Params != ParamsSettings {
			t.Errorf("%s: SettingsArg/SettingsClass are meaningful only with ParamsSettings", id)
		}
	}
}

// TestSlngEverywhere is the hard requirement as a test: every code target the
// catalogue serves must carry slng entries for its open listen and speak
// roles. Dropping SLNG support on any framework is a red build.
func TestSlngEverywhere(t *testing.T) {
	cat := DefaultCatalog()
	for _, fw := range []Provider{LiveKit, Pipecat} {
		for _, role := range []Role{Listen, Speak} {
			entry, ok := cat.Lookup(fw, role, "slng")
			if !ok || entry.Wildcard() {
				t.Errorf("%s %s: no slng entry", fw, role)
			}
		}
	}
}

// TestCheckVendor pins the one vendor/endpoint rulebook shared by validation
// and driver resolution: aliases, managed allowlists, unrestricted roles,
// wildcard endpoint gating, and endpoint slotting.
func TestCheckVendor(t *testing.T) {
	cat := DefaultCatalog()
	for _, tc := range []struct {
		fw       Provider
		role     Role
		vendor   string
		endpoint bool
		wantErr  string // "" = must pass; otherwise a substring of the error
	}{
		{ElevenLabs, Speak, "elevenlabs", false, ""},
		{ElevenLabs, Speak, "eleven_labs", false, ""}, // provider-spelling alias
		{ElevenLabs, Speak, "cartesia", false, "has no slot"},
		{Deepgram, Listen, "deepgram", false, ""},
		{Deepgram, Listen, "openai", false, "has no slot"},
		{Deepgram, Speak, "aws_polly", false, ""},
		{Deepgram, Speak, "open_ai", false, ""}, // Deepgram's own spelling
		{Vapi, Speak, "anything", false, ""},    // no rows = unrestricted (D10)
		{Pipecat, Listen, "", false, ""},        // empty defers to the target default
		{Pipecat, Listen, "acme", false, "endpoint_env"},
		{Pipecat, Listen, "acme", true, ""}, // OpenAI-compatible custom endpoint
		{Pipecat, Speak, "slng", true, "endpoint_env has no slot"},
		{LiveKit, Listen, "acme", false, "listen providers on livekit: assemblyai, cartesia, deepgram, elevenlabs, gradium, sarvam, slng, soniox, speechmatics"},
	} {
		err := cat.CheckVendor(tc.fw, tc.role, tc.vendor, tc.endpoint)
		switch {
		case tc.wantErr == "" && err != nil:
			t.Errorf("%s %s %q endpoint=%v: unexpected error %v", tc.fw, tc.role, tc.vendor, tc.endpoint, err)
		case tc.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tc.wantErr)):
			t.Errorf("%s %s %q endpoint=%v: want error containing %q, got %v", tc.fw, tc.role, tc.vendor, tc.endpoint, tc.wantErr, err)
		}
	}
}

func TestCatalogLookup(t *testing.T) {
	cat := DefaultCatalog()

	if e, ok := cat.Lookup(Pipecat, Speak, "eleven_labs"); !ok || e.Vendor != "elevenlabs" {
		t.Errorf("alias eleven_labs: got %v %v", e.Vendor, ok)
	}
	if e, ok := cat.Lookup(Pipecat, Listen, "acme"); !ok || !e.Wildcard() || !e.RequiresEndpoint {
		t.Errorf("unknown pipecat listen vendor should hit the endpoint-gated wildcard, got %v %v", e.Vendor, ok)
	}
	if _, ok := cat.Lookup(LiveKit, Listen, "acme"); ok {
		t.Error("livekit listen has no wildcard; unknown vendors must fail")
	}
	if got := cat.Vendors(LiveKit, Speak); strings.Join(got, ",") != "cartesia,deepgram,elevenlabs,gemini,gradium,inworld,rime,sarvam,slng,soniox" {
		t.Errorf("livekit speak vendors = %v", got)
	}
	if got := cat.RolesFor(Pipecat, "slng"); strings.Join(got, ",") != "listen,speak" {
		t.Errorf("slng roles on pipecat = %v", got)
	}
	if e, ok := cat.Lookup(ElevenLabs, Speak, "elevenlabs"); !ok || !e.VoiceRequired() {
		t.Errorf("ElevenLabs speak arity = %#v", e)
	}
}

func TestV24ProviderBrandsAreUniqueAndExposeDistributors(t *testing.T) {
	cat := DefaultCatalog()
	if got := strings.Join(cat.Brands(Pipecat, Speak), ","); got != "cartesia,deepgram,elevenlabs,gradium,inworld,openai,rime,sarvam,soniox" {
		t.Fatalf("pipecat speak brands = %q", got)
	}
	if got := strings.Join(cat.Distributors(Pipecat, Speak, "cartesia"), ","); got != "cartesia,slng" {
		t.Fatalf("cartesia distributors = %q", got)
	}
}
