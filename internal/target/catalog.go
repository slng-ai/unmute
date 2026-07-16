package target

import (
	"errors"
	"fmt"
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
	FormSlngRoute          ModelForm = "slng_route"           // strip the slng/ prefix (the plugin takes the bare vendor/model route)
	FormProviderSlashModel ModelForm = "provider_slash_model" // join provider + "/" + model (LiveKit Inference)
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
	APIKeyArg string // "" = the constructor takes no key argument
	APIKeyEnv string // "" = the <VENDOR>_API_KEY convention (wildcard rows)
	Model     FieldSpec
	Voice     FieldSpec
	Language  FieldSpec // portable agent language; explicit per integration
	Endpoint  FieldSpec // zero = endpoint_env is rejected for this entry
	Params    ParamsStyle
}

// Entry is one (framework, role, vendor) integration.
type Entry struct {
	Framework Provider
	Role      Role
	Vendor    string   // canonical binding spelling (SCHEMA.md N8); "*" = wildcard
	Aliases   []string // accepted alternative spellings
	Verified  string   // date last checked against upstream docs/source
	Docs      string
	Install   InstallSpec
	Import    string // full import line; "" = covered by the driver's core imports
	Call      *CallSpec
	// RequiresEndpoint gates wildcard rows: an unknown vendor is legal only as
	// a genuinely custom OpenAI-compatible endpoint (endpoint_env set). This is
	// the structural fix for driver-pipecat B1 (slng silently falling through).
	RequiresEndpoint bool
	Notes            []string
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
	entries = append(entries, elevenlabsCatalog...)
	return Catalog{entries: entries}
}

// Lookup resolves a binding vendor. Exact vendor (or alias) wins; otherwise
// the role's wildcard row, if any. The caller checks RequiresEndpoint.
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
