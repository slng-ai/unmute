package target

import (
	"errors"
	"fmt"
	"slices"
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

// CallSpec is the constructor shape. Nil on managed-target rows (they forward
// provider names into API bodies instead of emitting code).
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
	Params     ParamsStyle
	// SettingsArg/SettingsClass override the nested-args wrapper for
	// ParamsSettings entries. Defaults: "settings" and Class+".Settings"
	// (the Pipecat official-service shape). Soniox on LiveKit nests in
	// params=soniox.STTOptions(...).
	SettingsArg   string
	SettingsClass string
	// SettingsOverflow names the Settings field that carries provider-specific
	// params the dataclass has no field of its own for. Pipecat's
	// `ServiceSettings.extra` (services/settings.py:189) is merged verbatim into
	// the request body (services/openai/base_llm.py:361), so a param the class
	// never heard of still reaches the provider instead of raising
	// `TypeError: unexpected keyword argument`. Empty means the entry has no
	// overflow and every param stays a plain Settings field. Set it only where
	// the service is known to merge the field, not merely to declare it.
	SettingsOverflow string
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
	// Managed rows have no Call; these fields retain binding arity for UIs and
	// validation. Code rows derive the same facts from Call.
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
	slices.Sort(out)
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
	slices.Sort(out)
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
	slices.Sort(out)
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
	slices.Sort(out)
	return out
}

// Entries returns the full entry list (for invariant tests and listings).
func (c Catalog) Entries() []Entry { return c.entries }

// CheckVendor validates a binding's vendor against the matrix. This is the
// one rulebook for vendor/endpoint slotting, shared by ir.Validate and the
// drivers' resolution, so validate-green cannot disagree with generate.
// Lenient by design: an empty vendor defers to the target's default, and a
// (framework, role) with no entries at all is unrestricted (managed targets
// forward any provider name; SCHEMA.md D10).
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
