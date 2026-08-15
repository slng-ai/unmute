# Quickstart: verifying this feature by hand

Every step below is something a person can run. The automated versions live in
`go test ./...` and in the `smoke` build tag; this document is the manual
counterpart, and it is what Wave B and Wave C agents are pointed at.

Build once:

```sh
make build
```

Work in a scratch directory, never inside the repository.

---

## 1. The eight silent drops are loud

### 1a. A control no agent reaches

Author a telephony LiveKit package with a valid SIP connection, a
`human_transfer` under `controls:` with an env-var destination, and an agent
whose `tools:` does **not** list it.

```sh
bin/unmute validate .
```

**Before**: exit 0, no mention of the control. **After**: exit 1, naming the
control, its file and line, and the agents it could attach to.

Repeat for `agent_transfer`, `delegate`, an unreferenced `destinations:` entry,
and an unreferenced top-level `tools:` entry. All five refuse.

Then confirm the carve-out survives: add a `models:` entry nothing references.
It stays legal, exit 0, no warning.

### 1b. A transfer with no route

Author a package whose only channel is `web: realtime_audio`, LiveKit target, no
connection, `human_transfer` **attached** to the agent.

```sh
bin/unmute validate .
```

**Before**: exit 0, and `grep -c to_human build/livekit/agent.py` returns 2 after
a compile. **After**: exit 1 with the cold-transfer message from
[contracts/messages.md](./contracts/messages.md).

Switch the target to `pipecat` and re-run. It must print exactly one error, in
Pipecat's own words, not two.

### 1c. The secrets rule with no block

Take any package that writes environment names and delete its `secrets:` block
entirely.

```sh
bin/unmute validate . 2>warnings.txt; echo $?
```

**Before**: exit 0 and an empty `warnings.txt`. **After**: exit 0 and a warning
naming every missing name with its source file and key.

Then the scaffold case, which is the one that matters:

```sh
bin/unmute init hello && cd hello && bin/unmute validate .
```

The package declares `OPENAI_API_KEY` and `SLNG_API_KEY`, so this is silent —
and deleting the block makes it warn about both. That is the check working on a
package a new user actually has.

### 1d. A Pipecat container with local tools

```sh
bin/unmute compile examples/salon-support
cd examples/salon-support/build/pipecat
docker build -t unmute-check .
docker run --rm unmute-check python -c "import bot"
```

**Before**: `ModuleNotFoundError: No module named 'tools'`. **After**: it
imports.

If `docker build` stalls, check `docker ps` for stale `unmute dev` containers
first. They are ephemeral and `unmute dev` recreates them.

### 1e. The eight value checks

```sh
# turn detector, the reported one
bin/unmute validate .   # with turn: {detector: {provider: local, model: silero}}
```

**Before**: exit 0, then `compile` fails. **After**: `validate` fails with the
same message, target-prefixed.

Repeat for `sdk_language: node`, a bogus `pins` key, a bogus `pins` version, a
`pins` version below the floor, `version: "banana"`, `version: "9.9.9"`, and a
`deepgram` speak model. All eight fail at `validate`.

---

## 2. The first five minutes

```sh
bin/unmute init hello-agent
cd hello-agent
```

Read every file as a first-time user. Check by hand:

- `instructions.md` and the greeting do not mention a phone call, and the only
  channel is `web`.
- `.env.example` names exactly `OPENAI_API_KEY` and `SLNG_API_KEY`.
- `agent.yaml` has a `secrets:` block listing those two.
- The think model reads `provider: openai` and `model: gpt-5.6-luna` as two
  fields, never one combined string, carries
  `params: {reasoning_effort: minimal}`, and carries no `temperature`.
- Every comment describes something the file contains.
- The command in `.env.example` line 2 works when run from where you are.

Then check the sweep is complete, across the whole tree:

```sh
git grep -n "gpt-4o-mini\|gpt-4\.1-mini" -- examples docs docs-site README.md .env.example internal/scaffold internal/skill
git grep -n "openai/gpt-5\.6-luna"
```

Both empty. The only surviving hits anywhere are test fixtures, goldens, a
comment in `internal/target/catalog_livekit.go`, and the two `specs/` trees that
record the drift as history.

Then compile and compare:

```sh
bin/unmute compile .
diff <(grep -o '^[A-Z_]*=' .env.example) <(grep -o '^[A-Z_]*=' build/pipecat/.env.example)
```

Empty diff.

Then the thing this whole feature is for. With only `OPENAI_API_KEY` and
`SLNG_API_KEY` set:

```sh
bin/unmute dev .
```

Open the URL, allow the microphone, hear the greeting, and **have one real
spoken exchange after it**. A probe reaching a socket is not this step.

This is also where `reasoning_effort: minimal` is judged. `gpt-5.6-luna` is a
reasoning-family model, so listen for a pause before each reply. If it stalls,
drop to `none` and record the change. If the provider rejects the parameter
outright it fails here rather than at compile, because `params:` is forwarded
verbatim.

---

## 3. No Unmute name looks like a credential

```sh
bin/unmute compile examples/livekit-human-transfer
cat examples/livekit-human-transfer/build/livekit/.env.example
```

**Eight names, no comment block.** Before this feature it is twelve, with a
four-line "supplied for you, not by you" block holding `LIVEKIT_API_KEY`,
`LIVEKIT_API_SECRET`, `LIVEKIT_URL`, and `REDIS_URL`. After, those four are
absent, not relabelled. The eight that remain are the ones the author actually
goes and gets: two model provider keys, four connection values from their
carrier, and two phone numbers they declared as destinations.

Compile the same package for Pipecat and confirm `REDIS_URL` is gone from that
file too. Before this feature the two targets disagree about the same variable
from the same data.

```sh
bin/unmute compile examples/outbound-reminder
grep -n UNMUTE_ examples/outbound-reminder/build/*/.env.example
```

**Zero hits.** The emitted README's "set these before running" list names
exactly the same set as the env file.

Then confirm hiding did not delete:

```sh
grep -o '"required_env".*' examples/outbound-reminder/build/livekit/compile-report.json
```

Still names every hidden variable, so an operator deploying by hand can recover
them.

Then the naming rule, as a grep:

```sh
git grep -oh "UNMUTE_[A-Z_]*" -- internal | sort -u | grep -E "DAILY|LIVEKIT|TWILIO|TELNYX|PLIVO|OPENAI|SLNG"
```

Empty. No variable that configures a vendor's component wears Unmute's prefix.
`UNMUTE_DAILY_ROOM_GEO` and the three `UNMUTE_LIVEKIT_*` mappings are gone.

```sh
grep -rn "UNMUTE_" docs-site/index.mdx docs-site/start docs-site/build README.md internal/skill/assets internal/scaffold
```

Zero hits. This already holds today; the test is what keeps it holding.

---

## 4. Every example

For each of the eleven:

```sh
bin/unmute validate examples/<name>
bin/unmute compile examples/<name>
```

Both clean, for every target the package declares. Browser-only examples then:

```sh
bin/unmute dev examples/<name>
```

reaches a greeting. Telephony examples: the generated container builds and
imports.

Read each `README.md` and confirm it is true after the fixes, names every
`transport` its targets declare, and that every link out resolves.

No example may require a Langfuse account to run. Exactly one demonstrates
tracing, and `examples/README.md` says which.

---

## 5. The documented rules

Pick a validation error you can trigger and find the rule in the docs from the
field it names, without reading Go. Specifically:

- a telephony channel with no `capacity.peak_starts_per_second`
- a warm transfer on a channel with `outbound: false`

Both rules must be stated next to the fields they constrain, in `docs-site/` and
`docs/`.

---

## 6. The gate

Green on every commit:

```sh
make fmt && make lint && make build && make test
```

And, opt-in, needing Python and Docker:

```sh
make smoke
```

If it fails, name the blocking port and what held it before blaming the branch.
