package ir

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	packagespec "github.com/slng-ai/unmute/internal/spec"
)

func TestBuildBuiltinToolResolvesRegistryDefaults(t *testing.T) {
	pkg := loadSafeCore(t)
	tool := pkg.Tools["lookup_customer"]
	tool.Webhook, tool.Input, tool.Output, tool.Effect, tool.Description = nil, nil, nil, "", ""
	tool.Builtin = &packagespec.ToolBuiltin{ID: "end_call", Instructions: "Thank the caller and say goodbye."}
	pkg.Tools["lookup_customer"] = tool

	agent, err := Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	got := agent.Tools["lookup_customer"]
	if got.Builtin != "end_call" || got.Instructions != "Thank the caller and say goodbye." {
		t.Fatalf("builtin fields not carried through: %#v", got)
	}
	if got.Effect != ToolEndsConversation {
		t.Errorf("effect = %q, want ends_conversation (from registry)", got.Effect)
	}
	if got.Description == "" {
		t.Error("empty description must be filled from the registry default")
	}
}

func TestBuildSafeCore(t *testing.T) {
	agent, err := Build(loadSafeCore(t))
	if err != nil {
		t.Fatal(err)
	}
	if agent.EntryAgent != "intake" || len(agent.Targets) != 2 {
		t.Fatalf("unexpected IR: entry=%q targets=%d", agent.EntryAgent, len(agent.Targets))
	}
	if !strings.Contains(agent.Agents["intake"].Instructions, "front desk") {
		t.Fatal("prompt path was not composed")
	}
	if _, ok := agent.Controls["to_billing"].(*AgentTransfer); !ok {
		t.Fatalf("control union = %T", agent.Controls["to_billing"])
	}
	if agent.Tools["lookup_customer"].Effect != ToolReturnsData {
		t.Fatal("tool defaults were not applied")
	}
	if got := agent.Connections["primary_phone"].Environment["account_sid"]; got != "TWILIO_ACCOUNT_SID" {
		t.Fatalf("resolved connection account_sid = %q", got)
	}
}

func TestBuildAgentTransferAnnounce(t *testing.T) {
	pkg := loadSafeCore(t)
	want := "I’ll connect you with billing."
	control := pkg.Agent.Controls["to_billing"]
	control.Announce = &want
	pkg.Agent.Controls["to_billing"] = control

	agent, err := Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	if got := agent.Controls["to_billing"].(*AgentTransfer).Announce; got != want {
		t.Fatalf("announce = %q, want %q", got, want)
	}
}

func TestBuildRejectsInvalidAgentTransferAnnounce(t *testing.T) {
	for _, test := range []struct {
		name   string
		value  string
		mutate func(*packagespec.Package, *string)
		want   string
	}{
		{
			name:  "blank",
			value: " \t ",
			mutate: func(pkg *packagespec.Package, value *string) {
				control := pkg.Agent.Controls["to_billing"]
				control.Announce = value
				pkg.Agent.Controls["to_billing"] = control
			},
			want: "announce must not be blank",
		},
		{
			name:  "template",
			value: "I’ll connect you with {{destination}}.",
			mutate: func(pkg *packagespec.Package, value *string) {
				control := pkg.Agent.Controls["to_billing"]
				control.Announce = value
				pkg.Agent.Controls["to_billing"] = control
			},
			want: "announce does not support templates",
		},
		{
			name:  "delegate",
			value: "Please hold.",
			mutate: func(pkg *packagespec.Package, value *string) {
				task := "collect"
				pkg.Agent.Controls["bad"] = packagespec.Control{Kind: "delegate", Task: &task, Announce: value}
			},
			want: `field "announce" is illegal with control kind "delegate"`,
		},
		{
			name:  "human transfer",
			value: "Please hold.",
			mutate: func(pkg *packagespec.Package, value *string) {
				pkg.Agent.Controls["bad"] = packagespec.Control{
					Kind: "human_transfer", Announce: value,
					Cold: &packagespec.ColdTransfer{Destination: "billing_line"},
				}
			},
			want: `field "announce" is illegal with control kind "human_transfer"`,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			pkg := loadSafeCore(t)
			test.mutate(pkg, &test.value)
			_, err := Build(pkg)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("got %v, want %q", err, test.want)
			}
		})
	}
}

func TestBuildResolvesExactTelephonyPlan(t *testing.T) { // telephony V2, V4-V6
	pkg := loadSafeCore(t)
	enableTelephony(pkg)
	routeTarget(pkg, "pipecat", "primary_phone", "cloud-websocket", "twilio")

	agent, err := Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	plan := agent.Targets["pipecat"].Telephony
	if plan == nil {
		t.Fatal("telephony plan was not resolved")
	}
	if plan.Key.Transport != "cloud-websocket" || plan.Key.Carrier != "twilio" || plan.Connection != "primary_phone" {
		t.Fatalf("route = %#v", plan)
	}
	if plan.Coordination != "shared" || plan.AdmissionOwner != "generated_runtime" || len(plan.Evidence) == 0 {
		t.Fatalf("incomplete plan = %#v", plan)
	}
	// The platform terminates the carrier's stream on this route, so the package
	// hosts no process and no endpoint of its own and needs no coordination store.
	// This used to be asserted against carrier-websocket, which hosted all three
	// and no longer exists.
	if got := strings.Join(plan.Services, ","); got != "application" {
		t.Fatalf("services = %s", got)
	}
	if len(plan.Processes) != 0 || len(plan.PublicEndpoints) != 0 || len(plan.ManualSteps) == 0 {
		t.Fatalf("runtime facts = %#v", plan)
	}
	// Nothing is supplied locally: every name this route needs is a carrier
	// credential the author declares.
	if got := strings.Join(plan.LocalEnvironment, ","); got != "" {
		t.Fatalf("locally supplied environment = %s", got)
	}
	if got := strings.Join(plan.RequiredEnvironment, ","); got != "TWILIO_ACCOUNT_SID,TWILIO_AUTH_TOKEN,TWILIO_PHONE_NUMBER" {
		t.Fatalf("required environment = %s", got)
	}
	if got := coordinationReasonNames(plan.CoordinationReasons); got != "admission" {
		t.Fatalf("coordination reasons = %s", got)
	}
	// No auto webhook on this route: in production the number points at a TwiML
	// Bin, which is a console object rather than a URL, so there is nothing for
	// the CLI to write.
	if plan.AutoWebhookEndpoint != "" {
		t.Fatalf("auto webhook = %q", plan.AutoWebhookEndpoint)
	}
}

// An outbound-only channel emits no inbound endpoint, so the auto-webhook
// fact must not survive projection (nothing to point the carrier at).
func TestBuildClearsAutoWebhookWithoutInboundEndpoint(t *testing.T) {
	pkg := loadSafeCore(t)
	inbound, outbound := false, true
	pkg.Agent.Channels["phone"] = packagespec.Channel{
		Kind: "telephony", Inbound: &inbound, Outbound: &outbound, OnVoicemail: "hangup",
	}
	routeTarget(pkg, "pipecat", "primary_phone", "cloud-websocket", "twilio")

	agent, err := Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	plan := agent.Targets["pipecat"].Telephony
	if plan == nil || plan.AutoWebhookEndpoint != "" {
		t.Fatalf("outbound-only plan auto webhook = %#v", plan)
	}
}

func TestBuildLiveKitSIPUsesSharedDispatchPlan(t *testing.T) { // telephony T10, V13, V18
	pkg := loadSafeCore(t)
	enableTelephony(pkg)
	routeTarget(pkg, "livekit", "primary_phone", "sip", "twilio")
	connection := pkg.Connections["primary_phone"]
	connection.Environment = map[string]string{
		"sip_address": "TWILIO_SIP_ADDRESS", "sip_username": "TWILIO_SIP_USERNAME",
		"sip_password": "TWILIO_SIP_PASSWORD", "from_number": "TWILIO_PHONE_NUMBER",
	}
	pkg.Connections["primary_phone"] = connection

	agent, err := Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	plan := agent.Targets["livekit"].Telephony
	if plan == nil || plan.Coordination != "shared" || plan.AdmissionOwner != "livekit_dispatch" {
		t.Fatalf("LiveKit SIP plan = %#v", plan)
	}
	if got := strings.Join(plan.Services, ","); got != "application,livekit_server,livekit_sip,redis" {
		t.Fatalf("LiveKit SIP services = %s", got)
	}
	if len(plan.Processes) != 1 || len(plan.PublicEndpoints) != 0 || len(plan.ManualSteps) == 0 {
		t.Fatalf("LiveKit SIP runtime facts = %#v", plan)
	}
	if got := strings.Join(plan.LocalEnvironment, ","); got != "LIVEKIT_API_KEY,LIVEKIT_API_SECRET,LIVEKIT_URL,REDIS_URL" {
		t.Fatalf("LiveKit SIP locally supplied environment = %s", got)
	}
	// No environment name carries a trunk ID: the emitted telephony-setup.sh
	// resolves the inbound records by phone number (SCHEMA N36).
	if got := strings.Join(plan.RequiredEnvironment, ","); strings.Contains(got, "TRUNK") {
		t.Fatalf("LiveKit SIP required environment still carries a trunk ID: %s", got)
	}
	if plan.AutoWebhookEndpoint != "" {
		t.Fatalf("LiveKit SIP auto webhook = %q", plan.AutoWebhookEndpoint)
	}
	if got := coordinationReasonNames(plan.CoordinationReasons); got != "livekit_control_plane" {
		t.Fatalf("LiveKit SIP coordination reasons = %s", got)
	}
	if got := strings.Join(plan.CoordinationReasons[0].Consumers, ","); got != "livekit_server,livekit_sip" {
		t.Fatalf("LiveKit SIP coordination consumers = %s", got)
	}
}

func TestBuildSupportsMultipleCarrierTargets(t *testing.T) {
	pkg := loadSafeCore(t)
	enableTelephony(pkg)
	pipecat, livekit := pkg.Targets["pipecat"], pkg.Targets["livekit"]
	pkg.Targets = map[string]packagespec.Target{
		"pipecat_twilio": withTelephonyRoute(pipecat, "twilio_voice"),
		"livekit_twilio": withTelephonyRoute(livekit, "twilio_sip"),
		"livekit_telnyx": withTelephonyRoute(livekit, "telnyx_sip"),
		"livekit_plivo":  withTelephonyRoute(livekit, "plivo_sip"),
	}
	// Four connections, four routes. Three of them are the same transport with
	// three different carriers, which is the point: the carrier travels with the
	// transport in the connection file, not on the target. It used to be shown on
	// Pipecat carrier-websocket, which had three carriers and now does not exist;
	// LiveKit sip still has three and makes the same case.
	pkg.Connections = map[string]packagespec.Connection{
		"twilio_voice": {Transport: "cloud-websocket", Carrier: "twilio", Environment: map[string]string{
			"account_sid": "TWILIO_ACCOUNT_SID", "auth_token": "TWILIO_AUTH_TOKEN", "from_number": "TWILIO_PHONE_NUMBER",
		}},
		"telnyx_sip": {Transport: "sip", Carrier: "telnyx", Environment: map[string]string{
			"sip_address": "TELNYX_SIP_ADDRESS", "sip_username": "TELNYX_SIP_USERNAME", "sip_password": "TELNYX_SIP_PASSWORD", "from_number": "TELNYX_PHONE_NUMBER",
		}},
		"twilio_sip": {Transport: "sip", Carrier: "twilio", Environment: map[string]string{
			"sip_address": "TWILIO_SIP_ADDRESS", "sip_username": "TWILIO_SIP_USERNAME", "sip_password": "TWILIO_SIP_PASSWORD", "from_number": "TWILIO_PHONE_NUMBER",
		}},
		"plivo_sip": {Transport: "sip", Carrier: "plivo", Environment: map[string]string{
			"sip_address": "PLIVO_SIP_ADDRESS", "sip_username": "PLIVO_SIP_USERNAME", "sip_password": "PLIVO_SIP_PASSWORD", "from_number": "PLIVO_PHONE_NUMBER",
		}},
	}

	agent, err := Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]string{
		"pipecat_twilio": "pipecat/cloud-websocket/twilio/twilio_voice",
		"livekit_twilio": "livekit/sip/twilio/twilio_sip",
		"livekit_telnyx": "livekit/sip/telnyx/telnyx_sip",
		"livekit_plivo":  "livekit/sip/plivo/plivo_sip",
	} {
		plan := agent.Targets[name].Telephony
		if plan == nil {
			t.Fatalf("%s has no telephony plan", name)
		}
		got := strings.Join([]string{string(plan.Key.Provider), plan.Key.Transport, plan.Key.Carrier, plan.Connection}, "/")
		if got != want {
			t.Errorf("%s route = %s, want %s", name, got, want)
		}
	}
}

func TestBuildRequiresTelephonyConnectionAndRejectsInverse(t *testing.T) {
	t.Run("missing connection", func(t *testing.T) {
		pkg := loadSafeCore(t)
		enableTelephony(pkg)
		target := pkg.Targets["pipecat"]
		target.Connection = ""
		pkg.Targets = map[string]packagespec.Target{"pipecat": target}
		if _, err := Build(pkg); err == nil || !strings.Contains(err.Error(), `has a telephony channel and names no connection`) {
			t.Fatalf("got %v", err)
		}
	})
	// A connection nothing uses is refused, and the message names both ways to
	// use one.
	t.Run("connection nothing uses", func(t *testing.T) {
		pkg := loadSafeCore(t)
		target := pkg.Targets["livekit"]
		target.Connection = "livekit_trunk"
		pkg.Targets = map[string]packagespec.Target{"livekit": target}
		pkg.Connections["livekit_trunk"] = packagespec.Connection{
			Transport: "sip", Carrier: "twilio", Environment: map[string]string{
				"sip_address": "TWILIO_SIP_ADDRESS", "sip_username": "TWILIO_SIP_USERNAME",
				"sip_password": "TWILIO_SIP_PASSWORD", "from_number": "TWILIO_PHONE_NUMBER",
			},
		}
		if _, err := Build(pkg); err == nil || !strings.Contains(err.Error(), `nothing in this package uses a phone route`) {
			t.Fatalf("got %v", err)
		}
	})
}

// The two guards this feature deleted are gone because their inputs cannot be
// written any more, not because the checks were dropped. Both cases below used
// to need a target that declares a route and omits half of it; a target now
// declares no route at all, so the refusal is the moved-field one and it names
// the connection file (research R2, task T022).
func TestCollapsedDailyGuardsAreUnrepresentable(t *testing.T) {
	for _, tc := range []struct {
		name  string
		apply func(target *packagespec.Target)
		want  string
	}{
		{
			name:  "daily-sip with a telephony channel and no carrier",
			apply: func(target *packagespec.Target) { target.Transport = "daily-sip" },
			want:  `declares transport: daily-sip, which now belongs in`,
		},
		{
			name:  "daily-sip with a carrier and no telephony channel",
			apply: func(target *packagespec.Target) { target.Carrier = "twilio" },
			want:  `declares carrier: twilio, which now belongs in`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pkg := loadSafeCore(t)
			enableTelephony(pkg)
			target := pkg.Targets["pipecat"]
			tc.apply(&target)
			pkg.Targets = map[string]packagespec.Target{"pipecat": target}
			_, err := Build(pkg)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v, want a refusal containing %q", err, tc.want)
			}
		})
	}
}

// The carrier-backed Daily route resolves a Redis-free phone plan.
func TestBuildResolvesPipecatDailyCarrierPlan(t *testing.T) {
	dailyCarrier := func(t *testing.T) *packagespec.Package {
		t.Helper()
		pkg := loadSafeCore(t)
		enableTelephony(pkg)
		target := pkg.Targets["pipecat"]
		target.Connection = "twilio_sip_daily"
		pkg.Targets = map[string]packagespec.Target{"pipecat": target}
		pkg.Connections = map[string]packagespec.Connection{"twilio_sip_daily": {
			Transport: "daily-sip", Carrier: "twilio", Environment: map[string]string{
				"account_sid": "TWILIO_ACCOUNT_SID", "auth_token": "TWILIO_AUTH_TOKEN",
				"sip_address": "SIP_TRUNK_HOSTNAME", "from_number": "SIP_FROM_NUMBER",
			},
		}}
		return pkg
	}

	agent, err := Build(dailyCarrier(t))
	if err != nil {
		t.Fatal(err)
	}
	plan := agent.Targets["pipecat"].Telephony
	if plan == nil {
		t.Fatal("the carrier form resolves no telephony plan, so nothing on the route renders")
	}
	if plan.Key.Provider != ProviderPipecat || plan.Key.Transport != "daily-sip" || plan.Key.Carrier != "twilio" {
		t.Fatalf("route = %#v", plan.Key)
	}
	// No redis: this route keeps no shared control record, so a Redis service
	// would be an idle one.
	if got := strings.Join(plan.Services, ","); got != "application" {
		t.Fatalf("services = %q, want application only", got)
	}
	if got := coordinationReasonNames(plan.CoordinationReasons); got != "admission" {
		t.Fatalf("coordination reasons = %q, want admission only", got)
	}
	if plan.Coordination != "shared" {
		t.Fatalf("coordination = %q", plan.Coordination)
	}

	t.Run("no connection names the connection", func(t *testing.T) {
		pkg := dailyCarrier(t)
		target := pkg.Targets["pipecat"]
		target.Connection = ""
		pkg.Targets = map[string]packagespec.Target{"pipecat": target}
		if _, err := Build(pkg); err == nil || !strings.Contains(err.Error(), "has a telephony channel and names no connection") {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("carrier is required", func(t *testing.T) {
		pkg := dailyCarrier(t)
		conn := pkg.Connections["twilio_sip_daily"]
		conn.Carrier, conn.Environment = "", nil
		pkg.Connections["twilio_sip_daily"] = conn
		_, err := Build(pkg)
		if err == nil {
			t.Fatal("carrierless daily-sip remained an accepted route")
		}
		for _, want := range []string{`transport "daily-sip" declares no carrier`, "daily-sip with twilio", "connections/twilio_sip_daily.yaml"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("the refusal is missing %q: %v", want, err)
			}
		}
	})
}

// withTelephonyRoute points a target at a connection. The route itself — the
// transport and the carrier — is declared in that connection file, so naming it
// is all a target does about telephony (spec FR-001).
func withTelephonyRoute(target packagespec.Target, connection string) packagespec.Target {
	target.Connection = connection
	return target
}

// routeTarget points one target at a connection and declares the route there.
// It replaces the target map, which is what these fixtures did by hand when the
// route lived on the target.
func routeTarget(pkg *packagespec.Package, name, connection, transport, carrier string) {
	target := pkg.Targets[name]
	target.Connection = connection
	pkg.Targets = map[string]packagespec.Target{name: target}
	if pkg.Connections == nil {
		pkg.Connections = map[string]packagespec.Connection{}
	}
	conn := pkg.Connections[connection]
	conn.Transport, conn.Carrier = transport, carrier
	pkg.Connections[connection] = conn
}

func coordinationReasonNames(reasons []TelephonyCoordinationReason) string {
	names := make([]string, 0, len(reasons))
	for _, reason := range reasons {
		names = append(names, reason.Name)
	}
	return strings.Join(names, ",")
}

func TestBuildRejectsUnknownOrInvalidConnection(t *testing.T) { // telephony V1-V3
	tests := []struct {
		name   string
		mutate func(*packagespec.Package)
		want   string
	}{
		{
			name: "missing reference",
			mutate: func(pkg *packagespec.Package) {
				enableTelephony(pkg)
				target := pkg.Targets["pipecat"]
				target.Connection = "missing_phone"
				pkg.Targets = map[string]packagespec.Target{"pipecat": target}
			},
			want: "missing_phone",
		},
		{
			name: "invalid environment name",
			mutate: func(pkg *packagespec.Package) {
				connection := pkg.Connections["primary_phone"]
				connection.Environment["auth_token"] = "not-a-name"
				pkg.Connections["primary_phone"] = connection
			},
			want: "environment variable name",
		},
		{
			name: "missing route environment key",
			mutate: func(pkg *packagespec.Package) {
				enableTelephony(pkg)
				connection := pkg.Connections["primary_phone"]
				delete(connection.Environment, "auth_token")
				pkg.Connections["primary_phone"] = connection
				routeTarget(pkg, "pipecat", "primary_phone", "cloud-websocket", "twilio")
			},
			want: `requires environment key "auth_token"`,
		},
		{
			name: "unknown route environment key",
			mutate: func(pkg *packagespec.Package) {
				enableTelephony(pkg)
				connection := pkg.Connections["primary_phone"]
				connection.Environment["api_key"] = "TWILIO_API_KEY"
				pkg.Connections["primary_phone"] = connection
				routeTarget(pkg, "pipecat", "primary_phone", "cloud-websocket", "twilio")
			},
			want: `environment key "api_key" is not accepted`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			pkg := loadSafeCore(t)
			test.mutate(pkg)
			if _, err := Build(pkg); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("got %v", err)
			}
		})
	}
}

func TestBuildTracing(t *testing.T) { // V25
	pkg := loadSafeCore(t)
	pkg.Agent.Tracing = &packagespec.Tracing{Provider: "langfuse"}
	agent, err := Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	if agent.Tracing == nil || agent.Tracing.Provider != "langfuse" {
		t.Fatalf("tracing = %#v", agent.Tracing)
	}

	pkg.Agent.Tracing.Provider = "coval"
	agent, err = Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	if agent.Tracing == nil || agent.Tracing.Provider != "coval" {
		t.Fatalf("tracing = %#v", agent.Tracing)
	}

	pkg.Agent.Tracing.Provider = "other"
	_, err = Build(pkg)
	if err == nil || !strings.Contains(err.Error(), `unsupported tracing provider "other"`) {
		t.Fatalf("got %v", err)
	}
	// The error has to name every provider that would have worked, or the author
	// has to go read the source to find out.
	for _, provider := range TracingProviders {
		if !strings.Contains(err.Error(), provider) {
			t.Errorf("error does not name the supported provider %q: %v", provider, err)
		}
	}
}

// Each provider needs its own credentials named, and only its own. A package
// that switches provider and keeps the old required secrets is a package whose
// compile report lies.
func TestBuildTracingSecretsAreProviderSpecific(t *testing.T) {
	if len(TracingSecrets) != len(TracingProviders) {
		t.Fatalf("every provider needs a secret list: %v vs %v", TracingSecrets, TracingProviders)
	}
	for _, provider := range TracingProviders {
		if len(TracingSecrets[provider]) == 0 {
			t.Errorf("provider %q declares no required secret", provider)
		}
	}
}

func TestBuildReportsUnresolvedReferenceAtSource(t *testing.T) { // V1
	pkg := loadSafeCore(t)
	intake := pkg.Agent.Agents["intake"]
	intake.Model = "missing_model"
	pkg.Agent.Agents["intake"] = intake
	_, err := Build(pkg)
	if err == nil || !strings.Contains(err.Error(), "agent.yaml") || !strings.Contains(err.Error(), "missing_model") {
		t.Fatalf("got %v", err)
	}
}

func TestBuildRejectsBadAndCollidingNames(t *testing.T) { // V7
	tests := []struct {
		name   string
		mutate func(*packagespec.Package)
		want   string
	}{
		{
			name: "reserved underscore",
			mutate: func(pkg *packagespec.Package) {
				pkg.Agent.Models.Think["_private"] = packagespec.ModelDef{Provider: "openai", Model: "x"}
			},
			want: "lowercase snake_case",
		},
		{
			name: "tool control collision",
			mutate: func(pkg *packagespec.Package) {
				destination := "billing_line"
				pkg.Agent.Controls["lookup_customer"] = packagespec.Control{Kind: "human_transfer", Cold: &packagespec.ColdTransfer{Destination: destination}}
			},
			want: "collide",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			pkg := loadSafeCore(t)
			test.mutate(pkg)
			_, err := Build(pkg)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("got %v", err)
			}
		})
	}
}

func TestBuildRejectsFieldsFromAnotherControlKind(t *testing.T) { // V3
	pkg := loadSafeCore(t)
	control := pkg.Agent.Controls["to_billing"]
	control.Task = new(string)
	pkg.Agent.Controls["to_billing"] = control
	_, err := Build(pkg)
	if err == nil || !strings.Contains(err.Error(), `field "task" is illegal with control kind "agent_transfer"`) {
		t.Fatalf("got %v", err)
	}
}

func TestBuildFlattensAndRejectsFallbackCycles(t *testing.T) { // V10
	pkg := loadSafeCore(t)
	fast := pkg.Agent.Models.Think["fast_reasoning"]
	fast.Fallback = []string{"careful_reasoning"}
	pkg.Agent.Models.Think["fast_reasoning"] = fast
	agent, err := Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	if got := agent.Models["fast_reasoning"].Fallback; len(got) != 1 || got[0] != "careful_reasoning" {
		t.Fatalf("flattened fallback = %v", got)
	}

	careful := pkg.Agent.Models.Think["careful_reasoning"]
	careful.Fallback = []string{"fast_reasoning"}
	pkg.Agent.Models.Think["careful_reasoning"] = careful
	_, err = Build(pkg)
	if err == nil || !strings.Contains(err.Error(), "fallback cycle") {
		t.Fatalf("got %v", err)
	}
}

func TestBuildEnforcesModelReferenceContract(t *testing.T) { // V22
	tests := []struct {
		name   string
		mutate func(*packagespec.Package)
		want   string
	}{
		{
			name: "wrong section reference",
			mutate: func(pkg *packagespec.Package) {
				intake := pkg.Agent.Agents["intake"]
				intake.Voice = "fast_reasoning" // a think entry used as a voice
				pkg.Agent.Agents["intake"] = intake
			},
			want: "is a think model, not a speak model",
		},
		{
			name: "cross-section name collision",
			mutate: func(pkg *packagespec.Package) {
				pkg.Agent.Models.Speak["fast_reasoning"] = packagespec.ModelDef{Provider: "slng", Voice: "x"}
			},
			want: "names share one namespace",
		},
		{
			name: "ambiguous listen selection",
			mutate: func(pkg *packagespec.Package) {
				pkg.Agent.Models.Listen["alternate"] = packagespec.ModelDef{Provider: "soniox", Model: "stt-rt-v5"}
			},
			want: "select one with listen:",
		},
		{
			name: "pointer to unknown entry",
			mutate: func(pkg *packagespec.Package) {
				pkg.Agent.Listen = "ghost"
			},
			want: "does not name a models.listen entry",
		},
		{
			name: "fallback on a speak model",
			mutate: func(pkg *packagespec.Package) {
				voice := pkg.Agent.Models.Speak["front_desk"]
				voice.Fallback = []string{"specialist"}
				pkg.Agent.Models.Speak["front_desk"] = voice
			},
			want: "fallback is legal on think and listen models",
		},
		{
			name: "listen fallback cycle",
			mutate: func(pkg *packagespec.Package) {
				pkg.Agent.Models.Listen["backup_stt"] = packagespec.ModelDef{Provider: "soniox", Model: "stt-rt-v5", Fallback: []string{"transcriber"}}
				primary := pkg.Agent.Models.Listen["transcriber"]
				primary.Fallback = []string{"backup_stt"}
				pkg.Agent.Models.Listen["transcriber"] = primary
			},
			want: "fallback cycle",
		},
		{
			name: "listen fallback must stay in the listen section",
			mutate: func(pkg *packagespec.Package) {
				primary := pkg.Agent.Models.Listen["transcriber"]
				primary.Fallback = []string{"fast_reasoning"} // a think entry
				pkg.Agent.Models.Listen["transcriber"] = primary
			},
			want: "does not resolve",
		},
		{
			name: "unknown override key",
			mutate: func(pkg *packagespec.Package) {
				target := pkg.Targets["pipecat"]
				target.Models = map[string]packagespec.ModelDef{"ghost": {Provider: "openai", Model: "gpt-4o"}}
				pkg.Targets["pipecat"] = target
			},
			want: "not a defined model",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			pkg := loadSafeCore(t)
			test.mutate(pkg)
			_, err := Build(pkg)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("got %v", err)
			}
		})
	}
}

func TestBuildAllowsPaletteAlternates(t *testing.T) { // V22: unreferenced entries are legal
	pkg := loadSafeCore(t)
	pkg.Agent.Models.Think["experimental"] = packagespec.ModelDef{Provider: "anthropic", Model: "claude-sonnet-4-6"}
	pkg.Agent.Models.Listen["alternate"] = packagespec.ModelDef{Provider: "soniox", Model: "stt-rt-v5"}
	pkg.Agent.Listen = "transcriber" // 2+ listen entries need an explicit selection
	agent, err := Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	if agent.Listen != "transcriber" {
		t.Fatalf("listen selection = %q", agent.Listen)
	}
	// Alternates stay defined but never resolve into any target's bindings.
	for name, target := range agent.Targets {
		if _, ok := target.Models.Reason["experimental"]; ok {
			t.Fatalf("target %q compiled the unused think alternate", name)
		}
		if target.Models.Listen != nil && target.Models.Listen.Provider == "soniox" {
			t.Fatalf("target %q compiled the unselected listen alternate", name)
		}
	}
}

func TestT16_ListenFallbackResolvesIntoBindings(t *testing.T) {
	pkg := loadSafeCore(t)
	pkg.Agent.Models.Listen["backup_stt"] = packagespec.ModelDef{Provider: "soniox", Model: "stt-rt-v5"}
	primary := pkg.Agent.Models.Listen["transcriber"]
	primary.Fallback = []string{"backup_stt"}
	pkg.Agent.Models.Listen["transcriber"] = primary
	// No listen: pointer on purpose — a fallback-only entry is part of the
	// chain, so transcriber is the sole head and selects itself (T16).
	agent, err := Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	if agent.Listen != "transcriber" {
		t.Fatalf("listen selection = %q", agent.Listen)
	}
	if got := agent.Models["transcriber"].Fallback; len(got) != 1 || got[0] != "backup_stt" {
		t.Fatalf("flattened listen fallback = %v", got)
	}
	livekit := targetFor(agent, ProviderLiveKit)
	if len(livekit.Models.ListenFallbacks) != 1 || livekit.Models.ListenFallbacks[0].Name != "backup_stt" || livekit.Models.ListenFallbacks[0].Binding.Provider != "soniox" {
		t.Fatalf("listen fallback bindings = %#v", livekit.Models.ListenFallbacks)
	}
}

// A destination names an environment variable and nothing else. The literal
// forms SCHEMA N26 accepted are refused, because agent.yaml is the portable
// half of a package and a number is a deployment fact (spec FR-004d).
func TestBuildValidatesDestinationValues(t *testing.T) {
	for _, test := range []struct {
		value string
		valid bool
	}{
		{"BILLING_PHONE_NUMBER", true},
		{"+14155550123", false},
		{"sip:billing@example.com", false},
		{"sips:billing@example.com", false},
		{"", false},
		{"billing@example.com", false},
		{"not-a-phone", false},
	} {
		if got := envNamePattern.MatchString(test.value); got != test.valid {
			t.Errorf("destination %q accepted = %t, want %t", test.value, got, test.valid)
		}
	}

	pkg := loadSafeCore(t)
	addColdHumanTransfer(pkg)
	pkg.Agent.Destinations["billing_line"] = "+14155550123"
	_, err := Build(pkg)
	if err == nil || !strings.Contains(err.Error(), "a literal") {
		t.Fatalf("got %v", err)
	}
	if err != nil && !strings.Contains(err.Error(), "BILLING_PHONE_NUMBER") {
		t.Errorf("the refusal must show the form that works: %v", err)
	}
	// And never the number itself. A destination that is not a name is a phone
	// number, and the repository's hardest rule is that no output carries one.
	if err != nil && strings.Contains(err.Error(), "+14155550123") {
		t.Errorf("the refusal prints the number back, into a terminal and a CI log: %v", err)
	}
}

// A connection's environment maps a role onto a variable NAME. What lands there
// when it is wrong is a pasted credential, so the refusal locates it without
// printing it (Wave B, 2026-08-15).
func TestBuildDoesNotEchoAPastedConnectionValue(t *testing.T) {
	const pasted = "sk-live-pretend-key-value"
	pkg := loadSafeCore(t)
	connection := pkg.Connections["primary_phone"]
	connection.Environment["auth_token"] = pasted
	pkg.Connections["primary_phone"] = connection

	_, err := Build(pkg)
	if err == nil {
		t.Fatal("a value where a name belongs must be refused")
	}
	if !strings.Contains(err.Error(), "auth_token") {
		t.Errorf("the refusal must name the key so the author can find it: %v", err)
	}
	if strings.Contains(err.Error(), pasted) {
		t.Errorf("the refusal repeats the pasted value back: %v", err)
	}
}

// TestUnreachableControlIsRefused walks every row of the control reachability
// table. A package declares things and
// attaches things; anything declared and never attached is an error, with one
// carve-out. Before this check, every row below compiled at exit 0 and the
// declaration simply never reached the generated project (reproduction.md A).
func TestUnreachableControlIsRefused(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*packagespec.Package)
		want   string // "" means the package must still build
	}{
		{
			name:   "unattached human_transfer, cold",
			mutate: func(pkg *packagespec.Package) { detachTool(pkg, "billing", "to_human") },
			want:   `control "to_human" is declared but no agent reaches it`,
		},
		{
			name: "unattached human_transfer, warm",
			mutate: func(pkg *packagespec.Package) {
				control := pkg.Agent.Controls["to_human"]
				control.Cold, control.Warm = nil, &packagespec.WarmTransfer{Destination: "billing_line"}
				pkg.Agent.Controls["to_human"] = control
				detachTool(pkg, "billing", "to_human")
			},
			want: `control "to_human" is declared but no agent reaches it`,
		},
		{
			name:   "unattached agent_transfer",
			mutate: func(pkg *packagespec.Package) { detachTool(pkg, "intake", "to_billing") },
			want:   `control "to_billing" is declared but no agent reaches it`,
		},
		{
			name: "unattached delegate",
			mutate: func(pkg *packagespec.Package) {
				addTask(pkg, "check_balance")
				task := "check_balance"
				pkg.Agent.Controls["run_check"] = packagespec.Control{Kind: "delegate", Task: &task}
			},
			want: `control "run_check" is declared but no agent reaches it`,
		},
		{
			name: "unreferenced destination",
			mutate: func(pkg *packagespec.Package) {
				pkg.Agent.Destinations["front_desk_line"] = "FRONT_DESK_PHONE_NUMBER"
			},
			want: `destination "front_desk_line" is declared but no control resolves to it`,
		},
		{
			name:   "unreferenced top-level tool",
			mutate: func(pkg *packagespec.Package) { detachTool(pkg, "billing", "get_invoice") },
			want:   `tool "get_invoice" is declared but no agent reaches it`,
		},
		{
			name:   "unreachable task",
			mutate: func(pkg *packagespec.Package) { addTask(pkg, "check_balance") },
			want:   `task "check_balance" is declared but nothing reaches it`,
		},
		{
			name: "unreachable task group",
			mutate: func(pkg *packagespec.Package) {
				addTask(pkg, "check_balance")
				pkg.Agent.TaskGroups = map[string]packagespec.TaskGroup{}
				pkg.Agent.TaskGroups["closing"] = packagespec.TaskGroup{
					Steps: []string{"check_balance"}, ContextScope: "shared", Then: "return",
				}
			},
			want: `task group "closing" is declared but nothing reaches it`,
		},
		{
			name: "unreachable agent",
			mutate: func(pkg *packagespec.Package) {
				pkg.Agent.Agents["specialist"] = packagespec.AgentDef{
					Instructions: "instructions.md", Model: "careful_reasoning", Voice: "specialist",
				}
			},
			want: `agent "specialist" is declared but the entry agent "intake" cannot reach it`,
		},
		{
			// The models map is a palette: entries that nothing currently references
			// are legal. That rule is scoped to
			// models: and to nothing else, so the fix must not widen it.
			name: "unreferenced models entry stays legal",
			mutate: func(pkg *packagespec.Package) {
				pkg.Agent.Models.Think["spare_reasoning"] = packagespec.ModelDef{
					Description: "an unreferenced palette alternate", Provider: "openai", Model: "gpt-4o",
				}
			},
			want: "",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			pkg := loadSafeCore(t)
			addColdHumanTransfer(pkg)
			test.mutate(pkg)
			_, err := Build(pkg)
			if test.want == "" {
				if err != nil {
					t.Fatalf("must still build: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("built at exit 0: the declaration is dropped with no diagnostic, want %q", test.want)
			}
			if !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want it to contain %q", err, test.want)
			}
			if !strings.HasPrefix(err.Error(), "agent.yaml:") {
				t.Errorf("error = %v, want a file and line: this is a tier 1 refusal (research D7)", err)
			}
		})
	}
}

func TestTaskScopedAgentTransferIsReachable(t *testing.T) {
	pkg := loadSafeCore(t)
	detachTool(pkg, "intake", "to_billing")
	addTask(pkg, "route_billing")
	task := pkg.Agent.Tasks["route_billing"]
	task.Tools = []string{"to_billing"}
	pkg.Agent.Tasks["route_billing"] = task
	taskName := "route_billing"
	pkg.Agent.Controls["start_routing"] = packagespec.Control{Kind: "delegate", Task: &taskName}
	intake := pkg.Agent.Agents["intake"]
	intake.Tools = append(intake.Tools, "start_routing")
	pkg.Agent.Agents["intake"] = intake

	if _, err := Build(pkg); err != nil {
		t.Fatalf("task-scoped agent transfer must reach its target: %v", err)
	}
}

func addColdHumanTransfer(pkg *packagespec.Package) {
	pkg.Agent.Controls["to_human"] = packagespec.Control{
		Kind: "human_transfer", Cold: &packagespec.ColdTransfer{Destination: "billing_line"},
	}
	if pkg.Agent.Destinations == nil {
		pkg.Agent.Destinations = map[string]string{}
	}
	pkg.Agent.Destinations["billing_line"] = "BILLING_PHONE_NUMBER"
	billing := pkg.Agent.Agents["billing"]
	billing.Tools = append(billing.Tools, "to_human")
	pkg.Agent.Agents["billing"] = billing
}

// detachTool removes one name from an agent's tools: list, leaving whatever it
// named still declared. That is the whole shape of defect A.
func detachTool(pkg *packagespec.Package, agent, tool string) {
	def := pkg.Agent.Agents[agent]
	def.Tools = slices.DeleteFunc(slices.Clone(def.Tools), func(name string) bool { return name == tool })
	pkg.Agent.Agents[agent] = def
}

func addTask(pkg *packagespec.Package, name string) {
	if pkg.Agent.Tasks == nil {
		pkg.Agent.Tasks = map[string]packagespec.Task{}
	}
	pkg.Agent.Tasks[name] = packagespec.Task{
		Instructions: "instructions.md",
		Result:       map[string]any{"balance": "string"},
		Context:      packagespec.TaskContext{History: "full"},
	}
}

func loadSafeCore(t *testing.T) *packagespec.Package {
	t.Helper()
	pkg, err := packagespec.Load(filepath.Join("..", "testdata", "safe_core"))
	if err != nil {
		t.Fatal(err)
	}
	return pkg
}

func enableTelephony(pkg *packagespec.Package) {
	inbound, outbound := true, false
	pkg.Agent.Channels["phone"] = packagespec.Channel{
		Kind: "telephony", Inbound: &inbound, Outbound: &outbound,
		RequiredControls: []string{"cold_transfer", "hangup"},
	}
}

// TestBuildResolvesPipecatCloudWebsocketPlan walks every row of the route guard
// table, plus the SIP-key
// refusal. The row that matters most is the first: on this route a package that
// only *receives* calls needs no connection at all, because the platform receives
// the call without credentials (SCHEMA N38).
func TestBuildResolvesPipecatCloudWebsocketPlan(t *testing.T) {
	cloudWebsocket := func(t *testing.T, connection bool, outbound bool, transfer bool) *packagespec.Package {
		t.Helper()
		pkg := loadSafeCore(t)
		inbound := true
		controls := []string{"hangup"}
		if transfer {
			controls = append(controls, "cold_transfer")
		}
		pkg.Agent.Channels["phone"] = packagespec.Channel{
			Kind: "telephony", Inbound: &inbound, Outbound: &outbound,
			RequiredControls: controls,
		}
		if transfer {
			addColdHumanTransfer(pkg)
		}
		target := pkg.Targets["pipecat"]
		// Both shapes name a connection, because the connection is where the
		// route is written. What the receive-only shape drops is the credentials
		// inside it, which is the distinction this route is about.
		target.Connection = "twilio_voice"
		pkg.Connections = map[string]packagespec.Connection{"twilio_voice": {
			Transport: "cloud-websocket", Carrier: "twilio",
		}}
		if connection {
			conn := pkg.Connections["twilio_voice"]
			conn.Environment = map[string]string{
				"account_sid": "TWILIO_ACCOUNT_SID", "auth_token": "TWILIO_AUTH_TOKEN",
				"from_number": "TWILIO_PHONE_NUMBER",
			}
			pkg.Connections["twilio_voice"] = conn
		}
		pkg.Targets = map[string]packagespec.Target{"pipecat": target}
		return pkg
	}

	t.Run("phone inbound only, no credentials: valid", func(t *testing.T) {
		agent, err := Build(cloudWebsocket(t, false, false, false))
		if err != nil {
			t.Fatalf("a receive-only package on this route must compile with no credentials: %v", err)
		}
		built := agent.Targets["pipecat"]
		if built.Connection != "twilio_voice" {
			t.Errorf("connection = %q, want twilio_voice: the route is written there either way", built.Connection)
		}
		plan := built.Telephony
		if plan == nil {
			t.Fatal("no telephony plan, so the route would resolve and emit nothing: the silent downgrade")
		}
		if plan.Key.Transport != "cloud-websocket" || plan.Key.Carrier != "twilio" {
			t.Fatalf("route = %#v", plan.Key)
		}
		if len(plan.Environment) != 0 {
			t.Errorf("environment = %v, want none: receiving a call needs no credentials", plan.Environment)
		}
		// The row's defining shape: nothing for the operator to run or host.
		if len(plan.Processes) != 0 || len(plan.PublicEndpoints) != 0 {
			t.Errorf("plan declares %d process(es) and %d endpoint(s), want none of either", len(plan.Processes), len(plan.PublicEndpoints))
		}
		if got := strings.Join(plan.Services, ","); got != "application" {
			t.Errorf("services = %q, want application only", got)
		}
		if got := coordinationReasonNames(plan.CoordinationReasons); got != "admission" {
			t.Errorf("coordination reasons = %q, want admission only", got)
		}
	})

	t.Run("outbound with no credentials names the missing key", func(t *testing.T) {
		_, err := Build(cloudWebsocket(t, false, true, false))
		if err == nil {
			t.Fatal("a package that places calls compiled with no carrier credentials")
		}
		for _, want := range []string{"account_sid", "places or redirects calls", "only receives calls"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("the refusal is missing %q: %v", want, err)
			}
		}
	})

	t.Run("a transfer with no credentials says why they are needed", func(t *testing.T) {
		_, err := Build(cloudWebsocket(t, false, false, true))
		if err == nil {
			t.Fatal("a package that redirects calls compiled with no carrier credentials")
		}
		if !strings.Contains(err.Error(), "places or redirects calls") {
			t.Errorf("the refusal does not say why the credentials are needed: %v", err)
		}
	})

	// safe_core's cold transfer dials, so the phone channel has to go *and* the
	// transfer with it before nothing in the package uses the route (FR-016).
	t.Run("a connection nothing uses is dead weight", func(t *testing.T) {
		pkg := cloudWebsocket(t, true, false, false)
		delete(pkg.Agent.Channels, "phone")
		if _, err := Build(pkg); err == nil || !strings.Contains(err.Error(), "nothing in this package uses a phone route") {
			t.Fatalf("got %v", err)
		}
	})

	t.Run("a SIP key names the key, the route, and the accepted set", func(t *testing.T) {
		pkg := cloudWebsocket(t, true, true, false)
		connection := pkg.Connections["twilio_voice"]
		connection.Environment["sip_address"] = "SIP_TRUNK_HOSTNAME"
		pkg.Connections = map[string]packagespec.Connection{"twilio_voice": connection}
		_, err := Build(pkg)
		if err == nil {
			t.Fatal("a SIP key on this route compiled; nothing here speaks SIP")
		}
		for _, want := range []string{"sip_address", "cloud-websocket", "it accepts", "account_sid, auth_token, from_number"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("the refusal is missing %q: %v", want, err)
			}
		}
	})

	t.Run("outbound with a connection resolves the organization", func(t *testing.T) {
		agent, err := Build(cloudWebsocket(t, true, true, false))
		if err != nil {
			t.Fatal(err)
		}
		plan := agent.Targets["pipecat"].Telephony
		if !slices.Contains(plan.RequiredEnvironment, "PIPECAT_CLOUD_ORGANIZATION") {
			t.Errorf("required environment = %v, want PIPECAT_CLOUD_ORGANIZATION: outbound markup has to name the service host", plan.RequiredEnvironment)
		}
	})

	t.Run("a transfer does not need the organization", func(t *testing.T) {
		agent, err := Build(cloudWebsocket(t, true, false, true))
		if err != nil {
			t.Fatal(err)
		}
		plan := agent.Targets["pipecat"].Telephony
		if slices.Contains(plan.RequiredEnvironment, "PIPECAT_CLOUD_ORGANIZATION") {
			t.Errorf("transfer-only required environment = %v; no reconnect names a service host", plan.RequiredEnvironment)
		}
	})

	t.Run("inbound only does not ask for the organization", func(t *testing.T) {
		agent, err := Build(cloudWebsocket(t, false, false, false))
		if err != nil {
			t.Fatal(err)
		}
		plan := agent.Targets["pipecat"].Telephony
		if slices.Contains(plan.RequiredEnvironment, "PIPECAT_CLOUD_ORGANIZATION") {
			t.Errorf("a receive-only package is asked for PIPECAT_CLOUD_ORGANIZATION, which nothing on it reads: %v", plan.RequiredEnvironment)
		}
	})
}

// TestBuildToolAnnounceTrimsToASettledValue: one TrimSpace is the whole default
// resolution, so a driver never has to decide what a whitespace-only line means
// (FR-005). A real sentence survives byte for byte.
func TestBuildToolAnnounceTrimsToASettledValue(t *testing.T) {
	for _, tc := range []struct{ name, authored, want string }{
		{"sentence kept", "Let me check the calendar.", "Let me check the calendar."},
		{"surrounding space trimmed", "  Let me check.  ", "Let me check."},
		{"whitespace only reads as absent", "   \n\t ", ""},
		{"absent stays absent", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pkg := loadSafeCore(t)
			tool := pkg.Tools["lookup_customer"]
			tool.Announce = tc.authored
			pkg.Tools["lookup_customer"] = tool
			agent, err := Build(pkg)
			if err != nil {
				t.Fatal(err)
			}
			if got := agent.Tools["lookup_customer"].Announce; got != tc.want {
				t.Errorf("announce = %q, want %q", got, tc.want)
			}
		})
	}
}

// A target override replaces the vendor selection, not the silence window: the
// package sets endpointing_delay once for a slow transcriber, and a target that
// swaps the detector must not drop it (B: fragmented STT, 2026-08-20).
func TestTargetOverrideKeepsEndpointingDelay(t *testing.T) {
	agent := &Agent{
		Models: map[string]ModelDef{
			"detector": {Kind: KindTurn, Provider: "local", Model: "silero", EndpointingDelay: "1500ms"},
		},
		Turn: "detector",
	}
	overrides := map[string]packagespec.ModelDef{
		"detector": {Provider: "livekit", Model: "turn-detector-mini"},
	}
	bindings := resolveBindings(agent, map[string]bool{}, overrides)
	if bindings.Turn == nil {
		t.Fatal("no turn binding resolved")
	}
	if got := bindings.Turn.Model; got != "turn-detector-mini" {
		t.Errorf("override must still replace the model: got %q", got)
	}
	if got := bindings.Turn.EndpointingDelay; got != "1500ms" {
		t.Errorf("override dropped the endpointing delay: got %q", got)
	}

	// An override that states its own value still wins.
	overrides["detector"] = packagespec.ModelDef{Provider: "livekit", Model: "turn-detector-mini", EndpointingDelay: "800ms"}
	if got := resolveBindings(agent, map[string]bool{}, overrides).Turn.EndpointingDelay; got != "800ms" {
		t.Errorf("override value must win: got %q", got)
	}
}

// T086: the SIP plane's graph is the plane's, whichever driver's agent runs on
// it. Both routes name the same four services and the same one reason to keep a
// coordination store.
//
// This is not bookkeeping. `unmute dev --telephony` starts a SIP-plane run in two
// phases and reads this list for the first one: infrastructure, then the trunk
// and dispatch records, then the application. Derived from the *provider*, as it
// was, `(pipecat, sip)` claimed a two-container graph, so the first phase brought
// up the coordination store on its own and the records were then created against
// a LiveKit server that was not running.
//
// The two Pipecat correlation reasons are deliberately absent here. Each names a
// record the emitted carrier adapter keeps in Redis, and this route emits no
// carrier adapter: what it needs the store for is the server and the SIP service
// finding each other.
// Was "both SIP plane routes". The Pipecat one deployed to no managed platform
// and is gone, so one route runs on this plane now. The loop is kept at one
// entry rather than flattened because the whole test goes when the plane does.
func TestBuildGivesTheSIPPlaneRouteThePlanesGraph(t *testing.T) {
	for _, provider := range []string{"livekit"} {
		t.Run(provider, func(t *testing.T) {
			pkg := loadSafeCore(t)
			enableTelephony(pkg)
			routeTarget(pkg, provider, "primary_phone", "sip", "twilio")
			connection := pkg.Connections["primary_phone"]
			connection.Environment = map[string]string{
				"sip_address": "SIP_TRUNK_HOSTNAME", "sip_username": "SIP_AUTH_USERNAME",
				"sip_password": "SIP_AUTH_PASSWORD", "from_number": "SIP_FROM_NUMBER",
			}
			pkg.Connections["primary_phone"] = connection

			agent, err := Build(pkg)
			if err != nil {
				t.Fatal(err)
			}
			plan := agent.Targets[provider].Telephony
			if plan == nil {
				t.Fatal("telephony plan was not resolved")
			}
			// The LiveKit SIP route's four: the agent, the LiveKit Server and SIP
			// service it needs, and the store those two find each other through.
			for _, want := range []string{"application", "livekit_server", "livekit_sip", "redis"} {
				if !slices.Contains(plan.Services, want) {
					t.Errorf("the plan does not name %s, so the two-phase startup will not bring it up: %v",
						want, plan.Services)
				}
			}
			if got := coordinationReasonNames(plan.CoordinationReasons); got != "livekit_control_plane" {
				t.Errorf("coordination reasons = %s, want livekit_control_plane alone", got)
			}
		})
	}
}
