package generate

import (
	"strings"
	"testing"
)

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
