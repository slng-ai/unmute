package ir

import (
	"path/filepath"
	"strings"
	"testing"

	packagespec "github.com/slng/unmute/internal/spec"
)

func TestBuildBuiltinToolResolvesRegistryDefaults(t *testing.T) {
	pkg := loadSafeCore(t)
	tool := pkg.Tools["lookup_customer"]
	tool.Execution = "builtin"
	tool.Builtin = "end_call"
	tool.Instructions = "Thank the caller and say goodbye."
	tool.Input, tool.Output, tool.URLEnv, tool.Handler, tool.Effect, tool.Description = nil, nil, "", "", "", ""
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
	if agent.EntryAgent != "intake" || len(agent.Targets) != 4 {
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

func TestBuildResolvesExactTelephonyPlan(t *testing.T) { // telephony V2, V4-V6
	pkg := loadSafeCore(t)
	enableTelephony(pkg)
	target := pkg.Targets["pipecat"]
	target.Transport = "carrier-websocket"
	target.Carrier = "twilio"
	target.Connection = "primary_phone"
	pkg.Targets = map[string]packagespec.Target{"pipecat": target}

	agent, err := Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	plan := agent.Targets["pipecat"].Telephony
	if plan == nil {
		t.Fatal("telephony plan was not resolved")
	}
	if plan.Key.Transport != "carrier-websocket" || plan.Key.Carrier != "twilio" || plan.Connection != "primary_phone" {
		t.Fatalf("route = %#v", plan)
	}
	if plan.Coordination != "shared" || plan.AdmissionOwner != "generated_runtime" || len(plan.Evidence) == 0 {
		t.Fatalf("incomplete plan = %#v", plan)
	}
	if got := strings.Join(plan.Services, ","); got != "application,redis" {
		t.Fatalf("services = %s", got)
	}
	if len(plan.Processes) != 1 || len(plan.PublicEndpoints) != 3 || len(plan.ManualSteps) == 0 {
		t.Fatalf("runtime facts = %#v", plan)
	}
	if got := strings.Join(plan.LocalEnvironment, ","); got != "REDIS_URL" {
		t.Fatalf("locally supplied environment = %s", got)
	}
	if got := strings.Join(plan.RequiredEnvironment, ","); got != "REDIS_URL,TWILIO_ACCOUNT_SID,TWILIO_AUTH_TOKEN,TWILIO_PHONE_NUMBER,UNMUTE_PUBLIC_URL" {
		t.Fatalf("required environment = %s", got)
	}
	if got := coordinationReasonNames(plan.CoordinationReasons); got != "admission,call_correlation,callback_idempotency,human_transfer" {
		t.Fatalf("coordination reasons = %s", got)
	}
	if plan.AutoWebhookEndpoint != "inbound" || len(plan.DevEnvironment) != 0 {
		t.Fatalf("auto webhook = %q, dev environment = %v", plan.AutoWebhookEndpoint, plan.DevEnvironment)
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
	target := pkg.Targets["pipecat"]
	target.Transport, target.Carrier, target.Connection = "carrier-websocket", "twilio", "primary_phone"
	pkg.Targets = map[string]packagespec.Target{"pipecat": target}

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
	target := pkg.Targets["livekit"]
	target.Transport, target.Carrier, target.Connection = "sip", "twilio", "primary_phone"
	pkg.Targets = map[string]packagespec.Target{"livekit": target}
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
	// Fixture is inbound-only, so only the inbound trunk ID is dev-supplied.
	if got := strings.Join(plan.DevEnvironment, ","); got != "LIVEKIT_SIP_INBOUND_TRUNK" {
		t.Fatalf("LiveKit SIP dev-supplied environment = %s", got)
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
	pipecat.Transport, livekit.Transport = "carrier-websocket", "sip"
	pkg.Targets = map[string]packagespec.Target{
		"pipecat_twilio": withTelephonyRoute(pipecat, "twilio", "twilio_api"),
		"pipecat_telnyx": withTelephonyRoute(pipecat, "telnyx", "telnyx_api"),
		"livekit_twilio": withTelephonyRoute(livekit, "twilio", "twilio_sip"),
		"livekit_plivo":  withTelephonyRoute(livekit, "plivo", "plivo_sip"),
	}
	pkg.Connections = map[string]packagespec.Connection{
		"twilio_api": {Kind: "telephony", Environment: map[string]string{
			"account_sid": "TWILIO_ACCOUNT_SID", "auth_token": "TWILIO_AUTH_TOKEN", "from_number": "TWILIO_PHONE_NUMBER",
		}},
		"telnyx_api": {Kind: "telephony", Environment: map[string]string{
			"api_key": "TELNYX_API_KEY", "public_key": "TELNYX_PUBLIC_KEY", "connection_id": "TELNYX_CONNECTION_ID", "from_number": "TELNYX_PHONE_NUMBER",
		}},
		"twilio_sip": {Kind: "telephony", Environment: map[string]string{
			"sip_address": "TWILIO_SIP_ADDRESS", "sip_username": "TWILIO_SIP_USERNAME", "sip_password": "TWILIO_SIP_PASSWORD", "from_number": "TWILIO_PHONE_NUMBER",
		}},
		"plivo_sip": {Kind: "telephony", Environment: map[string]string{
			"sip_address": "PLIVO_SIP_ADDRESS", "sip_username": "PLIVO_SIP_USERNAME", "sip_password": "PLIVO_SIP_PASSWORD", "from_number": "PLIVO_PHONE_NUMBER",
		}},
	}

	agent, err := Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]string{
		"pipecat_twilio": "pipecat/carrier-websocket/twilio/twilio_api",
		"pipecat_telnyx": "pipecat/carrier-websocket/telnyx/telnyx_api",
		"livekit_twilio": "livekit/sip/twilio/twilio_sip",
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
		pkg.Targets = map[string]packagespec.Target{"pipecat": pkg.Targets["pipecat"]}
		if _, err := Build(pkg); err == nil || !strings.Contains(err.Error(), `target "pipecat" requires connection for telephony`) {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("connection without channel", func(t *testing.T) {
		pkg := loadSafeCore(t)
		target := pkg.Targets["livekit"]
		target.Connection = "primary_phone"
		pkg.Targets = map[string]packagespec.Target{"livekit": target}
		if _, err := Build(pkg); err == nil || !strings.Contains(err.Error(), `sets connection but has no telephony channel`) {
			t.Fatalf("got %v", err)
		}
	})
}

func withTelephonyRoute(target packagespec.Target, carrier, connection string) packagespec.Target {
	target.Carrier, target.Connection = carrier, connection
	return target
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
				target := pkg.Targets["pipecat"]
				target.Transport, target.Carrier, target.Connection = "carrier-websocket", "twilio", "primary_phone"
				pkg.Targets = map[string]packagespec.Target{"pipecat": target}
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
				target := pkg.Targets["pipecat"]
				target.Transport, target.Carrier, target.Connection = "carrier-websocket", "twilio", "primary_phone"
				pkg.Targets = map[string]packagespec.Target{"pipecat": target}
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

	pkg.Agent.Tracing.Provider = "other"
	if _, err := Build(pkg); err == nil || !strings.Contains(err.Error(), `unsupported tracing provider "other"`) {
		t.Fatalf("got %v", err)
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
				destination, mode := "billing_line", "cold"
				pkg.Agent.Controls["lookup_customer"] = packagespec.Control{Kind: "human_transfer", Destination: &destination, Mode: &mode}
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

func TestBuildValidatesDestinationValues(t *testing.T) {
	for _, test := range []struct {
		value string
		valid bool
	}{
		{"+14155550123", true},
		{"sip:billing@example.com", true},
		{"sips:billing@example.com", true},
		{"", false},
		{"billing@example.com", false},
		{"not-a-phone", false},
	} {
		if got := validDestination(test.value); got != test.valid {
			t.Errorf("validDestination(%q) = %t", test.value, got)
		}
	}

	pkg := loadSafeCore(t)
	target := pkg.Targets["livekit"]
	target.Destinations["billing_line"] = ""
	pkg.Targets["livekit"] = target
	_, err := Build(pkg)
	if err == nil || !strings.Contains(err.Error(), "E.164 phone number or SIP URI") {
		t.Fatalf("got %v", err)
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
