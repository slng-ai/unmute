# Unmute

Unmute is SLNG's portable voice-agent framework: define the agent once as a directory of declarative YAML, **compile** it to any **Orchestrator**, deploy it to any **Region** on any **Runtime**, and move it wherever you want. The portability is the product: one plain-YAML spec that makes every orchestration layer easy to write, read, and swap. SLNG's **Execution Layer** is the default model route — `unmute init` scaffolds SLNG credentials and routes unless asked otherwise — and a binding can name any provider the framework itself integrates; the provider catalogue maps those. Inspired by Vercel Eve's directory conventions, with voice-native primitives on top.

## Language

### The agent

**Agent**:
A voice agent, authored as a directory of declarative files (the spec). The directory *is* the agent. Compiles to a Framework project and deploys to a Runtime. The file-based form of SLNG's existing Voice Agent entity.
_Avoid_: bot, assistant

**Agent identity**:
The canonical authored name of an Agent, stored in project metadata and used for generated config-plane artifact names. Directory names may be convenient handles, but they are not the identity when metadata is present.
_Avoid_: directory basename, generated filename

**Prompt**:
The agent's system prompt, authored as a directory of single-purpose Markdown **fragments** in `agent/prompt/` and composed in a fixed canonical order at compile. Replaces the single `instructions.md` (ADR-0004). Structure follows the `voice-agent-prompting` guideline, treated as binding.
_Avoid_: instructions (the old single-file name), system message.

**Prompt fragment**:
One Markdown file in `agent/prompt/`, owning exactly one section of the system prompt (`identity`, `output-rules`, `personality`, `goals`, `guardrails`, `user-information`). Eve's filename-is-identity applied to prompt sections: discovered and composed at compile, never wired or registered. Compile order is fixed by canonical filename, not by a manifest.
_Avoid_: section, part, block.

**Channel**:
Where an agent surfaces to reach a caller — SIP/telephony or WebRTC web session. An agent needs at least one.
_Avoid_: transport (the lower-level mechanism a channel uses)

**SIP trunk**:
A telephony connection that lets an agent receive or place PSTN calls through a SIP provider. In Unmute authoring, the Channel declares the call surface and the Connection holds provider auth/config.
_Avoid_: phone number, SIP channel, transport

**Agent language**:
The primary spoken language for the agent's conversation defaults, prompts, fillers, and model-route examples. It is an agent-level authoring choice, while per-provider language knobs remain Bridge config.
_Avoid_: provider language, locale

**Idle nudge**:
A voice-runtime behavior that prompts a silent caller before ending a stalled session. It belongs to conversation behavior, not prompt content or model routing.
_Avoid_: timeout, reminder

**Tool**:
A function the model can call, declared one-per-file in `tools/` (filename = tool name, Eve-style). The file holds the declaration (description + JSON-Schema params); the **handler** is a reference — an HTTP/MCP endpoint, or an inline Python file. An inline handler is a **framework-neutral function** (no orchestrator imports), portable across Python orchestrators (Pipecat, LiveKit) because the compiler generates the per-framework adapter; webhook-only targets such as SLNG need the HTTP/MCP form instead.
_Avoid_: function (ambiguous), action

**Connection**:
An external service reference used by channels, tools, or model-facing capabilities for auth and provider config. Connections live in `connections/*.yaml` and contain environment variable names, not secret values. Each telephony target binds one Connection to its selected carrier route; a package may declare many targets and Connections.
_Avoid_: secret value, environment variable, handler

**Telephony route**:
The exact Orchestrator, transport, and carrier path used for one target, such
as Pipecat carrier WebSocket with Twilio or LiveKit SIP with Telnyx. Direction,
controls, variable sources, and limits resolve against this complete route. One
target emits one route; multiple carrier routes use multiple named targets and
produce separate artifacts.
_Avoid_: carrier capability, provider-wide support

**Call-start rate**:
The peak number of new calls admitted per second. It is separate from active
session concurrency: a deployment may support many steady calls but still be
unable to initialize a burst of new Agent sessions.
_Avoid_: concurrency, requests per second

**Coordination mode**:
The state boundary required by a telephony route. Every v1 LiveKit or Pipecat
route resolves `shared`; the resolved plan also says why and which declared
service consumes Redis. Pipecat uses Redis only for bounded telephony
correlation, idempotency, human-transfer, and admission records. LiveKit Server
and LiveKit SIP use Redis for their distributed control plane. Media, prompts,
transcripts, tasks, and agent handoff remain in the active worker.
_Avoid_: cache, sticky sessions

**Variable**:
A named `{{variable}}` placeholder, written with no interior spaces, resolved at runtime for a call or web session. Variables live in `agent/variables.yaml`, not in `env/config.yaml`; `env/config.yaml` is static non-secret deployment config.
_Avoid_: config value, env var (unless it is actually environment-backed)

**User variable**:
A variable supplied by an agent author default or by dispatcher/caller arguments, such as `patient_name`, `practice_name`, or `case_id`. User variables personalize prompts, greetings, and runtime-safe tool fields.
_Avoid_: template default (implementation field), argument (API transport detail)

**System variable**:
A variable supplied by the runtime, not by the dispatcher, such as `call_id`, `phone_number`, `transcript_messages`, or current date/time context. System variables describe session context and can feed system-triggered tools.
_Avoid_: user variable, env var

**System variable source**:
The runtime context kind that supplies a System variable's value, such as `call_id`, `phone_number`, `transcript_messages`, or `current_datetime`. It names session context, not a secret source or config source.
_Avoid_: env source, config source, provider source

### The two deploy axes

**Orchestrator**:
The voice pipeline engine an agent compiles to and whose turn loop runs it — Pipecat, LiveKit, Daily, etc. The *compile target*, chosen per deployment because clients are opinionated. SLNG ships a plugin for each (`pipecat-slng`, `livekit-plugins-slng`), so an orchestrator's STT/TTS flows through the Execution Layer by default; a binding may instead name any provider the orchestrator integrates, resolved through the provider catalogue (2026-07-15). Orchestrators differ in *pipeline mechanics* (VAD, turn-taking, tool API, transport); the model layer is a binding choice.
_Avoid_: framework (when you mean the compile target), engine, runtime

**Compile**:
The act of generating an orchestrator-native artifact (LiveKit YAML, Pipecat config, …) from the agent spec. The spec is the durable thing; the orchestrator is a swappable compile argument. Same artifact runs on SLNG's fleet or the customer's infra.
_Avoid_: build, eject (eject implies leaving; here the compile target is just chosen)

**Target profile**:
An orchestrator-specific profile that lives beside the Agent and records deploy/runtime knobs for one compile target, such as image names, secret-set references, local env-file choice, and Kubernetes defaults. It is agent-owned configuration, but not part of the portable Agent spec.
_Avoid_: spec field, deploy flag, project target

**Generated artifact set**:
The orchestrator-native files produced by Compile for a Target profile. The compiler owns the generated set and may rewrite it deterministically; agent authors own the source Agent and target profiles.
_Avoid_: hand-authored target code, runtime host, interpreter

**Runtime**:
The infrastructure an agent is deployed onto — SLNG's managed fleet (the default we push) or the customer's own infra. Independent of which Orchestrator it compiled to.
_Avoid_: using "runtime" to mean the Orchestrator or the in-container host process. Synonym: Infrastructure.

**Config plane**:
The agent's declarative settings that a live managed agent exposes over the Voice Agents API — prompt, model routing, overrides, tools, channels. Reconciled by `apply`. Distinct from the runtime plane (where and how the agent process runs), which `deploy` owns (ADR-0006).
_Avoid_: settings, spec (the spec is the durable source; the config plane is its live projection)

**Voice Agents API payload**:
A local request body rendered from an Agent spec for creating or reconciling an SLNG-managed Voice Agent. It is a generated config-plane artifact, not a network operation.
_Avoid_: orchestrator artifact, deploy payload, live agent

**Apply**:
Creating or reconciling a managed agent's Config plane to the local Agent spec via the Voice Agents API. The first apply may create the managed Voice Agent; later applies reconcile desired (local) config against live (remote) config. Does not touch the runtime plane and is not a local version bump — git already tracks the source (ADR-0006).
_Avoid_: deploy (runtime plane), push, sync, upload, version, checkpoint

**Deploy**:
Provisioning or moving the runtime plane — standing up the agent process on a Runtime/Region (SLNG fleet or customer infra). Heavy and infrequent, distinct from the per-tweak `apply` (ADR-0006).
_Avoid_: apply (config plane), ship, release

### What SLNG owns (underneath)

**Execution Layer**:
SLNG's owned middleware between frameworks and model providers: STT routing, tiered decisioning (full-LLM vs cheap path vs cached turn), output assembly / TTS caching, adaptive execution, regions, BYOK. The source of SLNG's cost and latency advantage. Underneath every SLNG-routed model call regardless of Framework or Runtime (the default route; not the only bindable one, 2026-07-15).

**Gateway**:
The single unified endpoint fronting every STT/TTS provider, with `provider/model:variant` path routing. Its STT/TTS protocol is the **Unmute bridge** (hence the CLI's name). BYOK and region pinning are request-header concerns here.
_Avoid_: proxy

**Region**:
In-jurisdiction execution zone (e.g. `eu-central`, `us-east`, `ap-south`). Controls both *where* an agent session runs and *how* its model requests route. One region per agent.

**Compliance posture**:
The agent's declared Region boundary for routing. In v1, this is only the agent region.
_Avoid_: compliance folder, deploy target

**Provider** vs **Model**:
A **Provider** is an upstream vendor (Deepgram, Cartesia, ElevenLabs, …). A **Model** is one of its offerings, addressed as `provider/model:variant`. The agent spec references models; the Gateway resolves them.

**SLNG-hosted model**:
A Model hosted by SLNG and addressed with the `slng/` prefix, such as `slng/deepgram/nova:3-en` or `slng/rime/arcana:3-en`. Prefer SLNG-hosted STT/TTS routes when available; proxied provider routes remain valid fallbacks.
_Avoid_: self-hosted (means customer-operated infra), hosted model (ambiguous)

**Model route**:
A portable address for a Model through the Gateway or Voice Agents API, usually `provider/model:variant` or `slng/provider/model:variant`. It identifies the desired model path but does not prove the live catalog currently supports it.
_Avoid_: model ID (too validation-heavy), provider config

**Model fallback**:
An alternate Model route, and any modality-required companion fields, used when the primary route fails or times out. Fallbacks belong to Model routing config, not local catalog validation.
_Avoid_: backup provider, retry config

**Bridge config**:
Optional per-modality request knobs sent with a Model route, such as language, sample rate, encoding, or TTS speed when the route supports it. Absence means the Execution Layer/provider default applies.
_Avoid_: model choice, provider config, runtime config

**Model routing config**:
The agent's declared STT, LLM, and TTS Model routes plus optional Bridge config and fallbacks; TTS also carries a required voice. It describes what should be routed through the Execution Layer; exact live catalog membership is resolved by SLNG at deploy/runtime, not by local authoring.
_Avoid_: model catalog, validation, provider config

### Quality & testing

**CVV (Core Voice Vitals)**:
The production health-and-quality signal for a deployed agent — per-call voice/conversation vitals and scoring. Compliance rejections and runtime failures surface here; a CVV-flagged failure can trigger the Swarm's reproduce mode.
_Avoid_: metrics (too generic), telemetry

**Swarm**:
The eval system: simulated callers (**personas**) drive **scenarios** against an agent and are graded by a scoring rubric. Runs pre-deploy, scheduled, on-demand, or to reproduce a CVV-flagged production failure.
_Avoid_: tests (the Swarm is live scored calls, not unit tests)
