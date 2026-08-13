package spec

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// FR-030 locks the authoring surface this feature deliberately did not grow.
//
// Pipecat Cloud telephony added a route, not a field. Two things were proposed
// and rejected: a hosting-model selector (there is one hosting model per driver,
// so an author has nothing to choose) and a phone channel on the Daily route
// (the compiler derives the route facts, and a channel would drag `capacity`
// in with it per SCHEMA §4.10). Both rejections were written down in the spec
// and in contracts/authoring.md, and a fact stated twice with nothing enforcing
// it is the shape Principle III exists to prevent. So they are tested.
//
// If you are here because this test failed, you are adding authoring surface.
// That is allowed, but the constitution prices it: a numbered dated SCHEMA
// amendment, the derived schemas, a capability row, the agreement tests, the
// scaffold templates, the interactive console, the examples, and docs/user/, all
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

// The Daily route has two forms, and both are load-time facts, so both are
// asserted where an author would hit them.
//
// The no-carrier form declares a transport and nothing else: no connection,
// because Daily's own infrastructure delivers the call, and no phone channel,
// because the compiler derives what the route needs from the transport. The
// carrier form declares all three, which SCHEMA N37 made valid (and which N34's
// superseded clause used to reject). What has not changed is the thing this file
// exists to pin: the derived authoring schema grows no property either way,
// which is what makes the carrier leg a combination of existing fields rather
// than a new field (spec FR-001).
func TestDailyRouteFormsNeedNoNewAuthoringField(t *testing.T) {
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

	pkg := write(t, map[string]string{
		"instructions.md": "Help the caller.\n",
		"agent.yaml":      "version: 1\nentry_agent: intake\nagents:\n  intake:\n    instructions: instructions.md\n",
		"targets.yaml": "targets:\n  pipecat:\n    provider: pipecat\n    version: \"1.5.0\"\n" +
			"    transport: daily-sip\n",
	})
	got := pkg.Targets["pipecat"]
	if got.Transport != "daily-sip" {
		t.Fatalf("transport = %q, want daily-sip", got.Transport)
	}
	if got.Connection != "" {
		t.Errorf("connection = %q, want empty: the no-carrier Daily form has no carrier connection", got.Connection)
	}
	if got.Carrier != "" {
		t.Errorf("carrier = %q, want empty", got.Carrier)
	}
	if pkg.Agent.Channels != nil {
		t.Errorf("channels = %+v, want none declared", pkg.Agent.Channels)
	}

	// The carrier form: the same three existing fields, plus the existing phone
	// channel and the existing capacity block a telephony channel already forces.
	carrier := write(t, map[string]string{
		"instructions.md": "Help the caller.\n",
		"agent.yaml": "version: 1\nentry_agent: intake\nagents:\n  intake:\n    instructions: instructions.md\n" +
			"channels:\n  phone:\n    kind: telephony\n    inbound: true\n" +
			"capacity:\n  peak_sessions: 2\n  max_sessions: 4\n  peak_starts_per_second: 1\n  avg_session_duration: 3m\n",
		"connections/twilio_sip_daily.yaml": "kind: telephony\nenvironment:\n" +
			"  account_sid: TWILIO_ACCOUNT_SID\n  auth_token: TWILIO_AUTH_TOKEN\n" +
			"  sip_address: SIP_TRUNK_HOSTNAME\n  from_number: SIP_FROM_NUMBER\n",
		"targets.yaml": "targets:\n  pipecat:\n    provider: pipecat\n    version: \"1.5.0\"\n" +
			"    transport: daily-sip\n    carrier: twilio\n    connection: twilio_sip_daily\n",
	})
	built := carrier.Targets["pipecat"]
	if built.Transport != "daily-sip" || built.Carrier != "twilio" || built.Connection != "twilio_sip_daily" {
		t.Fatalf("carrier form = %#v, want all three existing fields set", built)
	}
	if _, ok := carrier.Agent.Channels["phone"]; !ok {
		t.Error("carrier form declares no phone channel, so it is not the carrier form")
	}

	// FR-001: no new authoring field, on either form.
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
		"targets.yaml": "targets:\n  pipecat:\n    provider: pipecat\n    version: \"1.5.0\"\n" +
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
		"targets.yaml": "targets:\n  pipecat:\n    provider: pipecat\n    version: \"1.5.0\"\n" +
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
