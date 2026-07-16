# Provider catalogue

Status: design accepted, pilot landed (Pipecat + LiveKit), 2026-07-15.
Scope: how (framework × role × provider) selections become injected code: imports, dependencies, service instantiation. Companion to [SCHEMA.md](./SCHEMA.md) (which owns what a binding *is*) and the driver specs (which own each lowering). Where this file and SCHEMA.md disagree, SCHEMA.md wins.

Decisions carried in from review (2026-07-15):

1. **The catalogue is a universal map of the platforms.** SLNG is the starting point of every generation (the init scaffold binds it), never the only route. If a user rebinds STT or TTS to another provider the framework already integrates, the driver emits that integration. driver-livekit C8/V11 were amended accordingly.
2. **No tier field.** The official-vs-community distinction is not modeled; what matters operationally is the install path, which each entry states explicitly. The scalability goal it served is met differently: anyone can add an entry for an integration that already exists upstream (see "Extending" below).
3. **Python or JSON only.** No per-language dimension on entries; `sdk_language` stays a capability-table concern (the LiveKit MCP gate).

## 1. The problem it solves

Provider knowledge used to live in four disconnected places: hand-written validation allowlists (`RoleProviders`), per-driver `switch binding.Provider` statements, a class-to-import map (`serviceInfo`), and per-class branches inside templates (`voice_id` vs `voice`). Adding a provider touched all four; missing one produced plausible-but-wrong output (driver-pipecat B1: `provider: slng` silently emitted `OpenAITTSService`). The LiveKit driver hard-coded a single provider.

## 2. The data model

One entry per **(framework, role, vendor)**, typed Go data in `internal/target/catalog.go`, entries one file per framework (`catalog_pipecat.go`, `catalog_livekit.go`). An entry carries:

| Field | Meaning |
|---|---|
| `Framework, Role, Vendor` | the key; `Vendor` is the canonical stored distributor/integration spelling (N8), `"*"` is a role's wildcard row |
| `Aliases` | accepted alternative spellings (`eleven_labs`) |
| `Distributes` | provider brands routed by an aggregate distributor such as SLNG; empty means the integration distributes its own brand |
| `Verified, Docs` | date last checked against upstream docs/source, and where |
| `Install` | exactly one of `Extra` (rides the framework pin: `pipecat-ai[deepgram]`, `livekit-agents[deepgram]`) or `Package`+`Constraint` (standalone: `pipecat-slng>=0.4.0`); both empty = ships with the framework |
| `Import` | the full import line; empty = covered by the driver's core imports (LiveKit Inference) |
| `Call` | the constructor shape (below); nil = managed-target matrix row, no code |
| `RequiresEndpoint` | wildcard rows only: an unknown vendor is legal only as a genuinely custom OpenAI-compatible endpoint (`endpoint_env` set). The structural fix for B1. |
| `Notes` | quirks worth knowing (surfaced in docs, not in every report) |

`Call` maps binding fields to constructor arguments:

- `Class`, `APIKeyArg` + `APIKeyEnv` (empty env = the `<VENDOR>_API_KEY` convention, used by wildcard rows only),
- `Model`/`Voice`/`Endpoint` as `FieldSpec{Arg, Required, Form}`. A zero FieldSpec means the binding field has no slot here and a non-empty value is a hard error (SCHEMA.md 6.2 rule 5), never silently dropped.
- `Model.Form` names a transform: `verbatim`, `slng_route` (strip the `slng/` prefix; the LiveKit plugin takes the bare vendor/model route), `provider_slash_model` (LiveKit Inference). Each form is a few lines of shared Go; new forms only with a real second user.
- `Params` style: `kwargs` (flat), `settings` (model/voice/params nest in `Class.Settings(...)`, the current Pipecat official-service shape), `extra_kwargs` (one dict, LiveKit Inference).

**What the catalogue never contains:** model names, voice ids, or param schemas. D10 stands: identities and `params` forward verbatim, unvalidated. An entry is a code slot, not an allowlist. Known provider brands that ride inside an aggregate route (`slng/deepgram/nova:3`) are recorded only to let the wizard present a provider once and then ask for its distributor; the route and model identity remain forwarded text.

## 3. Resolution and injection

`internal/generate/service_call.go` resolves one binding:

```
Lookup(framework, role, vendor) → Entry     (exact or alias, else wildcard, else no-slot error)
Entry + Binding → ServiceCall{Class, Args, SettingsArgs}   (each value already a Python expression)
```

Templates render any service through one define (`svc`): Pipecat's multi-line builder form with the optional nested `Settings(...)` block, LiveKit's inline session-kwarg form. The per-class template branches are gone. Imports, extras, standalone deps, and required env all collect off the used entries, so an emitted class structurally cannot lose its import or its install; on LiveKit, plugin imports merge into one `from livekit.plugins import ...` line and extras merge onto the `livekit-agents[...]` pin.

Error messages quote the matrix (D3):

```
pipecat reason binding provider "slng" has no slot; reason providers on pipecat: openai
("slng" provides listen, speak on pipecat)

livekit listen binding provider "acme" has no slot; listen providers on livekit: deepgram, slng
```

Every used entry lands in the compile report next to the forwarded bindings:

```
listen: deepgram via DeepgramSTTService (pipecat-ai[deepgram], verified 2026-07-15)
speak: slng via SlngTTSService (pipecat-slng>=0.4.0, verified 2026-07-15)
```

## 4. The worked pair: SLNG STT + TTS on both code targets

Same binding vocabulary, per-framework facts:

| | Pipecat | LiveKit |
|---|---|---|
| install | `pipecat-slng>=0.4.0` (standalone package) | `livekit-plugins-slng>=1.6.1` (standalone package) |
| import | `from pipecat_slng import SlngSTTService, SlngTTSService` | `from livekit.plugins import slng` |
| model form | verbatim `slng/<vendor>/<model>` | `slng_route` (prefix stripped, bare `deepgram/nova:3`) |
| call | `SlngTTSService(api_key=..., voice=..., model=...)`, flat kwargs | `slng.TTS(api_key=..., voice=..., model=...)`, flat kwargs |
| endpoint_env | no slot: hard error | no slot: hard error |
| env | `SLNG_API_KEY` | `SLNG_API_KEY` |

And the same vendor diverging across frameworks, which is why entries are per-(framework × role × vendor): ElevenLabs speak is `Settings(voice=...)` + `ELEVENLABS_API_KEY` on Pipecat, but flat `voice_id=` + `ELEVEN_API_KEY` on LiveKit.

`TestSlngEverywhere` (internal/target) encodes the hard requirement: every code target with open listen/speak must carry slng entries for them; dropping one is a red build.

## 5. Extending

User-facing configuration (what to write in targets.yaml per framework, env vars, what gets installed) lives in [docs/user/reference/providers.md](docs/user/reference/providers.md). This section is the implementation side.

### 5.1 The in-repo recipe (curated path)

Adding an integration that already exists upstream in the framework is one entry plus a golden refresh. The AssemblyAI pilot entry is the worked example; the steps, concretely:

1. **Verify the upstream surface** (class, import path, install extra or package, constructor kwargs, key env) against the framework's current docs or source, never from memory. Note whether model/voice/params are flat kwargs or nest in `Class.Settings(...)`.
2. **Append one entry literal** to `internal/target/catalog_<framework>.go` with the `Verified` date and `Docs` URL:

   ```go
   {
       Framework: Pipecat, Role: Listen, Vendor: "assemblyai",
       Verified: "2026-07-15", Docs: "https://docs.pipecat.ai/.../stt/assemblyai",
       Install: InstallSpec{Extra: "assemblyai"},
       Import:  "from pipecat.services.assemblyai.stt import AssemblyAISTTService",
       Call: &CallSpec{
           Class: "AssemblyAISTTService", APIKeyArg: "api_key", APIKeyEnv: "ASSEMBLYAI_API_KEY",
           Model:  FieldSpec{Arg: "model", Required: true},
           Params: ParamsSettings,
       },
   },
   ```

3. **Refresh the resolution golden**: `go test ./internal/generate -run TestCatalogResolutionGolden -update-catalog`, then eyeball the new block in `testdata/golden/catalog_resolution.txt` (class, args, settings placement, import, install, env). The golden iterates the catalogue, so a new entry cannot dodge coverage.
4. **Run the invariants**: `go test ./internal/target` (exactly-one install, import provides the class, dated verification, alias collisions).
5. **Extend a smoke fixture** if the entry introduces a new constructor shape: bind it in `pipecat_v1_smoke_test.go`'s multi-vendor variant so `make smoke` instantiates it against the real installed package.
6. Nothing else: no driver code, no template edits, no validation edits. Validation picks the new vendor up automatically (`Catalog.CheckVendor` reads the same entries).

A managed-target allowlist row is the degenerate form: the same entry with `Call: nil` (see `catalog_deepgram.go`), which feeds validation and the matrix without emitting code.

### 5.2 Overlay (planned)

An optional `providers.yaml` next to `targets.yaml`, same shape as the entries, parsed with `goccy/go-yaml` and validated against a JSON schema derived from the Entry struct (`jsonschema-go`), merged add-only (colliding with a built-in key is an error). Report marks those entries user-supplied. This is how a third party ships an integration without waiting for an unmute release.

Unmute never hosts integration code either way (ADR-0002): entries describe upstream packages, they do not implement them.

## 6. Testing and drift

- **L1**: `TestCatalogInvariants` (exactly-one install, import provides the class, no duplicate keys, dated verification, wildcard rules) + `TestSlngEverywhere` + `TestCheckVendor` (the shared vendor/endpoint rulebook) + fail-closed lookups.
- **L3**: per-driver goldens, plus `TestCatalogResolutionGolden`: every entry rendered through the real resolver into `testdata/golden/catalog_resolution.txt` (class, args, settings, import, install, env, one block per entry). It iterates the catalogue, so a new entry automatically demands golden coverage; `-update-catalog` refreshes.
- **L4 smoke** (`make smoke`, opt-in): upgraded from `py_compile` to **import-and-instantiate**. Two emitted projects (safe_core; a multi-vendor variant with assemblyai/elevenlabs/cartesia) get their pyproject resolved by uv into real venvs, `bot.py` is imported, and every `build_*` service builder is called, so constructor kwargs are checked against the installed packages. This is the tripwire F2 proved was missing; both projects pass (about 35 seconds with a warm uv cache).
- **Doc drift**: `Verified` + `Docs` per entry make staleness greppable; refresh on a periodic pass (the `.firecrawl/` doc-watching habit covers this). No scraper in the CLI.

## 7. Pilot findings (2026-07-15)

Pilot scope: catalogue package + invariants; Pipecat driver and template rewired (switches, `serviceInfo`, and the `voice_id` template branch deleted); LiveKit driver opened from SLNG-only to catalogue-driven (guards deleted, dynamic imports/deps); AssemblyAI (Pipecat listen) and Deepgram/ElevenLabs/Cartesia (LiveKit) added; specs amended (driver-livekit C8/V11/T11, driver-pipecat C10/V11/T9/B6). `go test ./...`, `golangci-lint`, and `make smoke` green.

- **F1: the mechanism holds.** Both drivers now resolve every listen/speak/reason binding through one lookup; one generic template block renders every service. The three-vendor LiveKit artifact (Deepgram STT + ElevenLabs TTS + Cartesia TTS override) emits correct constructors, one merged plugin import line, `livekit-agents[cartesia,deepgram,elevenlabs]>=1.5`, and the right env set, with no slng dep pulled in (`TestLiveKitV1MultiVendor`). Swapping any one binding back to SLNG is a one-line targets.yaml change.
- **F2: encoding constructor shapes as data caught real API drift** (now driver-pipecat B6). Pipecat deprecated flat model/voice kwargs in v0.0.105; deepgram/elevenlabs/cartesia/assemblyai/openai services all take `Class.Settings(...)` now. The old emitter used the deprecated flat form on every official STT/TTS service, and `py_compile` smoke could never notice. Forced to fill in a `ParamsStyle` per entry, the question "flat or settings?" got asked and answered per service against live docs. Goldens changed deliberately: `DeepgramSTTService(model=...)` became `DeepgramSTTService(settings=DeepgramSTTService.Settings(model=...))`.
- **F3: per-entry key envs are not a convention.** LiveKit's ElevenLabs plugin documents `ELEVEN_API_KEY`; the managed ElevenLabs driver uses `ELEVENLABS_API_KEY`; Pipecat's service uses `ELEVENLABS_API_KEY`. A derived `<PROVIDER>_API_KEY` convention (the old code path) ships a broken `.env.example` on LiveKit. The convention survives only for wildcard custom-endpoint rows.
- **F4: the same vendor needs different kwargs per framework** (`voice_id` flat on LiveKit vs `Settings(voice=...)` on Pipecat for ElevenLabs). Any provider-centric abstraction ("the ElevenLabs integration") would have papered over this; the (framework × role × vendor) key is the right grain.
- **F5: fail-closed wildcards close B1 for good.** An unknown vendor without `endpoint_env` now errors with the matrix quoted; with `endpoint_env` it is an explicit OpenAI-compatible endpoint with conventional key env (`TestPipecatUnknownProviderFailsClosed`, `TestLiveKitV1UnknownVendorFailsWithMatrix`).
- **F6: golden churn was small and legible.** Pipecat: the B6 Settings fix + three report notes; everything else byte-identical (the generic renderer reproduces the old whitespace exactly). LiveKit: a cosmetic voice/model kwarg-order swap plus report notes.
- **F7: pre-existing red repaired in passing.** `TestGenerateWarnOnlyReachesRemainingStubs` still listed LiveKit and ElevenLabs as unimplemented stubs; the working-tree safe_core edit (multi-vendor LiveKit bindings) had also outrun the SLNG-only driver. Both now agree with reality: the stub list is Vapi + Deepgram, and safe_core's livekit-dev target compiles by design.
- **F8: not everything moved into data, on purpose.** The Pipecat LLM builder still injects `system_instruction` (the agent prompt) into `Settings` from the driver, and the task job-workers drive the OpenAI SDK directly off raw binding fields. Entries own "how to construct the service"; templates own "where it sits in the program". The `Quirk` escape hatch stays unimplemented: zero entries needed it.

Second pass (validate-time matrix + testing hardening, same day):

- **F9: validation and generation now share one rulebook.** `Catalog.CheckVendor` (vendor known, wildcard-needs-endpoint, endpoint-has-a-slot) is called by `ir.Validate` for every listen/turn/speak/reason binding and by the drivers' `resolveService`. The hand-written `RoleProviders` map is deleted; Deepgram and ElevenLabs allowlists became call-less matrix rows (with Deepgram's own `eleven_labs`/`open_ai` spellings as aliases). A spec that validates green can no longer fail provider selection at generate time, and `unmute validate` now quotes the matrix for unknown vendors on code targets too. Leniency is deliberate: an empty vendor defers to the target default, and a role with no rows at all (Vapi) stays unrestricted per D10.
- **F10: the upgraded smoke confirms the emitted constructors against reality.** Real `pipecat-ai==1.5.0` + `pipecat-slng` installs, `bot.py` imports (which already constructs the agent workers), and every builder instantiates: Settings-style Deepgram/AssemblyAI/OpenAI/ElevenLabs/Cartesia and flat-kwargs SLNG all accepted by the installed packages. Kwarg drift now fails `make smoke` instead of a user's first run.

## 8. Next steps

Done in the second pass: validate-time matrix (F9), per-entry resolution golden, import-and-instantiate smoke (F10), and the user-facing providers reference ([docs/user/reference/providers.md](docs/user/reference/providers.md)).

1. `providers.yaml` overlay loader (section 5.2) with the struct-derived schema.
2. Surface `Vendors()` in `unmute init`'s pickers and a `unmute providers` listing.
3. Wire a LiveKit smoke equivalent (driver-livekit T8 already tracks it); the Pipecat smoke is the template.
4. Vapi/Deepgram drivers add entry files when they land; nothing else changes.
