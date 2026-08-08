package target

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"sort"
	"strings"
)

// The provider catalogue: the universal map of (framework × role × vendor) to
// the facts codegen needs — install path, import, constructor shape. Entries
// describe integrations that already exist upstream in the framework; Unmute
// never hosts integration code (ADR-0002). Model/voice identities and params
// are forwarded verbatim and never enumerated here (SCHEMA.md D10): an entry
// is a code slot, not a model allowlist.
//
// Entries live one file per framework (catalog_<framework>.go), because an
// entry's contract is with that driver's templates and pinned version range.

// InstallSpec says how the integration is installed: exactly one of Extra
// (an extra on the framework's own pin, e.g. pipecat-ai[deepgram] or
// livekit-agents[deepgram]) or Package (a standalone pip package with its own
// version floor). Both empty means the integration ships with the framework.
type InstallSpec struct {
	Extra      string
	Package    string
	Constraint string // version floor for Package; user pins: override it
}

// ModelForm names the transform from the binding's model string to the
// constructor's model argument. Each form is a few lines of driver-shared Go;
// new forms are added only with a real second user.
type ModelForm string

const (
	FormVerbatim           ModelForm = ""                     // forwarded as written
	FormProviderSlashModel ModelForm = "provider_slash_model" // join provider + "/" + model (LiveKit Inference); provider "livekit" passes verbatim (V19)
)

// ParamsStyle says where the binding's model/voice/params land in the call.
type ParamsStyle string

const (
	// ParamsKwargs: flat constructor kwargs (the SLNG plugins, LiveKit plugins).
	ParamsKwargs ParamsStyle = ""
	// ParamsSettings: model/voice/params nest in Class.Settings(...) while
	// api_key/base_url stay flat (Pipecat official services since v0.0.105
	// deprecated the flat forms; verified against the service docs 2026-07-15).
	ParamsSettings ParamsStyle = "settings"
	// ParamsExtraKwargs: params ride one extra_kwargs dict (LiveKit Inference).
	ParamsExtraKwargs ParamsStyle = "extra_kwargs"
)

// FieldSpec maps one binding field onto a constructor argument. A zero
// FieldSpec means the binding field has no slot on this entry and a non-empty
// value is rejected (a structural fact, not a judgment; SCHEMA.md 6.2 rule 5).
type FieldSpec struct {
	Arg      string
	Required bool
	Form     ModelForm // meaningful on Model only
}

// CallSpec is the constructor shape. Nil on call-less rows: a row whose driver
// injects no code, so the provider name is forwarded into an API body instead.
// That is the whole Deepgram catalogue today, not a managed target — Vapi is
// the only managed one (N17) and it has no catalogue file at all.
type CallSpec struct {
	Class     string
	APIKeyArg string   // "" = the constructor takes no key argument
	APIKeyEnv string   // "" = the <VENDOR>_API_KEY convention (wildcard rows)
	ExtraEnvs []string // required env the constructor reads implicitly (AWS SDK creds); no kwarg emitted
	Model     FieldSpec
	Voice     FieldSpec
	Language  FieldSpec // portable agent language; explicit per integration
	// NoLanguage declares the integration has no language slot on purpose:
	// language rides the model/voice id (deepgram aura) or is auto-detected
	// (soniox). The agent language field is then not forwarded — a per-entry
	// decision, never a silent fallthrough. Exactly one of Language/NoLanguage.
	NoLanguage bool
	Endpoint   FieldSpec // zero = endpoint_env is rejected for this entry
	// Generation params: the four knobs Unmute owns (SCHEMA.md 4.3), folded
	// into a binding's params by ir.Build. An author-supplied params key still
	// forwards verbatim and unvalidated (D10), but these four are Unmute
	// vocabulary, so each entry states whether its class has a slot and what
	// the vendor calls it (rime speed_alpha, sarvam pace). A zero FieldSpec
	// means no slot and a set value is rejected (SCHEMA.md 6.2 rule 5, N20),
	// never emitted as a kwarg the constructor does not accept.
	Temperature FieldSpec
	TopP        FieldSpec
	TopK        FieldSpec
	Speed       FieldSpec
	Params      ParamsStyle
	// SettingsArg/SettingsClass override the nested-args wrapper for
	// ParamsSettings entries. Defaults: "settings" and Class+".Settings"
	// (the Pipecat official-service shape). Soniox on LiveKit nests in
	// params=soniox.STTOptions(...).
	SettingsArg   string
	SettingsClass string
}

// Entry is one (framework, role, vendor) integration.
type Entry struct {
	Framework   Provider
	Role        Role
	Vendor      string   // canonical distributor spelling (SCHEMA.md N8); "*" = wildcard
	Aliases     []string // accepted alternative spellings
	Distributes []string // provider brands routed through this distributor
	Verified    string   // date last checked against upstream docs/source
	Docs        string
	Install     InstallSpec
	Import      string // full import line; "" = covered by the driver's core imports
	Call        *CallSpec
	// Call-less rows have no Call; these fields retain binding arity for UIs and
	// validation. Rows with a Call derive the same facts from it.
	RequireModel bool
	RequireVoice bool
	// RequiresEndpoint gates wildcard rows: an unknown vendor is legal only as
	// a genuinely custom OpenAI-compatible endpoint (endpoint_env set). This is
	// the structural fix for driver-pipecat B1 (slng silently falling through).
	RequiresEndpoint bool
	Notes            []string
}

func (e Entry) ModelRequired() bool {
	return e.RequireModel || e.Call != nil && e.Call.Model.Required
}

func (e Entry) VoiceRequired() bool {
	return e.RequireVoice || e.Call != nil && e.Call.Voice.Required
}

// Wildcard reports whether this entry is a role's catch-all row.
func (e Entry) Wildcard() bool { return e.Vendor == "*" }

type Catalog struct{ entries []Entry }

// DefaultCatalog assembles the built-in entries. A user overlay
// (providers.yaml) merges add-only on top of this; not implemented yet.
func DefaultCatalog() Catalog {
	var entries []Entry
	entries = append(entries, pipecatCatalog...)
	entries = append(entries, livekitCatalog...)
	entries = append(entries, deepgramCatalog...)
	return Catalog{entries: entries}
}

// Lookup resolves a binding vendor. Exact vendor (or alias) wins; otherwise
// the role's wildcard row, if any. The caller checks RequiresEndpoint.
// Packages lists a framework's standalone install packages and their version
// floors (constraint with the leading ">=" intact), for pin validation.
func (c Catalog) Packages(fw Provider) map[string]string {
	out := map[string]string{}
	for _, e := range c.entries {
		if e.Framework == fw && e.Install.Package != "" {
			out[e.Install.Package] = e.Install.Constraint
		}
	}
	return out
}

func (c Catalog) Lookup(fw Provider, role Role, vendor string) (Entry, bool) {
	var wildcard *Entry
	for i := range c.entries {
		e := &c.entries[i]
		if e.Framework != fw || e.Role != role {
			continue
		}
		if e.Vendor == vendor || slices.Contains(e.Aliases, vendor) {
			return *e, true
		}
		if e.Wildcard() {
			wildcard = e
		}
	}
	if wildcard != nil {
		return *wildcard, true
	}
	return Entry{}, false
}

// Vendors lists the canonical vendors for a role (wildcards excluded), sorted.
func (c Catalog) Vendors(fw Provider, role Role) []string {
	var out []string
	for _, e := range c.entries {
		if e.Framework == fw && e.Role == role && !e.Wildcard() {
			out = append(out, e.Vendor)
		}
	}
	sort.Strings(out)
	return out
}

// Brands lists provider brands once, regardless of how many distributors
// expose them. Entries without an explicit route distribute themselves.
func (c Catalog) Brands(fw Provider, role Role) []string {
	var out []string
	for _, e := range c.entries {
		if e.Framework != fw || e.Role != role || e.Wildcard() {
			continue
		}
		if len(e.Distributes) == 0 {
			out = append(out, e.Vendor)
		} else {
			out = append(out, e.Distributes...)
		}
	}
	sort.Strings(out)
	return slices.Compact(out)
}

// Distributors lists the integrations that can deliver a provider brand.
func (c Catalog) Distributors(fw Provider, role Role, brand string) []string {
	var out []string
	for _, e := range c.entries {
		if e.Framework != fw || e.Role != role || e.Wildcard() {
			continue
		}
		if (e.Vendor == brand && len(e.Distributes) == 0) || slices.Contains(e.Distributes, brand) {
			out = append(out, e.Vendor)
		}
	}
	sort.Strings(out)
	return slices.Compact(out)
}

// Brand resolves a stored distributor/model binding to its user-facing brand.
func (c Catalog) Brand(fw Provider, role Role, distributor, model string) string {
	entry, ok := c.Lookup(fw, role, distributor)
	if !ok || entry.Wildcard() {
		return distributor
	}
	if len(entry.Distributes) == 0 {
		return entry.Vendor
	}
	parts := strings.Split(model, "/")
	for _, part := range parts {
		if slices.Contains(entry.Distributes, part) {
			return part
		}
	}
	return entry.Distributes[0]
}

// RolesFor lists the roles a vendor serves on a framework, sorted. Used for
// "slng provides listen and speak here" diagnostics.
func (c Catalog) RolesFor(fw Provider, vendor string) []string {
	var out []string
	for _, e := range c.entries {
		if e.Framework == fw && !e.Wildcard() &&
			(e.Vendor == vendor || slices.Contains(e.Aliases, vendor)) {
			out = append(out, string(e.Role))
		}
	}
	sort.Strings(out)
	return out
}

// Entries returns the full entry list (for invariant tests and listings).
func (c Catalog) Entries() []Entry { return c.entries }

// CheckVendor validates a binding's vendor against the matrix. This is the
// one rulebook for vendor/endpoint slotting, shared by ir.Validate and the
// drivers' resolution, so validate-green cannot disagree with generate.
// Lenient by design: an empty vendor defers to the target's default, and a
// (framework, role) with no entries at all is unrestricted (a role with no
// call-bearing row forwards any provider name; SCHEMA.md D10).
func (c Catalog) CheckVendor(fw Provider, role Role, vendor string, hasEndpoint bool) error {
	if vendor == "" {
		return nil
	}
	if len(c.Vendors(fw, role)) == 0 && !c.hasWildcard(fw, role) {
		return nil
	}
	entry, ok := c.Lookup(fw, role, vendor)
	if !ok {
		return c.noSlot(fw, role, vendor, "")
	}
	if entry.Wildcard() && entry.RequiresEndpoint && !hasEndpoint {
		return c.noSlot(fw, role, vendor, "; an unlisted provider needs endpoint_env (an OpenAI-compatible endpoint)")
	}
	if hasEndpoint && entry.Call != nil && entry.Call.Endpoint.Arg == "" {
		return fmt.Errorf("%s %s binding provider %q: endpoint_env has no slot here; drop it or use an OpenAI-compatible provider", fw, role, vendor)
	}
	return nil
}

func (c Catalog) hasWildcard(fw Provider, role Role) bool {
	_, ok := c.Lookup(fw, role, "*")
	return ok
}

// GenerationParams are the param names Unmute owns: ir.Build folds the typed
// generation fields of a model definition into the binding's params under
// exactly these keys. Every other key is an author-supplied provider setting
// and forwards verbatim (D10). Keep in sync with ir.foldParams.
var GenerationParams = []string{"speed", "temperature", "top_k", "top_p"}

// DefaultVendor resolves an unset binding provider to the schema's
// OpenAI-compatible default spelling. Shared so validation and driver
// resolution normalise a binding identically.
func DefaultVendor(vendor string) string {
	if vendor == "" {
		return "openai"
	}
	return vendor
}

// paramSlot returns the constructor argument this entry uses for one of the
// GenerationParams, and false when the name is not one Unmute owns. Nil-safe:
// call-less rows are the majority of the Deepgram catalogue.
func (s *CallSpec) paramSlot(name string) (FieldSpec, bool) {
	if s == nil {
		return FieldSpec{}, false
	}
	switch name {
	case "speed":
		return s.Speed, true
	case "temperature":
		return s.Temperature, true
	case "top_k":
		return s.TopK, true
	case "top_p":
		return s.TopP, true
	default:
		return FieldSpec{}, false
	}
}

// ParamSlots returns the generation params this entry declares a slot for,
// keyed by Unmute's name and valued by the vendor's kwarg. For listings and the
// catalogue golden; resolution itself goes through LowerParams.
func (s *CallSpec) ParamSlots() map[string]string {
	out := map[string]string{}
	for _, name := range GenerationParams {
		if slot, _ := s.paramSlot(name); slot.Arg != "" {
			out[name] = slot.Arg
		}
	}
	return out
}

// LowerParams is the one rulebook for generation-param slotting, shared by
// ir.Validate and the drivers' resolution, so a binding that validates green
// cannot fail param lowering at generate time.
//
// It splits a binding's params by provenance (ir.Binding.Generation). A key the
// compiler wrote from a typed generation field lowers through the entry's own
// kwarg and fails when the entry has no slot: the value has nowhere to go
// (SCHEMA.md 6.2 rule 5, N20), and emitting it anyway is Python that raises
// TypeError on the first call. A key the author wrote in params is forwarded
// verbatim and never checked, even when it happens to share one of our names —
// that is the single exception D2 carves out, and narrowing it here would take
// away the escape hatch the same error message points at.
//
// label names the binding in errors ("speak.host"); empty means use the role.
//
// Lenient the same way CheckVendor is: a (framework, role) with no entries, or a
// call-less row, forwards everything. See SCHEMA.md N20's scope note for why that
// permanently excludes Vapi and Deepgram, and what covering them would need.
func (c Catalog) LowerParams(fw Provider, role Role, label, vendor string, params, generation map[string]any) (map[string]any, error) {
	if len(params) == 0 {
		return params, nil
	}
	if label == "" {
		label = string(role)
	}
	vendor = DefaultVendor(vendor)
	entry, ok := c.Lookup(fw, role, vendor)
	if !ok || entry.Call == nil {
		return params, nil
	}
	out := make(map[string]any, len(params))
	var problems []string // every bad param, so fixing them is not a rerun loop
	for _, name := range slices.Sorted(maps.Keys(params)) {
		slot, owned := entry.Call.paramSlot(name)
		if _, typed := generation[name]; !owned || !typed {
			out[name] = params[name] // author's own key: forwarded as-is (D2)
			continue
		}
		switch {
		case slot.Arg == "":
			problems = append(problems, fmt.Sprintf(
				"%s has no slot here (drop it, or set the vendor's own key in params, which is forwarded as-is)", name))
		case slot.Arg != name && hasKey(params, slot.Arg):
			// Two spellings of one knob. Unlike params.language shadowing a typed
			// language (N16), the author cannot see this collision — only we know
			// these are the same setting — so it fails rather than picking a winner.
			problems = append(problems, fmt.Sprintf(
				"%s lowers to %s here, which params already sets (keep one)", name, slot.Arg))
		default:
			out[slot.Arg] = params[name]
		}
	}
	if len(problems) > 0 {
		return nil, fmt.Errorf("%s %s binding provider %q: %s", fw, label, vendor, strings.Join(problems, "; "))
	}
	return out, nil
}

func hasKey(params map[string]any, key string) bool {
	_, ok := params[key]
	return ok
}

// noSlot names the gap in the matrix's own vocabulary: which vendors the role
// does take here, and which roles the vendor does serve here.
func (c Catalog) noSlot(fw Provider, role Role, vendor, suffix string) error {
	msg := fmt.Sprintf("%s %s binding provider %q has no slot; %s providers on %s: %s%s",
		fw, role, vendor, role, fw, strings.Join(c.Vendors(fw, role), ", "), suffix)
	if roles := c.RolesFor(fw, vendor); len(roles) > 0 {
		msg += fmt.Sprintf(" (%q provides %s on %s)", vendor, strings.Join(roles, ", "), fw)
	}
	return errors.New(msg)
}
