//go:build smoke

package generate

import (
	"strings"
	"testing"
)

// Exercise the emitted prebuilt tool through the pinned SDK, including its
// real model follow-up, TTS drain and session shutdown. The existing streaming
// harness supplies controlled providers; only room/job I/O is replaced here.
func TestSmokeLiveKitEndCallDrainsOneGoodbye(t *testing.T) {
	harness, _, ok := strings.Cut(devStreamingLiveKitScript, "async def main(capture):")
	if !ok {
		t.Fatal("LiveKit streaming harness has no main entry point")
	}
	checkStreamingOutput(t, runLiveKitSmokeScript(t, "remy", nil, addBuiltinEndCall, harness+livekitEndCallScript))
}

const livekitEndCallScript = `
async def check_end_call(capture, *, enabled):
    from livekit.agents.beta.tools import end_call as native_end_call

    os.environ["UNMUTE_DEV_METRICS"] = "1" if enabled else "0"
    call_id = "livekit-end-call-enabled" if enabled else "livekit-end-call-disabled"
    goodbye = "You're very welcome. Have a lovely day, and goodbye!"
    model = ToolModel([
        Response([llm.ChatChunk(id="end-request", delta=llm.ChoiceDelta(role="assistant", tool_calls=[
            llm.FunctionToolCall(call_id="end-call", name="end_call", arguments="{}")]))]),
        Response([llm.ChatChunk(id="goodbye-request", delta=llm.ChoiceDelta(role="assistant", content=goodbye))]),
    ])
    speaker = Speaker()
    closed, shutdowns, cleanup, deleted, messages, playback = [], [], [], [], [], []

    async def delete_room():
        deleted.append(True)

    # Keep the actual EndCallTool. Replace only the worker's external room/job
    # endpoint: no LiveKit Cloud connection or real room deletion is needed.
    job = SimpleNamespace(
        shutdown=lambda *, reason: shutdowns.append(reason),
        add_shutdown_callback=cleanup.append,
        delete_room=delete_room,
    )
    original_context = native_end_call.get_job_context
    native_end_call.get_job_context = lambda: job
    try:
        async with make_session(model, speaker) as session:
            output = session.output.audio
            output.on("playback_finished", playback.append)
            dev_metrics.install_dev_metrics(session, call_id=call_id)
            session.on("conversation_item_added", lambda event: messages.append(event.item)
                       if isinstance(event.item, llm.ChatMessage) else None)
            session.on("close", lambda event: closed.append((event, output.frames, len(playback))))
            await session.start(QuietGreeter(initial=True))
            handle = session.generate_reply(user_input="Awesome, thank you very much.")
            await asyncio.wait_for(speaker.started.wait(), 3)
            assert model.calls == 2, "end_call requested more than one goodbye"
            assert not handle.done() and not closed and not shutdowns and output.frames == 0
            if enabled:
                await until(lambda: capture.has_text(call_id, "assistant", goodbye, final=True), "Goodbye did not stream")
                assert len(capture.messages(call_id, "assistant")) == 1
            speaker.release.set()
            await until(lambda: bool(closed) and bool(shutdowns), "end_call left the session open after audio drained")
            assert handle.done() and not handle.interrupted and handle.exception() is None
            assert len(closed) == 1 and closed[0][0].error is None
            assert closed[0][1] > 0 and closed[0][2] == 1 and not playback[0].interrupted, "Session closed before goodbye audio finished"
            assert model.calls == 2, "A second goodbye was generated after end_call"
            assert [item.text_content for item in messages if item.role == "assistant"] == [goodbye]
            assert shutdowns == [closed[0][0].reason.value] and len(cleanup) == 1
            await cleanup[0]()
            assert deleted == [True], "Native end_call did not request room deletion"
            if enabled:
                call = capture.latest(call_id, "call")[0]["call"]
                assert call["state"] == "ended"
                tools = [r["operation"] for r in capture.latest(call_id, "operation") if r["operation"]["type"] == "tool"]
                assert len(tools) == 1 and tools[0]["name"] == "end_call" and tools[0]["state"] == "returned"
            else:
                assert not capture.records(call_id), "Disabled metrics emitted records"
    finally:
        native_end_call.get_job_context = original_context


async def main(capture):
    assert version("livekit-agents") == "1.6.10"
    await check_end_call(capture, enabled=True)
    await check_end_call(capture, enabled=False)


captured = Capture()
try:
    with redirect_stdout(captured):
        asyncio.run(main(captured))
finally:
    sys.stdout.write(captured.getvalue())
print("LiveKit end_call: one goodbye plays before session/job shutdown, metrics enabled and disabled")
`
