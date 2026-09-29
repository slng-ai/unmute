# Agora Agents target

Use `provider: agora`, `version: "2.11.0"` and optionally `sdk_language: python`.
The distribution is `agora-agents`; its Python import is `agora_agent`.
Use `examples/agora-voice` as the complete package model.

Only one agent with a `realtime_audio` browser channel and cascade architecture
is supported. Bind Deepgram `nova-3` with language `en`, OpenAI `gpt-4o-mini`,
and MiniMax `speech_2_6_turbo` with an explicit voice. These use Agora-managed
credentials. The example voice is `English_captivating_female1`; availability
still needs a live call.

Require system instructions, a fixed greeting with `speaks_first: agent`, and
an explicit `conversation.interruption.enabled`. Enabled interruption uses
start-of-speech handling. `max_duration` is whole seconds from `1s` to `600s`,
with `600s` when omitted.

Do not add tools/MCP, tasks/handoffs, variables, telephony, video, live/realtime
models, model params, tracing, capacity or deployment fields. The compiler
rejects them instead of silently ignoring them. These are adapter limits.

Validate and compile the package. Run its generated Python project using the
emitted README; `unmute dev` and `unmute deploy` are unsupported for Agora.
The four user-facing surfaces (runbook, example README, public target docs and
this reference) must agree when this scope changes.

The service needs `AGORA_APP_ID`, `AGORA_APP_CERTIFICATE` and optionally
`AGORA_AREA=us|eu|ap`. Never send the certificate to the browser. Use a project
with Conversational AI enabled. The browser joins RTC before agent start.

This is a localhost, single-process demo with five active/prepared slots,
60-second unused preparation expiry, a bounded server session deadline, and a
30-second cloud departure idle timeout. Stop is idempotent and failed cleanup
is retried while the server runs. Hard process death needs the cloud fallback;
do not present it as durable production session control. Tokens are not renewed.

Before claiming the integration works live, record an actual browser conversation
with greeting, response, interruption and cleanup. Offline tests are insufficient.

The interactive starter scaffold does not create Agora packages yet. Copy
`examples/agora-voice` and edit its authored files before compiling.
