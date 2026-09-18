package target

// Prebuilt is a provider-shipped tool a user selects by id with
// `execution: builtin` + `builtin: <id>`, rather than authoring a handler.
// The registry carries only what is portable across drivers: the default
// description and the implied effect. Which providers host it is the
// FieldToolBuiltin capability gate; which params it accepts is the closed set
// of typed Tool fields.
type Prebuilt struct {
	ID                 string
	DefaultDescription string
	Effect             string // "ends_conversation" | "returns_data"
	// Config are the capability's own settings a package may pin, written with
	// `inject:` and lowered to the attachment's config_overrides rather than its
	// argument_overrides. Empty for a capability that takes none.
	//
	// The distinction is the platform's, not a style: a curated capability has
	// no argument schema of its own — `current_datetime` publishes `{}` — so a
	// value pinned on one has nowhere else to go, and the model never sees it
	// either way.
	Config []string
}

// prebuilts is the closed registry. Adding a prebuilt is one row here plus a
// per-driver lowering; no new authoring surface.
var prebuilts = map[string]Prebuilt{
	"end_call": {
		ID:                 "end_call",
		DefaultDescription: "End the call when the caller is finished or says goodbye.",
		Effect:             "ends_conversation",
	},
	// send_sms is SLNG's curated text-message capability. The model supplies the
	// recipient and the body; the package pins the sender with
	// `inject: from_number`, which the slng driver writes as the attachment's
	// config override because the platform requires the sender there
	// (agent_runtime_compiler.py:1489-1492). Twilio credentials are read from
	// the organisation's vault by the platform itself. Only the slng driver
	// lowers it; a code target refuses the id by name.
	"send_sms": {
		ID:                 "send_sms",
		DefaultDescription: "Send a text message to a phone number the caller has given and confirmed.",
		Effect:             "returns_data",
		Config:             []string{"from_number"},
	},
	// current_datetime is SLNG's curated clock. It publishes an empty argument
	// schema and reads its zone from the attachment's config, defaulting to UTC,
	// which is wrong for every agent that is not in it: a package in San
	// Francisco was told the date was already tomorrow from four in the
	// afternoon. The zone cannot be fixed on the tool, because the curated
	// record belongs to no organisation (`organisation_id: null`), so pinning it
	// on the attachment is the only route a package has.
	"current_datetime": {
		ID:                 "current_datetime",
		DefaultDescription: "Read the current date and time.",
		Effect:             "returns_data",
		Config:             []string{"timezone"},
	},
	// user_phone_number is the caller's own number, which the platform knows
	// from the call leg. It takes no settings and no arguments.
	"user_phone_number": {
		ID:                 "user_phone_number",
		DefaultDescription: "Read the number the caller is calling from.",
		Effect:             "returns_data",
	},
}

// SendSmsSenderPattern is the platform's own rule for a send_sms sender: a
// literal E.164 number (shared_tool_contract.py SendSmsOverrides.validate_sender).
// A template token is refused there, so it is refused at validate too.
const SendSmsSenderPattern = `^\+[1-9]\d{1,14}$`

// SendSmsVaultSecrets are the two vault entries SLNG's send_sms reads on its
// own, by these exact names (agent_runtime_compiler.py:1494-1496). A package
// declares neither; the preflight checks both.
var SendSmsVaultSecrets = []string{"TWILIO_ACCOUNT_SID", "TWILIO_AUTH_TOKEN"}

// LookupPrebuilt returns the registry entry for id, or ok=false if unknown.
func LookupPrebuilt(id string) (Prebuilt, bool) {
	p, ok := prebuilts[id]
	return p, ok
}
