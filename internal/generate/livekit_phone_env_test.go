package generate

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/ir"
	"github.com/slng-ai/unmute/internal/spec"
	"github.com/slng-ai/unmute/internal/target"
)

func TestLiveKitHumanTransferEnvironmentFollowsTheCallMode(t *testing.T) {
	pkg, err := spec.Load(filepath.Join("..", "..", "examples", "livekit-human-transfer"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := Generate(agent, targetByProvider(t, agent, ir.ProviderLiveKit), target.Default())
	if err != nil {
		t.Fatal(err)
	}

	agentPy := artifactFile(t, artifact, "agent.py")
	required := requiredEnvBlock(t, agentPy)
	compose := artifactFile(t, artifact, "compose.dev.yaml")
	for _, name := range []string{
		"SUPERVISOR_PHONE_NUMBER",
		"SIP_TRUNK_HOSTNAME",
		"SIP_AUTH_USERNAME",
		"SIP_AUTH_PASSWORD",
		"SIP_FROM_NUMBER",
	} {
		if !strings.Contains(required, name) {
			t.Errorf("browser REQUIRED_ENV missing warm-transfer value %s", name)
		}
		if !strings.Contains(compose, "- "+name) {
			t.Errorf("browser compose missing warm-transfer value %s", name)
		}
	}
	if strings.Contains(required, "BILLING_PHONE_NUMBER") {
		t.Error("browser REQUIRED_ENV demands the cold-transfer destination")
	}
	if strings.Contains(compose, "- BILLING_PHONE_NUMBER") {
		t.Error("browser compose demands the cold-transfer destination")
	}

	callStart := strings.Index(agentPy, "CALL_REQUIRED_ENV = [")
	if callStart < 0 {
		t.Fatal("agent.py has no CALL_REQUIRED_ENV")
	}
	callEnd := strings.Index(agentPy[callStart:], "]")
	if callEnd < 0 {
		t.Fatal("agent.py CALL_REQUIRED_ENV is unterminated")
	}
	callRequired := agentPy[callStart : callStart+callEnd]
	if !strings.Contains(callRequired, "BILLING_PHONE_NUMBER") {
		t.Error("phone call check is missing the cold-transfer destination")
	}
	if strings.Contains(callRequired, "SUPERVISOR_PHONE_NUMBER") {
		t.Error("phone-only check contains the always-on warm destination")
	}

	entryAt := strings.Index(agentPy, "async def entrypoint(ctx: JobContext) -> None:")
	if entryAt < 0 {
		t.Fatal("agent.py has no entrypoint")
	}
	entrypoint := agentPy[entryAt:]
	outboundCheck := strings.Index(entrypoint, "if outbound_job:\n        require_call_env()")
	outboundStart := strings.Index(entrypoint, "await session.start(")
	if outboundCheck < 0 || outboundStart < 0 || outboundCheck > outboundStart {
		t.Error("outbound phone jobs do not check the cold destination before the session starts")
	}
	inboundCheck := strings.Index(entrypoint, "if not outbound_job and participant.kind == rtc.ParticipantKind.PARTICIPANT_KIND_SIP:\n        require_call_env()")
	if inboundCheck < 0 {
		t.Error("inbound SIP jobs do not check the cold destination")
	} else if greeting := strings.Index(entrypoint[inboundCheck:], "await _livekit_entry_greeting(session)"); greeting < 0 {
		t.Error("inbound SIP call check is not before the greeting")
	}

	coldAt := strings.Index(agentPy, "async def send_to_billing(")
	warmAt := strings.Index(agentPy, "async def escalate_to_supervisor(")
	if coldAt < 0 || warmAt < 0 || coldAt >= warmAt {
		t.Fatal("generated transfer methods are missing or out of order")
	}
	cold, warm := agentPy[coldAt:warmAt], agentPy[warmAt:entryAt]
	if !strings.Contains(cold, `transfer_to=_refer_uri(os.environ["BILLING_PHONE_NUMBER"])`) || strings.Contains(cold, "sip_call_to=") {
		t.Error("cold transfer did not build only its REFER destination")
	}
	if !strings.Contains(warm, `sip_call_to=_sip_user(os.environ["SUPERVISOR_PHONE_NUMBER"])`) || strings.Contains(warm, "transfer_to=") {
		t.Error("warm transfer did not build only its dial destination")
	}
}

func TestLiveKitSIPBuildsCallContextBeforeHydration(t *testing.T) {
	agent, resolved := configuredLiveKitSIP(t)
	entry := agent.Agents[agent.EntryAgent]
	entry.Instructions = "Campaign {{campaign_id}} for call {{provider_call_id}}."
	agent.Agents[agent.EntryAgent] = entry
	artifact, err := GenerateLiveKit(agent, resolved, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	agentPy := artifactFile(t, artifact, "agent.py")
	if !strings.Contains(agentPy, "from livekit import rtc") {
		t.Error("warm-only SIP variables use rtc.ParticipantKind without importing rtc")
	}
	entryAt := strings.Index(agentPy, "async def entrypoint(ctx: JobContext) -> None:")
	if entryAt < 0 {
		t.Fatal("agent.py has no entrypoint")
	}
	entrypoint := agentPy[entryAt:]
	preContextAt := strings.Index(entrypoint, "call_context = _livekit_call_context(ctx.room.name, None, metadata)")
	preCallStartAt := strings.Index(entrypoint, "_hydrate_call_start(session.userdata, call_start)")
	startAt := strings.Index(entrypoint, "await session.start(")
	outboundParticipantAt := strings.Index(entrypoint, `participant = await ctx.wait_for_participant(identity="phone_user")`)
	outboundContextAt := strings.Index(entrypoint, "call_context = _livekit_call_context(ctx.room.name, participant, metadata)")
	refreshAt := strings.Index(entrypoint, "await session.current_agent.update_instructions(")
	amdAt := strings.Index(entrypoint, "amd_result = await detector.execute()")
	participantAt := strings.Index(entrypoint, "participant = await ctx.wait_for_participant")
	guardAt := strings.Index(entrypoint, "if not outbound_job and participant.kind == rtc.ParticipantKind.PARTICIPANT_KIND_SIP:\n        call_context =")
	contextAt := strings.LastIndex(entrypoint, "call_context = _livekit_call_context(ctx.room.name, participant, metadata)")
	hydrateAt := strings.LastIndex(entrypoint, "_hydrate_livekit_context(session.userdata, call_context)")
	callStartAt := strings.LastIndex(entrypoint, "_hydrate_call_start(session.userdata, call_start)")
	if preContextAt < 0 || preCallStartAt < 0 || startAt < 0 || preContextAt >= preCallStartAt || preCallStartAt >= startAt {
		t.Fatalf("outbound pre-start order = context:%d call_start:%d start:%d", preContextAt, preCallStartAt, startAt)
	}
	if outboundParticipantAt < 0 || outboundContextAt < 0 || refreshAt < 0 || amdAt < 0 ||
		outboundParticipantAt >= outboundContextAt || outboundContextAt >= refreshAt || refreshAt >= amdAt {
		t.Fatalf("outbound full-hydration order = participant:%d context:%d refresh:%d amd:%d", outboundParticipantAt, outboundContextAt, refreshAt, amdAt)
	}
	if participantAt < 0 || guardAt < 0 || contextAt < 0 || hydrateAt < 0 || callStartAt < 0 ||
		participantAt >= guardAt || guardAt >= contextAt || contextAt >= hydrateAt || hydrateAt >= callStartAt {
		t.Fatalf("SIP setup order = participant:%d guard:%d context:%d system:%d call_start:%d", participantAt, guardAt, contextAt, hydrateAt, callStartAt)
	}
	if !strings.Contains(entrypoint, "    if not outbound_job:\n        _hydrate_call_start(session.userdata, call_start)") {
		t.Error("call_start hydration is not outside the SIP-only system-context guard")
	}
}
