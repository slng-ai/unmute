package target

import (
	"maps"
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
		// A generation-param slot must not reuse a kwarg the entry already
		// fills from a typed field, or one call would emit it twice.
		taken := map[string]string{}
		for _, field := range []struct {
			name string
			spec FieldSpec
		}{
			{"model", e.Call.Model}, {"voice", e.Call.Voice},
			{"language", e.Call.Language}, {"endpoint", e.Call.Endpoint},
		} {
			if field.spec.Arg != "" {
				taken[field.spec.Arg] = field.name
			}
		}
		for _, name := range GenerationParams {
			slot, _ := e.Call.paramSlot(name)
			if slot.Arg == "" {
				continue
			}
			if owner, clash := taken[slot.Arg]; clash {
				t.Errorf("%s: %s lowers to %q, already the %s kwarg", id, name, slot.Arg, owner)
			}
			taken[slot.Arg] = name
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

// TestLowerParams pins the generation-param half of the shared rulebook (N20).
// The provenance split is the load-bearing part: a typed field lowers through
// the entry's own kwarg and fails without a slot, while the author's own params
// key is forwarded verbatim even when it shares one of our names, because that
// is the exception D2 carves out.
func TestLowerParams(t *testing.T) {
	cat := DefaultCatalog()
	// typed mimics Build's foldParams: the value lands in params and its
	// provenance in generation. author leaves generation empty.
	typed := func(kv map[string]any) (map[string]any, map[string]any) {
		return maps.Clone(kv), maps.Clone(kv)
	}
	author := func(kv map[string]any) (map[string]any, map[string]any) {
		return maps.Clone(kv), nil
	}
	for _, tc := range []struct {
		name    string
		fw      Provider
		role    Role
		vendor  string
		in      func(map[string]any) (map[string]any, map[string]any)
		params  map[string]any
		want    map[string]any // checked when wantErr is ""
		wantErr string         // substring of the expected error
	}{
		// --- typed fields: slotted, renamed, or refused --------------------
		{name: "typed temperature has a slot", fw: Pipecat, role: Reason, vendor: "openai", in: typed,
			params: map[string]any{"temperature": 0.5}, want: map[string]any{"temperature": 0.5}},
		{name: "typed speed renames to the pipecat spelling", fw: Pipecat, role: Speak, vendor: "rime", in: typed,
			params: map[string]any{"speed": 1.2}, want: map[string]any{"speedAlpha": 1.2}},
		{name: "typed speed renames to the livekit spelling", fw: LiveKit, role: Speak, vendor: "rime", in: typed,
			params: map[string]any{"speed": 1.2}, want: map[string]any{"speed_alpha": 1.2}},
		{name: "anthropic takes top_k", fw: LiveKit, role: Reason, vendor: "anthropic", in: typed,
			params: map[string]any{"top_k": 40}, want: map[string]any{"top_k": 40}},
		{name: "typed speed with no slot", fw: Pipecat, role: Speak, vendor: "cartesia", in: typed,
			params: map[string]any{"speed": 1.1}, wantErr: `pipecat speak binding provider "cartesia": speed has no slot here`},
		{name: "typed top_p with no slot", fw: LiveKit, role: Reason, vendor: "anthropic", in: typed,
			params: map[string]any{"top_p": 0.9}, wantErr: `livekit reason binding provider "anthropic": top_p has no slot here`},
		{name: "typed top_k with no slot", fw: LiveKit, role: Reason, vendor: "openai", in: typed,
			params: map[string]any{"top_k": 40}, wantErr: `livekit reason binding provider "openai": top_k has no slot here`},
		{name: "every bad param is reported, not just the first", fw: LiveKit, role: Reason, vendor: "anthropic", in: typed,
			params: map[string]any{"top_p": 0.9, "temperature": 0.5}, wantErr: `top_p has no slot here`},

		// --- author's params: the D2 escape hatch, never checked ------------
		{name: "author key forwards verbatim", fw: Pipecat, role: Speak, vendor: "slng", in: author,
			params: map[string]any{"sample_rate": 24000}, want: map[string]any{"sample_rate": 24000}},
		{name: "author speed is not renamed", fw: Pipecat, role: Speak, vendor: "rime", in: author,
			params: map[string]any{"speed": 0.9}, want: map[string]any{"speed": 0.9}},
		{name: "author temperature on a slotless entry still passes", fw: Pipecat, role: Listen, vendor: "openai", in: author,
			params: map[string]any{"temperature": 0.2}, want: map[string]any{"temperature": 0.2}},
		{name: "author key on a slotless speak entry still passes", fw: Pipecat, role: Speak, vendor: "cartesia", in: author,
			params: map[string]any{"speed": 1.1}, want: map[string]any{"speed": 1.1}},

		// --- collisions and leniency ---------------------------------------
		{name: "two spellings of one knob is an error", fw: Pipecat, role: Speak, vendor: "sarvam", in: typed,
			params:  map[string]any{"speed": 1.9, "pace": 0.5},
			wantErr: `speed lowers to pace here, which params already sets`},
		{name: "empty vendor normalises to openai", fw: Pipecat, role: Reason, vendor: "", in: typed,
			params: map[string]any{"temperature": 0.5}, want: map[string]any{"temperature": 0.5}},
		{name: "empty vendor gates like openai does", fw: LiveKit, role: Reason, vendor: "", in: typed,
			params: map[string]any{"top_k": 40}, wantErr: `top_k has no slot here`},
		{name: "uncatalogued role stays lenient", fw: Vapi, role: Speak, vendor: "anything", in: typed,
			params: map[string]any{"speed": 2.0}, want: map[string]any{"speed": 2.0}},
		{name: "call-less row stays lenient", fw: Deepgram, role: Speak, vendor: "deepgram", in: typed,
			params: map[string]any{"speed": 2.0}, want: map[string]any{"speed": 2.0}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			params, generation := tc.in(tc.params)
			got, err := cat.LowerParams(tc.fw, tc.role, "", tc.vendor, params, generation)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("want error containing %q, got %v", tc.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error %v", err)
			}
			if !maps.Equal(got, tc.want) {
				t.Errorf("lowered = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestLowerParamsLabelsTheBinding pins the label, which is what stops three
// broken profiles on one vendor collapsing into a single deduped error line.
func TestLowerParamsLabelsTheBinding(t *testing.T) {
	cat := DefaultCatalog()
	params := map[string]any{"speed": 1.1}
	_, err := cat.LowerParams(Pipecat, Speak, "speak.host", "cartesia", params, params)
	if err == nil || !strings.Contains(err.Error(), `pipecat speak.host binding provider "cartesia"`) {
		t.Fatalf("want the label in the error, got %v", err)
	}
}

// TestParamSlotsNilCall guards the exported accessor against the call-less rows,
// which are the majority of the Deepgram catalogue.
func TestParamSlotsNilCall(t *testing.T) {
	entry, ok := DefaultCatalog().Lookup(Deepgram, Speak, "deepgram")
	if !ok || entry.Call != nil {
		t.Fatalf("want a call-less deepgram row, got ok=%v call=%v", ok, entry.Call)
	}
	if slots := entry.Call.ParamSlots(); len(slots) != 0 {
		t.Errorf("ParamSlots on a call-less row = %v, want empty", slots)
	}
}

// TestGenerationSlotSpellingsDifferPerFramework pins the spellings that look
// like typos and are not. Without this, "fixing" speedAlpha to speed_alpha (or
// the reverse) passes every other test and fails on the first live call.
func TestGenerationSlotSpellingsDifferPerFramework(t *testing.T) {
	cat := DefaultCatalog()
	for _, tc := range []struct{ fw, vendor, want string }{
		{"pipecat", "rime", "speedAlpha"}, // Rime's own camelCase, in Settings
		{"livekit", "rime", "speed_alpha"},
		{"pipecat", "sarvam", "pace"},
		{"livekit", "sarvam", "pace"},
		{"pipecat", "inworld", "speaking_rate"},
		{"livekit", "inworld", "speaking_rate"},
	} {
		entry, ok := cat.Lookup(Provider(tc.fw), Speak, tc.vendor)
		if !ok || entry.Call == nil {
			t.Fatalf("%s/%s: no entry", tc.fw, tc.vendor)
		}
		if got := entry.Call.Speed.Arg; got != tc.want {
			t.Errorf("%s/%s speed slot = %q, want %q", tc.fw, tc.vendor, got, tc.want)
		}
	}
}

// TestLowerParamsCoversEveryGenerationParam keeps GenerationParams honest: each
// name must resolve to a slot lookup, so adding a fifth typed field to
// ir.foldParams without a CallSpec field fails here rather than forwarding
// unchecked.
func TestLowerParamsCoversEveryGenerationParam(t *testing.T) {
	spec := &CallSpec{}
	for _, name := range GenerationParams {
		if _, owned := spec.paramSlot(name); !owned {
			t.Errorf("GenerationParams lists %q but CallSpec has no field for it", name)
		}
	}
	if _, owned := spec.paramSlot("sample_rate"); owned {
		t.Error("an author-supplied key must not resolve to a generation slot")
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
	if e, ok := cat.Lookup(LiveKit, Speak, "elevenlabs"); !ok || !e.VoiceRequired() {
		t.Errorf("livekit elevenlabs speak arity = %#v", e)
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
