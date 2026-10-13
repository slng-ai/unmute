# Agora browser voice agent

A single assistant runs on Agora Conversational AI and speaks through an Agora
RTC browser connection. This example demonstrates the `agora` target: compile
the package, run its generated Python service, and talk to it without a local
speech pipeline. Its transport is Agora RTC, not a telephone carrier.

On this page:

- [Quickstart](#quickstart)
- [What to listen for](#what-to-listen-for)
- [Boundary](#boundary)
- [Troubleshooting](#troubleshooting)

## Quickstart

You need Python 3.12 or newer and an Agora project with Conversational AI
activated. Supply `AGORA_APP_ID` and `AGORA_APP_CERTIFICATE`; optionally set
`AGORA_AREA` to `us` (default), `eu` or `ap`. No vendor keys are required for the
managed Deepgram nova-3, OpenAI gpt-4o-mini and MiniMax speech_2_6_turbo models.
The compiler does not verify account/model/voice availability.

From the repository root:

```sh
unmute validate examples/agora-voice
unmute compile examples/agora-voice
cd examples/agora-voice/build/agora
python -m venv .venv
. .venv/bin/activate
pip install -r requirements.txt
cp .env.example .env
# Fill in Agora credentials in .env, keeping the certificate on the server.
set -a
. ./.env
set +a
python server.py
```

Open http://localhost:8080 and allow microphone access. The browser loads its
pinned Agora RTC SDK from jsDelivr. The generated `README.md` also includes a
Docker command that publishes only on loopback.

## What to listen for

The fixed greeting should play after the browser joins. Ask a short question,
then interrupt the agent while it answers. The package explicitly enables
start-of-speech interruption. End the conversation and confirm the microphone
is released and the cloud agent stops. Record a short call with sound to
verify this behavior; do not include credentials in the recording.

## Boundary

The package pins `agora-agents` 2.11.0. This initial target accepts one English
cascade agent, fixed greeting, managed model bindings, explicit interruption
and a maximum session duration of 1 to 600 seconds (600 if omitted). It rejects
tools/MCP, tasks/handoffs, variables, telephone routes, video, realtime models,
custom parameters, tracing, capacity and deployment settings. A different
MiniMax voice is allowed, but requires real-call verification.

`unmute dev` and `unmute deploy` do not run this target. Use the generated
service. It is a localhost demo with in-memory sessions, five concurrent slots,
a 60-second preparation expiry and server-side session deadlines. Stop requests
are idempotent and retried on failure. If the server is killed, only the cloud
agent's departure idle timeout remains; this is not a durable production
controller. The example does not renew tokens or extend sessions.

No live-call or recording result is asserted by this example. A successful
compile or offline SDK test does not prove a real Agora conversation worked.

## Troubleshooting

If the page cannot start, confirm the generated service is running and open
its localhost URL. A microphone error needs browser permission and a working
input device. For an Agora request failure, check the App ID, Certificate,
Conversational AI activation and managed model access. If the agent joins but
is silent, check the selected voice and the Agora session diagnostics. Do not
treat a successful start response as proof of working audio.

The interactive starter scaffold does not create Agora packages yet. Copy
`examples/agora-voice` and edit its authored files before compiling.
