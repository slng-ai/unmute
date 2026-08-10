# SPEC — tool file shape: execution-keyed block + webhook auth schemes

Source brief: "restructure the tool compile so we pass secrets, parameters,
execution type and effect; HMAC is missing". Schema truth:
[SCHEMA.md](docs/SCHEMA.md) §5. Tool reference:
[docs/user/reference/tools.md](docs/user/reference/tools.md). Shape and scheme
set chosen by the user 2026-08-10 (execution-keyed block; `input:` is the
parameters, no new static-params field). Scheme set amended 2026-08-10: **hmac
removed at the user's direction** — `bearer` and `api_key` only.

## §G goal

One tool file says what the model sees at the top level and how the tool runs in
exactly one execution-keyed block, and a `webhook` tool can authenticate itself
two ways — `bearer` and `api_key` — with every secret named, never written.

## §C constraints

- C1: the execution kind **is** the block name. `webhook:`, `local:`, `mcp:`,
  `builtin:`, `client:`, `provider_hosted:`. The top-level `execution:` key is
  deleted. Exactly one block per file, so a field that belongs to another kind
  is unwritable, not merely rejected.
- C2: `effect` and `interruption` stay top-level scalars. They are conversation
  facts, not transport facts, they are one word each, and every existing tool
  file keeps them unchanged.
- C3: the model contract stays top-level: `description`, `input`, `output`.
  `input` **is** the parameter list (user, 2026-08-10) — no static-parameters
  field in this change.
- C4 (amended 2026-08-10): two auth schemes ship: `bearer` and `api_key`.
  **`hmac` is removed** — not gated, not dormant: no field, no lowering, no
  helper, no import, no doc row. `basic` and `oauth2` client-credentials stay
  non-goals. A caller that needs request signing writes a `local:` Python
  handler until a scheme is specified again.
- C5: secrets are environment variable names, never values. Every `*_env` field
  takes the same check, tightened to `UPPER_SNAKE` in this change so a pasted
  token or URL fails validation instead of becoming a lookup that KeyErrors at
  call time.
- C6: hard break, no compatibility shim. There is one shape in the docs and one
  in the loader. Every in-repo tool file migrates in the same change.
- C7 (retired 2026-08-10 with C4): described HMAC parameterisation. No HMAC.
- C8: no new Go dependency and no new Python dependency. Both remaining schemes
  are a dict literal over `os.environ`; nothing beyond the existing imports.
- C9: emitted Python stays `ruff check` + `ty check` clean with no dead imports
  or helpers ([driver-livekit](docs/spec/driver-livekit.md) V26,
  [driver-pipecat](docs/spec/driver-pipecat.md) V12).
- C10: L1–L3 need zero Python, zero network. Real signing and real headers are
  L4 (`make smoke`).

## §I surfaces

- I.shape: the authoring form. Model contract, one execution block,
  conversation scalars:

  ```yaml
  description: Search for a place by name, type, or area.

  input:
    type: object
    properties:
      query: { type: string, description: 'e.g. "tapas bar in Madrid"' }
    required: [query]

  webhook:
    url_env: LOOKUP_PLACES_URL
    auth:
      type: bearer
      token_env: LOOKUP_PLACES_TOKEN

  effect: returns_data
  interruption: provider_default
  ```

  Per kind: `webhook: {url_env, auth?}` · `local: {handler?}` ·
  `mcp: {url_env}` · `builtin: {id, instructions?}` ·
  `client: {}` / `provider_hosted: {}` (empty or null; both stay gated on every
  target). `mcp:` is the block that may grow its own `auth:` later without a
  second shape change — the reason the block is keyed by kind.
- I.auth (amended 2026-08-10): two scheme shapes. `type` selects; `header`
  belongs to `api_key` alone. The whole surface is three fields.

  ```yaml
  auth: { type: bearer,  token_env: PLACES_TOKEN }
  auth: { type: api_key, token_env: PLACES_API_KEY, header: X-API-Key }
  ```
- I.spec: `internal/spec` — `Tool` gains one pointer per kind
  (`Webhook`, `Local`, `MCP`, `Builtin`, `Client`, `ProviderHosted`) plus a
  `ToolAuth` struct; `Execution`, `URLEnv`, `Handler`, `Builtin`,
  `Instructions`, `Auth`, `TokenEnv` leave the top level. `spec.Load` keeps
  reading a `local` tool's handler file, now off the block.
- I.load: a top-level `execution:`, `url_env:`, `handler:`, `token_env:`, or
  `auth:` key produces a migration error naming the block form, not a bare
  goccy "unknown field" (V2). Strict decode with line/col is unchanged
  otherwise.
- I.ir: `internal/ir` stays **flat** — `Tool{Execution, URLEnv, Handler,
  Builtin, Instructions, Auth *ToolAuth, ...}`. `ir.Build` folds the authoring
  block into it. The resolved/debug schema therefore shows one tool shape, and
  the generators keep reading the fields they already read.
- I.livekit (amended 2026-08-10): `templates/livekit_v1/agent.py.tmpl` — the
  shared `webhook_tool` define keeps **one** request form, `json=<body>`, plus a
  conditional `headers=<expr>` line. Helpers `_bearer(env)` and
  `_api_key(header, env)` emit per scheme in use. `buildLiveKitTool` carries the
  auth and `env.add`s its token env.
- I.pipecat (amended 2026-08-10): `templates/pipecat_v1/bot.py.tmpl` — the same
  one form plus a conditional headers argument at all three webhook sites
  (single-agent direct function, worker `@tool` method, flows handler);
  `buildTool` carries the auth and `env.add`s the token env, so it lands in
  `.env.example` and `REQUIRED_ENV`.
- I.table: `internal/target/table.go` — one capability row `tools.auth`
  (replacing `tools.auth.bearer`): core on LiveKit and Pipecat, deny on Vapi
  and Deepgram, which configure tool auth provider-side.
- I.scaffold: `internal/scaffold/templates/tool.yaml.tmpl` writes the block
  shape; `scaffold.Tool` carries the block fields; the TUI tool editor keeps its
  current field set (URL env, handler, prebuilt id, goodbye) and reads/writes it
  through the blocks. Auth is not editable in the console in this change.
- I.docs: `docs/SCHEMA.md` §5 (rewritten to the block shape, §7 row),
  `docs/user/reference/tools.md`, `docs/user/learn/02-add-a-tool.md`,
  `docs/user/targets/{livekit,pipecat,vapi}.md`,
  `docs/user/reference/safe-core.md`, and the driver/compiler §T rows.

## §V invariants

- V1: a tool file carries **exactly one** execution block. Zero blocks and two
  or more blocks are both errors naming the file, the blocks found, and (for two
  blocks) the second block's line.
- V2: a top-level `execution:`, `url_env:`, `handler:`, `auth:`, or
  `token_env:` key fails with a migration error that names the new block form.
  The old flat shape never loads and never silently half-loads.
- V3: `description`, `input`, `output`, `effect`, and `interruption` keep their
  current position, values, defaults, and per-target tags. Migrating a tool
  file touches only the moved keys.
- V4: every secret and endpoint is an `UPPER_SNAKE` environment variable name.
  No auth field accepts a literal token, secret, password, or URL — the pattern
  is shared with `url_env`, `endpoint_env`, and connection env values.
- V5 (amended 2026-08-10): `auth` is legal only inside a `webhook` block.
  `auth.type` is `bearer` or `api_key`; each requires exactly its own fields,
  and a field belonging to the other scheme is an error. `hmac` is not a value:
  it fails as an unknown type like any other word.
- V6 (retired 2026-08-10 with C4): HMAC defaults. No HMAC.
- V7 (retired 2026-08-10 with C4): signed-bytes rule. With no signing, the
  emitted webhook call has exactly one request form again — `json=<body>` — and
  no generator path serialises a body by hand.
- V8: each auth helper emits iff some emitted tool uses that scheme (C9). A
  project with no auth tool contains no auth helper, and no project gains an
  import from this feature: both schemes are a dict over `os.environ`.
- V9: every `*_env` name in an emitted webhook block appears in
  `.env.example`, and on Pipecat also in `REQUIRED_ENV`, so a missing secret
  fails at startup rather than mid-call.
- V10: `internal/ir` carries the flat tool shape. The authoring blocks exist in
  `internal/spec` only; the resolved/debug schema shows no block wrapper.
- V11: the `tools.auth` capability row is core on the code targets and denied on
  Vapi and Deepgram, and both emitter agreement tests (`livekitEmittedFields`,
  `pipecatEmittedFields`) stay green against it.
- V12: every tool file in the repo — `internal/testdata/{safe_core,remy}`, all
  four `examples/*`, `webhook-test`, and everything `unmute init` or the create
  wizard writes — is in the block shape, and a freshly built `bin/unmute`
  validates and compiles each one.
- V13 (amended 2026-08-10): the header text is pinned at L3 — `bearer` emits
  `Authorization: Bearer <token>` and `api_key` the configured header, both
  reading `os.environ` at call time. There is no separate L4 auth smoke: with
  signing gone, a header is a literal dict entry with no wire subtlety left to
  prove, and the L4 static-check smokes already compile the emitted projects.
- V14 (amended 2026-08-10 after review): every package under `examples/` **and**
  `internal/testdata/` loads, builds, validates its declared targets with zero
  errors, and generates — enforced at L1-L3 by
  `TestPublicExamplesValidateAndGenerate` and `TestFixturePackagesValidate`,
  which need no Python. `ruff check` + `ruff format --diff` cleanliness of the
  emitted projects stays where it can actually run: the existing L4 static-check
  smokes (`make smoke`) plus the CLI's own format pass.

## §T tasks

id|status|desc|cites
T1|x|`internal/spec`: per-kind block structs + `ToolAuth`; drop the flat keys; `spec.Load` reads a local handler off the block; migration error for every moved top-level key|I.spec,I.load,I.shape,V1,V2,V3
T2|x|`internal/ir`: keep the flat `Tool`, add `Auth`; `Build` folds blocks into it; `Validate` enforces one-block, per-scheme field sets, env-name checks (hmac rules retired by T10); compiler golden regenerated|I.ir,V1,V4,V5,V10
T3|x|LiveKit lowering: the shared `webhook_tool` define plus per-scheme helpers gated by `AuthKinds`, secrets through `env.add` (the hmac request form retired by T10)|I.livekit,V8,V9
T4|x|Pipecat lowering: the same shape at all three webhook sites, same helpers gated per scheme, secrets into `.env.example` + `REQUIRED_ENV` (hmac form retired by T10)|I.pipecat,V8,V9
T5|x|capability table: `tools.auth.bearer` → `tools.auth`; both emitted-field maps and agreement tests|I.table,V11
T6|x|`internal/scaffold` tool template + `scaffold.Tool` fields + TUI tool editor read/write the blocks; scaffold goldens|I.scaffold,V3,V12
T7|x|migrate every in-repo tool file (fixtures, four examples, `webhook-test`) to the block shape; regenerate all goldens; `make build` then validate + compile each package|V12
T8|x|tests: L1 validate table (one-block, each scheme, each error), L2 migration-error text, L3 per-scheme emission incl. a no-auth project proving V8 (the L4 auth smoke retired by T10)|V1-V9,V13
T9|x|docs: SCHEMA §5 rewritten to the block shape + §7 row; `docs/user/reference/tools.md`, `learn/02-add-a-tool.md`, `targets/{livekit,pipecat,vapi}.md`, `reference/safe-core.md`; driver-livekit / driver-pipecat / compiler §T rows|I.docs,V3,V5,V6
T10|x|remove hmac end to end (C4): `ToolAuth` loses `secret_env`/`algorithm`/`encoding`/`prefix`/`timestamp_header`/`signed_payload` in spec **and** ir; `ir.ToolAuthHMAC`, `HMACAlgorithm`, `HMACEncoding`, `HMACPayload`, `DefaultSignatureHeader` deleted; `webhook_auth.go` loses the hmac arm, `SignsBody`, and `authKindSet.HMAC`; both templates lose the `content=payload` branch, the `_hmac_headers` helper, and the `base64`/`hashlib`/`hmac`/`time` imports plus their `json` conditions; the L4 auth smoke is deleted; scaffold template drops the hmac rows|C4,I.auth,I.livekit,I.pipecat,V5,V8,V13
T11|x|prove every `examples/` package: validate + compile for its declared target (four livekit, one pipecat telephony), `ruff check` + `ruff format --diff` clean on each emitted project, no stale `build/` left behind; a Go test walks the examples so a broken example fails the suite, not a human|V12,V14
T12|x|docs: strip hmac from SCHEMA §5.3 + §7, `docs/user/reference/tools.md`, `learn/02-add-a-tool.md`, `targets/{livekit,pipecat,vapi}.md`, `reference/safe-core.md`, and the driver/compiler §T rows; point signing needs at `local:` handlers|I.docs,C4,V5

Dependency order: T1 → T2 → T3, T4, T5 (parallel) → T6 → T7 → T8 → T9. T5 may
land with T3. T7 is the breaking commit: nothing validates between T1 and T7.
T10 → T11 → T12 (remove first, then prove the examples against the smaller
surface, then document what remains).

## §B bugs

id|date|cause|fix
B1|2026-08-10|`bin/unmute dev webhook-test` failed with `unknown field "token_env"` although `spec.Tool` carried the field: `bin/unmute` was built 10:59, the field landed 11:15. Strict decode was correct; the binary was stale. No code defect|No invariant. `make build` before running a spec that uses a new field; T7 verifies every package against a freshly built binary
B2|2026-08-10|`tool.yaml.tmpl` emitted `url_env:` for every non-local kind, so the console writing a `client`/`provider_hosted` tool produced `client:\n  url_env:` — `ToolNoFields` strict-decodes that as an unknown field and the package it just wrote no longer loads. The old flat template guarded the line with `[[if .URLEnv]]`; the block rewrite dropped the guard. Found by the review pass, never shipped|Template renders `client: {}` / `provider_hosted: {}` and emits `url_env` only when set. `TestMaintainToolBlocksRoundTrip` saves and reloads a package carrying webhook+auth, local, and mcp blocks; `TestScaffoldFieldlessToolBlocks` pins the two fieldless kinds. V36 (compiler.md) states the round-trip rule
B3|2026-08-10|Three loader shape holes, all found by the review pass and none shipped: a block with an empty body (`webhook:`) decoded to nil and surfaced much later as `invalid execution ""` with no file or line; the new-shape inline mapping `builtin: { id: end_call }` was rejected as the old scalar form with a nonsense hint; and a quoted key (`"webhook":`) read as no block at all|`checkToolBlockBody` runs after decode and names the file, line, and the fix (`client: {}` for the fieldless kinds); the scalar-builtin hint fires only when the value is not a flow mapping; `topLevelKeys` trims quotes. `TestLoadToolShape` covers all fourteen shapes, legal and illegal
