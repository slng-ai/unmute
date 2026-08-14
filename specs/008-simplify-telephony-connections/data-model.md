# Phase 1 data model

Two surfaces, and the whole feature is the difference between them. The
**authoring** surface changes shape. The **resolved** surface does not.

---

## 1. Authoring surface (`internal/spec`)

### 1.1 `Target` — loses three fields

```
Provider          string              unchanged, required
Version           string              unchanged
Pins              map[string]string   unchanged
SDKLanguage       string              unchanged
Connection        string              unchanged — now the ONLY route field
DeploymentRegion  Regions             unchanged
Models            map[string]ModelDef unchanged

Transport         REMOVED  → connections/<name>.yaml
Carrier           REMOVED  → connections/<name>.yaml
Destinations      REMOVED  → agent.yaml
```

After the change a target holds **no phone number and no route mechanism of any
kind**, on every provider. It says which orchestrator runs the package, how it is
pinned, where it deploys, and which connection carries its calls.

**One knock-on in the capability table.** `vapi` and `deepgram` have no route and
no connection, and four rows condition on their `carrier`. After this change no
author can write one, so the condition could only ever produce a refusal naming an
impossible fix. All four lose the condition (FR-001a) and keep the Twilio
requirement as a comment for whoever builds that driver. Research R11 has the
measurement: stripping `carrier` from `safe_core` breaks exactly one row.

The three removed fields stay on the decode struct tagged `json:"-"` so they still
parse and can be rejected with a message naming their new home (research R7). They
are absent from the derived authoring schema, which
`authoring_surface_test.go` asserts.

### 1.2 `Connection` — loses one field, gains two

```
Transport    string             NEW, required — the mechanism
Carrier      string             NEW, required except where the route has none
Environment  map[string]string  unchanged — role name → UPPER_SNAKE env name

Kind         REMOVED — every transport in the catalog is telephony (FR-006)
```

A connection file is now readable alone. Three shapes are valid:

| Shape | Contents | Used by |
|---|---|---|
| Full | transport, carrier, environment | every credentialed route |
| No credentials | transport, carrier, **no environment** | receive-only `(pipecat, cloud-websocket, twilio)` |
| No carrier | transport only | Daily-provisioned `daily-sip` |

**Validation rules**

1. `transport` is required. A connection with no transport declares no route.
2. With a carrier: `(provider, transport, carrier)` must resolve to a
   **selectable** route — present in the table *with* a `route` feature, so
   placeholder rows are never offered (FR-011a, research R6). Without a carrier:
   the transport must be one with a carrier-less form, which today is `daily-sip`
   alone, and which has no row in the capability table (research R10). A flat
   triple lookup here breaks `pipecat-human-transfer-daily`.
3. `environment` keys must be exactly the route's vocabulary: no missing required
   key, no unaccepted key. The existing check already does both directions and
   already puts the accepted set in the message.
4. `environment` **values** must be valid shell identifiers — letters, digits,
   underscores, never leading with a digit (FR-013).
5. `environment` may be absent when the route needs no credentials.
6. Two connections may declare the same route. They are two accounts, or one
   account reached two ways.
7. A connection no target names warns and exits 0 (FR-015). It is the only check
   here that is not a failure.

### 1.3 `Agent` — gains one field

```
Destinations  map[string]string  NEW — symbolic name → UPPER_SNAKE env var name
```

**Validation rules**

1. A value must be the `UPPER_SNAKE` name of an environment variable. The literal
   E.164 and `sip:` URI forms the target accepted are **rejected** (FR-004d):
   `agent.yaml` is the portable half of a package and a literal number is a
   deployment fact.
2. Required when any `human_transfer` control is used.
3. A `human_transfer` resolves its symbolic name against this map. The model never
   sees a number, exactly as today.
4. There is no per-target override and nothing replaces one (FR-004b).

### 1.4 Relationships

```
agent.yaml
├── channels.phone ──────────── requires ──► a Connection on each target
├── destinations ◄───── resolved by ──────── controls.human_transfer
└── secrets ◄────── must list ────┐
                                  │
targets.yaml                      │ every author-written env NAME
└── target                        │
    ├── provider ──┐              │
    └── connection ┼──► ROUTE ────┤
                   │  (provider,  │
connections/*.yaml │  transport,  │
└── connection ────┘   carrier)   │
    ├── transport                 │
    ├── carrier                   │
    └── environment ──────────────┘
```

The route triple is assembled from two files and one of them is the target. That
is unchanged; what changed is that two of the three parts now travel together.

---

## 2. Resolved surface (`internal/ir`) — shape unchanged

`ir.Target` keeps `Transport`, `Carrier`, and `Destinations`. `ir.Connection`
keeps `Kind` and `Environment`. `ir.TelephonyPlan` keeps its `Key`,
`Connection`, `Environment`, and `Destinations`.

`buildTarget` changes where it reads, not what it writes:

| Resolved field | Read from, today | Read from, after |
|---|---|---|
| `Target.Transport` | `spec.Target.Transport` | `spec.Connections[name].Transport` |
| `Target.Carrier` | `spec.Target.Carrier` | `spec.Connections[name].Carrier` |
| `Target.Destinations` | `spec.Target.Destinations` | `spec.Agent.Destinations` |
| everything else | unchanged | unchanged |

`ir.Connection.Kind` has no author to read it from once `kind:` is gone. Set it to
the constant `"telephony"` at build time rather than deleting the field, so the
resolved schema and every golden that carries it stay still. Deleting it is a
resolved-surface change this feature has no reason to make.

**Consequence, and the point of the design**: both generators, all seven
`validate.go` route branches, the three `cli/dev*` route branches, and every
golden file read exactly what they read today.

---

## 3. State transitions

None. Nothing here has a lifecycle — a package is loaded, built, validated, and
generated in one pass, and the four stages are unchanged.

---

## 4. What a rejected package looks like

Full message text is in [`contracts/errors.md`](./contracts/errors.md). The
classes:

| Class | Severity | Requirement |
|---|---|---|
| Moved field on a target (`transport`, `carrier`, `destinations`) | error | FR-007 |
| `kind:` in a connection | error | FR-006 |
| Unknown key in a connection | error | FR-010 |
| Unsupported or unselectable `(transport, carrier)` for the provider | error | FR-011, FR-011a |
| Missing or unaccepted environment key | error | FR-012 |
| Environment value that is not a shell identifier | error | FR-013 |
| Target names a connection that does not exist | error | FR-014 |
| Telephony channel with no connection | error | FR-016 |
| Connection nothing uses | error | FR-016 |
| Transfer the route cannot emit | error | FR-016a |
| Literal number in `agent.yaml` destinations | error | FR-004d |
| **Connection no target names** | **warning, exit 0** | FR-015 |
| **Author-written env name missing from `secrets:`** | **warning, exit 0** | FR-005e |
