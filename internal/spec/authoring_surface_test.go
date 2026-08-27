package spec

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// FR-030 locks the authoring surface this feature deliberately did not grow.
//
// Pipecat Cloud telephony added a route, not a field. A hosting-model selector
// was proposed and rejected: there is one hosting model per driver, so an author
// has nothing to choose. The route itself uses existing connection fields.
//
// If you are here because this test failed, you are adding authoring surface.
// That is allowed, but the constitution prices it: a numbered dated SCHEMA
// amendment, the derived schemas, a capability row, the agreement tests, the
// scaffold templates, the interactive console, the examples, and docs-site/, all
// in one commit. Delete a line here only alongside that work.

func TestAuthoringSchemaHasNoHostingModelField(t *testing.T) {
	schema, err := Schema()
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(schema)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{
		"hosting", "hosting_model", "deployment_model", "managed", "cloud", "self_hosted",
	} {
		if found := searchSchema(decoded, name); found != nil {
			t.Errorf("derived authoring schema grew a %q property: %v", name, found)
		}
	}
}

// The carrier-backed Daily route uses the existing connection fields. The
// derived authoring schema must not grow a route-specific property (FR-001).
func TestDailyCarrierRouteNeedsNoNewAuthoringField(t *testing.T) {
	write := func(t *testing.T, files map[string]string) *Package {
		t.Helper()
		dir := t.TempDir()
		for name, content := range files {
			path := filepath.Join(dir, name)
			if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		pkg, err := Load(dir)
		if err != nil {
			t.Fatalf("a Daily target must load: %v", err)
		}
		return pkg
	}

	carrier := write(t, map[string]string{
		"instructions.md": "Help the caller.\n",
		"agent.yaml": "version: 1\nentry_agent: intake\nagents:\n  intake:\n    instructions: instructions.md\n" +
			"channels:\n  phone:\n    kind: telephony\n    inbound: true\n" +
			"capacity:\n  peak_sessions: 2\n  max_sessions: 4\n  peak_starts_per_second: 1\n  avg_session_duration: 3m\n",
		"connections/twilio_sip_daily.yaml": "transport: daily-sip\ncarrier: twilio\nenvironment:\n" +
			"  account_sid: TWILIO_ACCOUNT_SID\n  auth_token: TWILIO_AUTH_TOKEN\n" +
			"  sip_address: SIP_TRUNK_HOSTNAME\n  from_number: SIP_FROM_NUMBER\n",
		"targets.yaml": "targets:\n  pipecat:\n    provider: pipecat\n    version: \"1.8.0\"\n" +
			"    connection: twilio_sip_daily\n",
	})
	built := carrier.Targets["pipecat"]
	if built.Connection != "twilio_sip_daily" {
		t.Fatalf("carrier target = %#v, want its connection", built)
	}
	connection := carrier.Connections["twilio_sip_daily"]
	if connection.Transport != "daily-sip" || connection.Carrier != "twilio" {
		t.Fatalf("carrier connection = %#v, want daily-sip with twilio", connection)
	}
	if _, ok := carrier.Agent.Channels["phone"]; !ok {
		t.Error("carrier form declares no phone channel, so it is not the carrier form")
	}

	// FR-001: no new authoring field for this route.
	schema, err := Schema()
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(schema)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{
		"helper", "helper_url", "sip_provider", "interconnect", "termination", "hold_audio", "room_geo",
	} {
		if found := searchSchema(decoded, name); found != nil {
			t.Errorf("derived authoring schema grew a %q property: %v", name, found)
		}
	}
}

// The Pipecat Cloud native carrier WebSocket route (SCHEMA N38) is one more
// value in `transport`, and that is the whole authoring change. Two shapes load:
// a pure-inbound package with **no connection at all**, because the platform
// receives the call without credentials, and the full shape with a connection for
// packages that place or redirect calls. Neither grows a property, which is what
// makes this a route rather than a feature (spec FR-002).
func TestCloudWebsocketRouteNeedsNoNewAuthoringField(t *testing.T) {
	write := func(t *testing.T, files map[string]string) *Package {
		t.Helper()
		dir := t.TempDir()
		for name, content := range files {
			path := filepath.Join(dir, name)
			if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		pkg, err := Load(dir)
		if err != nil {
			t.Fatalf("a cloud-websocket target must load: %v", err)
		}
		return pkg
	}
	channels := "channels:\n  phone:\n    kind: telephony\n    inbound: true\n" +
		"capacity:\n  peak_sessions: 2\n  max_sessions: 4\n  peak_starts_per_second: 1\n  avg_session_duration: 3m\n"

	inbound := write(t, map[string]string{
		"instructions.md": "Help the caller.\n",
		"agent.yaml": "version: 1\nentry_agent: intake\nagents:\n  intake:\n    instructions: instructions.md\n" +
			channels,
		"targets.yaml": "targets:\n  pipecat:\n    provider: pipecat\n    version: \"1.8.0\"\n" +
			"    transport: cloud-websocket\n    carrier: twilio\n",
	})
	got := inbound.Targets["pipecat"]
	if got.Transport != "cloud-websocket" || got.Carrier != "twilio" {
		t.Fatalf("pure-inbound form = %#v, want transport and carrier only", got)
	}
	if got.Connection != "" {
		t.Errorf("connection = %q, want empty: receiving a call on this route needs no credentials", got.Connection)
	}

	full := write(t, map[string]string{
		"instructions.md": "Help the caller.\n",
		"agent.yaml": "version: 1\nentry_agent: intake\nagents:\n  intake:\n    instructions: instructions.md\n" +
			channels,
		"connections/twilio_voice.yaml": "kind: telephony\nenvironment:\n" +
			"  account_sid: TWILIO_ACCOUNT_SID\n  auth_token: TWILIO_AUTH_TOKEN\n" +
			"  from_number: TWILIO_PHONE_NUMBER\n",
		"targets.yaml": "targets:\n  pipecat:\n    provider: pipecat\n    version: \"1.8.0\"\n" +
			"    transport: cloud-websocket\n    carrier: twilio\n    connection: twilio_voice\n",
	})
	if built := full.Targets["pipecat"]; built.Connection != "twilio_voice" {
		t.Fatalf("connection form = %#v, want the existing connection field set", built)
	}

	schema, err := Schema()
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(schema)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	// Everything this route was tempted to ask for and does not: the platform
	// endpoint, the organization, the auth mode, the markup itself.
	for _, name := range []string{
		"organization", "websocket_auth", "service_host", "stream_url", "twiml", "bin", "websocket_url",
	} {
		if found := searchSchema(decoded, name); found != nil {
			t.Errorf("derived authoring schema grew a %q property: %v", name, found)
		}
	}
}

// The router think binding is authoring surface this feature does grow, and the
// price is paid: the two fields derive from the Go structs rather than from a
// hand-authored schema file, which is what keeps internal/spec the one owner of
// what an author may write.
func TestAuthoringSchemaCarriesTheRouterFields(t *testing.T) {
	schema, err := Schema()
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(schema)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	// agent_id scopes the router cache; upstream says who serves the model. Both
	// hang off a models entry, so both have to reach the derived schema or an
	// author's editor reports a valid package as invalid.
	for _, name := range []string{"agent_id", "upstream"} {
		if found := searchSchema(decoded, name); found == nil {
			t.Errorf("derived authoring schema is missing the %q property", name)
		}
	}
	// The block is typed, not a free map: every field an upstream provider can
	// take is a named property, so a typo is a schema error in the editor rather
	// than a 400 on the first turn of a call.
	for _, name := range []string{
		"provider", "url", "key_env", "auth_header", "deployment", "api_version",
		"credentials_env", "location", "project", "access_key_id_env",
		"secret_access_key_env", "session_token_env", "region", "model_id",
	} {
		if found := searchSchema(decoded, name); found == nil {
			t.Errorf("derived authoring schema is missing the upstream %q property", name)
		}
	}
}
