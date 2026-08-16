# End-to-end example harness

Use this runbook to prove each package under `examples/` can hold a real
conversation on every declared target. Start with the first package not marked
verified in [Results](#results). Run it after changes to emitted code, model
bindings, provider catalogue rows, or an example, and before a release.

This harness complements the automated checks: only a live provider request and
a human conversation can prove that the agent hears, reasons, calls tools, and
speaks a useful answer.

## Prerequisites

- Docker with the Compose plugin. Every run builds and starts the deployable
  container.
- `uv`, for any manual Python check.
- A repository root `.env` with real keys. Validate and compile need no keys.
  Only `dev` reads credentials.
- `make build` first, every time. The binary is what the harness tests, and a
  stale `bin/unmute` will happily recompile an example with yesterday's driver.

### Each example needs its own `.env`

`unmute dev` reads `.env` from the current directory first, then the package
root, and later files win. Running from inside the example directory makes both
the same file, so give every example a copy of the root one:

```sh
cd examples
for d in */; do
  [ -f "${d}agent.yaml" ] || continue
  [ -f "${d}.env" ] || cp ../.env "${d}.env"
done
```

`.env` is gitignored at any depth, so none of those copies can be committed.
Check it yourself before trusting that:

```sh
git check-ignore -v examples/mcp-example/.env
```

Some examples need more than the two model keys. `mcp-example` needs
`FIRECRAWL_MCP_URL` and `FIRECRAWL_API_KEY`, `simple-prompt` needs the three
`LANGFUSE_*` names, and the telephony examples need carrier credentials. The
root `.env.example` is the full menu, and each compiled target writes the exact
list it needs to `build/<target>/.env.example`.

**One trap worth knowing.** A variable name that starts with a digit is not a
valid shell identifier, so `export` fails on it and every later name in the file
can go missing. The old `11LABS_API_KEY` did exactly this. The current name is
`ELEVENLABS_API_KEY`. If a run reports a key as unset that you can see in the
file, look for a name that starts with a digit above it.

## The loop, per example

Run both targets at once, on separate ports, so a call on one does not block
the other and you can compare the same spoken script side by side. This is how
the differences between the two drivers become visible.

### 1. Build and check the package compiles

```sh
make build
EXAMPLE=multi-task

bin/unmute validate "examples/$EXAMPLE"
bin/unmute compile  "examples/$EXAMPLE"
```

`validate` prints one line per declared target and exits 0. Warnings go to
stderr and stay exit 0. Compile writes `build/<target>/` for each target.

Read the compile output rather than skimming it. It prints every model binding
and every forwarded param, which is where you see whether the fix you just made
actually reached the emitted code:

```sh
grep -rn "reasoning_effort" "examples/$EXAMPLE"/build/*/agent.py "examples/$EXAMPLE"/build/*/bot.py
```

### 2. Start both targets

Before starting, append the flags for the current example to **both** target
commands. These values stand in for the production call-start payload; omitting
them tests a default, not dispatch hydration.

| Example | Required start flags |
|---|---|
| `salon-support` | `--var customer_name=Ada --var customer_id=cus_2002` |
| `outbound-reminder` | `--var customer_id=cus_1042 --var name=Ada --var appointment_time="tomorrow at 3pm"` |

```sh
bin/unmute dev "examples/$EXAMPLE" --target livekit --no-open
bin/unmute dev "examples/$EXAMPLE" --target pipecat --port 8766 --bot-port 7861 --no-open
```

Run each in the background and wait for `ctrl-c to stop` to appear in its
output before handing the URLs over. LiveKit serves `http://localhost:8765/?agent=livekit`
and Pipecat serves `http://localhost:8766/?agent=pipecat`.

Skip a target the package does not declare. `livekit-human-transfer` is LiveKit
only, and the two `pipecat-human-transfer-*` packages are Pipecat only.

`outbound-reminder` is the exception to the browser loop: its acceptance is a
real outbound call. Run each target separately with `--telephony --to <E.164>`
plus its three `--var` flags, answer the call, stop that target, then repeat on
the other target. Its appointment tools are local fixtures, so it needs no
salon API values.

### 3. Watch both logs

LiveKit's log carries the self hosted media server's own debug output, which is
most of the file and none of the signal. Filter it out first:

```sh
tail -F "examples/$EXAMPLE/build/livekit/dev.log" \
  | grep -v "livekit_server-1" \
  | grep -E --line-buffered "APIStatusError|Timed out|McpError|Error in _llm|exception occurred|Traceback|LLM metrics"
```

```sh
tail -F "examples/$EXAMPLE/build/pipecat/dev.log" \
  | grep -E --line-buffered "Error calling mcp|TypeError|Error code|ErrorFrame|Traceback|Calling function"
```

`Calling function` on Pipecat proves a tool ran, with the arguments the model
chose. LiveKit Agents 1.6.10 `start` mode no longer writes tool names or
arguments at its default INFO level; its log proves LLM activity and error
absence, but not tool order. Also remember that neither target shows injected
arguments to the model: injection is deliberately applied inside the generated
handler after the model supplies only its public arguments. Use a deterministic
tool result or inspect that handler when the injected value itself is under
test.

### 4. Ask a human to make the call

**Wait for a person to speak to it.** A container that starts, a worker that
registers, and a greeting that plays prove only that the process is alive. The
reasoning bug in PR #82 passed all three of those and still failed on the first
question. Do not mark an example verified without a human call, and say plainly
which target a human actually spoke to.

Give the human the exact script from the table below, so the run exercises the
feature the example exists to demonstrate rather than whatever the conversation
drifts into.

### 5. Read the result, then count the errors

```sh
grep -v "livekit_server-1" "examples/$EXAMPLE/build/livekit/dev.log" \
  | grep -cE "APIStatusError|Error in _llm|Traceback"
grep -cE "Error code|ErrorFrame|TypeError|Traceback" "examples/$EXAMPLE/build/pipecat/dev.log"
```

If the package delegates to a task, count each delegate's invocations too. A
delegate that ran twice throws nothing and reads as a normal log, so it is
invisible unless you count:

```sh
grep -v "livekit_server-1" "examples/$EXAMPLE/build/livekit/dev.log" \
  | grep -c "check_customer"
grep -c "Calling function \[check_customer" "examples/$EXAMPLE/build/pipecat/dev.log"
```

One per spoken request. Two, with no user speech between them, is the
delegation bug in the table below.

Zero errors on both, each delegate called once, plus the expected tool calls in
order, plus a human who heard a sensible answer, is a pass. Anything else goes
to [the fixing rules](#the-fixing-rules).

### 6. Stop both, move to the next example

Stop the two `dev` processes before starting the next example, or the ports
stay taken.

## What to look for in the logs

These are failure signatures met in real runs, not invented ones.

| Signature | What it means | What to do |
|---|---|---|
| `Function tools with reasoning_effort are not supported for <model> in /v1/chat/completions. To use function tools, use /v1/responses or set reasoning_effort to 'none'` | The think model is a reasoning model, and chat completions rejects tools unless the request says `none`. Omitting the field fails too, because the server has its own default. | Add `params: {reasoning_effort: "none"}` to the think model. `minimal` is not a legal value for this model family. Legal values are `none`, `low`, `medium`, `high`, `xhigh`. |
| `mcp.shared.exceptions.McpError: Timed out while waiting for response to ClientRequest. Waited 5.0 seconds.` | The MCP request timeout. LiveKit's `MCPServerHTTP` defaults both `timeout` and `client_session_timeout_seconds` to 5, and no web search or crawl answers that fast. Pipecat's MCP client defaults to 30, so the same package behaved differently on the two targets. | Both drivers now write 30 explicitly, from one constant. If you see 5 seconds again, the emitted code lost the argument. |
| `MCP error -32602: ... sources: Invalid input: expected array, received object` | An intermittent model slip on tool arguments, seen on both targets. The schema reaching the model is correct: raw `tools/list`, LiveKit and Pipecat all send byte identical JSON, and 35 out of 35 direct API calls got the shape right. | Nothing. The agent reads the error and retries correctly. Do not chase it unless it stops recovering. |
| `TypeError: OpenAILLMSettings.__init__() got an unexpected keyword argument '<name>'` | A forwarded param landed as a Settings field on a Pipecat service whose dataclass has no such field. | The catalogue row needs `SettingsOverflow`, so the param rides `extra={...}` instead. See `internal/target/catalog_pipecat.go`. |
| A task calls `finish` with `status: "failed"` and never calls its own tools | The task did not receive a value an earlier step assigned. It is obeying its own prompt, which usually says to give up when a prerequisite is missing. `assign:` writes a variable, and the only thing that reads one back is a `{{variable}}` reference in a prompt. Write the mapping and never reference it and the value goes nowhere. | Reference the variable in the task prompt that needs it. An unset variable renders as empty, never as the word `None`, so the prompt should say what empty means rather than leave the model to guess. |
| The same delegate runs twice with no user speech in between | The owner lost the record that the first delegation completed, so the unchanged caller request looks unfinished. This has two observed causes. LiveKit can merge task turns over the owner's tool record. Pipecat 1.7.0 can restore a snapshot taken inside the delegate handler before the asynchronous owner tool call and result reach the context aggregator. The second run may fail or may repeat the caller questions and overwrite the first result. | Preserve the owner's completed tool call and result across the task boundary. LiveKit snapshots before the task and restores after it. On Pipecat, resolve the delegate with `run_llm=False`, drain that result into the owner context, and only then snapshot before the Flow rewrites the context. |

Noise that is not a bug, and that a run should not stop for:

- `failed to detect interruption ... WSServerHandshakeError: 401 ... /v1/bargein`, followed by `adaptive interruption disabled ... falling back to VAD-based interruption`. Adaptive interruption is a LiveKit Cloud inference feature, and local `dev` runs a self hosted server with a dev key. It retries three times, falls back, and the call is fine.
- `InsecureKeyLengthWarning: The HMAC key is 6 bytes long`. That is the `devkey`/`secret` pair the dev compose file ships. It never reaches a deployment.
- `main::BusBridge Trying to process LLMServiceMetadataFrame#N but StartFrame not received yet`, on Pipecat, at startup. It prints on runs that work perfectly.

Telephony setup note for this sweep: the same phone number has been used for
both the TwiML carrier-WebSocket route and the SIP route. Before testing those
rows, update the number or its routing for the route under test. Treat a call
reaching the wrong transport as setup drift until that change is made, not as
an agent behavior failure.

## The examples

Eleven packages. The three at the top were run in a real call and the scripts
are what actually worked. The other eight scripts are derived from each
package's own `agent.yaml` and `README.md` and have **not** been spoken yet:
treat them as a starting point and correct them as you go.

| Example | Targets | What it exercises | Spoken script | Pass looks like |
|---|---|---|---|---|
| `salon-support` | livekit, pipecat | Local Python tools, call start variables, secrets. The smallest package that talks. | Start both targets with `--var customer_name=Ada --var customer_id=cus_2002`. Confirm the greeting says "Hi Ada", then say "I'd like to book a haircut for tomorrow at 3pm." Answer any question it asks. | The exact personalized greeting, then `update_variables`, `check_availability`, and `book_appointment`. The booking receives `customer_id: cus_2002` through injection, and the caller hears a confirmation. Zero provider or runtime errors. |
| `mcp-example` | livekit, pipecat | A remote MCP server as a tool source, with bearer auth and a `tools_filter`. | "Is there any really good Indian restaurant in Barcelona?" | `firecrawl_search` executes, takes roughly 6 to 8 seconds, and the prompt token count jumps sharply on the next turn as results land. One malformed retry is acceptable. |
| `multi-task` | livekit, pipecat | Two delegated tasks, typed results, and `assign:` carrying a value from the first task to the second. | "I'd like to book an appointment." Give a full name, then a phone number, then agree to create the profile, then pick a time. | The chain `check_customer`, `lookup_customer`, `create_customer`, the task's finish tool, `manage_appointment`, `check_availability`, `book_appointment`. The critical detail: `book_appointment` must carry the `customer_id` the first task produced. |
| `simple-prompt` | livekit, pipecat | One agent owning five tools, plus Langfuse tracing. The only package that configures a tracing provider. | "I'd like to book an appointment." Give a new phone number and full name, agree to create the profile, ask for a haircut tomorrow, then choose 3pm. | `lookup_customer`, `create_customer`, `check_availability`, then `book_appointment`, each once, and a trace visible in Langfuse. Needs the three `LANGFUSE_*` keys. |
| `subagents` | livekit, pipecat | `agent_transfer` in both directions between `booking_desk` and `appointment_manager`, sharing one tool set. | "I want to change an appointment I already have." Give `+1 555 010 101`. Then say, "Actually, leave my existing appointment unchanged. I want a separate new haircut appointment for August eighteenth, twenty twenty-six." Choose the returned 3 p.m. slot. | A spoken cue finishes before each transfer. `lookup_customer` runs once, the handoff back does not repeat the greeting or phone question, then `check_availability` and `book_appointment` use the exact returned customer and slot IDs. Zero errors or invented results. |
| `task-groups` | livekit, pipecat | One delegate running an ordered group of three tasks with shared context: `identify_customer`, `select_appointment`, `finalize_appointment`. | Derived, unverified. "I'd like to book an appointment." Give name and phone, pick a service and a time. | `manage_appointment` opens the group, then the three steps run in order, each finishing before the next starts, and the booking carries the id from step one. It uses `context_scope: shared`, the one branch that always snapshotted and restored the owner context, so it never carried the `multi-task` delegation bug. The `isolated` branch beside it did, and has been fixed the same way. Expect this one to pass with no new work. |
| `outbound-reminder` | pipecat, livekit | A real outbound call carrying input variables from the dispatch, a system variable from the route, and a conversation variable, with three deterministic local Python outcomes. | Derived, unverified. Start each target with `--telephony --to <E.164>` and `--var customer_id=cus_1042 --var name=Ada --var appointment_time="tomorrow at 3pm"`, answer the call, then say "no, can we move it to Friday?" | The phone rings from each target, the greeting already says the name and time, `update_variables` saves `reschedule_to`, and the local `reschedule_appointment` receives `cus_1042` plus the saved Friday value. Zero provider or runtime errors. |
| `twilio-telephony-hello` | pipecat, livekit | The smallest telephony package. No tools at all, so it is the one example the reasoning bug could not break. | Derived, unverified. "Hi, how are you?" then a short exchange. | A greeting and two or three coherent turns. In the browser it proves the build; the real test is `--telephony`. |
| `livekit-human-transfer` | livekit | Cold and warm transfer to a person over SIP, with `destinations:` read from environment names. | Derived, unverified. "Can you put me through to billing?" then on a second call, "I need to speak to a supervisor." | `send_to_billing` runs a cold transfer, `escalate_to_supervisor` runs a warm one with a briefing. Needs real numbers and a trunk. |
| `pipecat-human-transfer-daily` | pipecat | Cold transfer over Daily, the one route where Pipecat has a native transfer primitive. | Derived, unverified. "Can you put me through to billing?" | `send_to_billing` fires. `dev --telephony` refuses this package by name, so run it in the browser and test the carrier path from the deployed agent. |
| `pipecat-human-transfer-twilio` | pipecat | The same salon reached through Twilio streaming straight to Pipecat Cloud. | Derived, unverified. "Can you put me through to billing?" | `send_to_billing` fires. Runs `--telephony` locally with Twilio credentials. |

The [local telephony guide](../docs-site/dev/telephony.mdx) shows which examples
can run with `dev --telephony`. Everything else runs in the browser.

## The fixing rules

When an example fails, this is the loop that worked.

1. **Find the root cause before proposing a fix.** Read the whole error,
   including the part that names the remedy. The reasoning bug's 400 said
   `use /v1/responses or set reasoning_effort to 'none'`, which was the entire
   answer, sitting in the log.

2. **Prove it outside the agent.** Reproduce against the live API with `curl`
   or a short script, or read the installed SDK source in the `uv` cache, or
   both. Never diagnose a provider or an SDK from memory. Two examples of what
   this catches: the live API confirmed `minimal` was rejected as well as
   omitted, and the installed signature showed `MCPServerHTTP` defaults its two
   timeouts to 5, not to something reasonable.

3. **Check both targets, always.** The same package on the other driver is a
   free control experiment. `multi-task` failed on LiveKit and passed on
   Pipecat, and that contrast is what located the bug. A difference between the
   two targets on one package is itself a bug, because removing that difference
   is the compiler's whole job.

4. **The first plausible cause is not always the cause, and one fix rarely
   explains every symptom.** `multi-task` showed two separate defects wearing
   one failure. The variable data flow was fixed first, which was real and
   necessary, and the retest still repeated the delegate. That second run is
   what exposed the context merge underneath. Retest after each fix, one fix at
   a time, and treat a symptom that survives as a new investigation rather than
   as the old one not working yet.

5. **Prefer a code gate to a prompt instruction.** A prompt is a request. The
   emitted delegate description already said "Do not run this flow again for the
   same request" and the model ran it again anyway. This repo's rule is that a
   rule with no gate is a wish.

6. **Land a test that fails without the fix.** Then prove it fails: revert the
   fix, watch the test go red, restore it. A test nobody has seen fail is a
   test that proves nothing.

7. **Re-run the gates**: `make test`, `make lint`, and `make smoke`. Then
   recompile the example, restart both targets, and have the human make the
   same call again.

## Results

Fill this in as you go. "Verified" means a human spoke to it on that target and
the pass criteria in the table were met.

Fresh sweep started on 2026-08-16 from commit `82000bb`. Earlier passes do not
count for this sweep; they are kept below as history.

| Example | LiveKit | Pipecat | Notes |
|---|---|---|---|
| `salon-support` | verified | verified | Fresh human booking on both targets with `customer_name=Ada` and `customer_id=cus_2002`. Both containers received that exact call-start payload. Pipecat rendered "talking to Ada" in the prompt and "Hi Ada" in the greeting, saved `requested_service: haircut`, then called `check_availability` and `book_appointment` once each. Its result `apt_56f20a93a4` matches the fixture hash for `cus_2002` plus the selected 3 p.m. slot, proving customer injection rather than the `cus_1001` default. LiveKit completed seven LLM turns and the human heard the personalized greeting and completed booking; its INFO log exposes no tool payloads. Both logs contain zero provider or runtime errors. |
| `mcp-example` | verified | verified | Fresh human search passed on both targets. Pipecat called `firecrawl_search` exactly once with Barcelona restaurant search arguments, completed in 6.09 seconds, and grew from 1,145 prompt tokens before the call to 166,124 after the results landed. LiveKit grew from 1,234 to 175,931 prompt tokens across its MCP result. Both spoke useful recommendations with zero malformed retries, MCP timeouts, provider errors, or runtime exceptions. |
| `multi-task` | verified | verified | LiveKit completed the fresh human booking with one customer step followed by one appointment step, a spoken confirmation, and no provider or runtime failure during the call. Its generated boundary assigns the customer task's `customer_id` into session state before rendering the appointment task, but LiveKit 1.6.10 INFO logs do not expose the exact tool payload. The first Pipecat call exposed B2: restoring a snapshot taken before the owner tool result arrived erased the completed delegation and ran `check_customer` twice. After V2 moved the snapshot behind the drained result, the fresh human retest ran `check_customer`, `lookup_customer`, `create_customer`, both finish tools, `manage_appointment`, `check_availability`, and `book_appointment` exactly once each. Customer identification produced `cus_e0aad0a9`; the restored owner context retained that completed delegate call and typed result; `book_appointment` received the same exact ID and produced `apt_3d243c32aa`. Name and phone were asked once, the caller heard the booking confirmation, and there were zero provider or runtime failures during the call. |
| `simple-prompt` | verified | verified | Fresh human bookings passed on both targets. Each Langfuse trace contains exactly one `lookup_customer`, `create_customer`, `check_availability`, and `book_appointment` span, all successful. LiveKit trace `095f35a97455020c44a6f0b66a278cb7` created `cus_e0aad0a9`, passed that exact ID with the returned 3 p.m. slot into booking, and produced `apt_3d243c32aa`; Pipecat trace `aa272ce5ab73b656860317f9e7723921` proves the same exact chain and IDs. Both callers heard the booking confirmation. LiveKit logged no in-call error; Pipecat logged only the documented harmless BusBridge startup line. |
| `subagents` | verified | verified | Pipecat completed the fresh booking-desk → appointment-manager → booking-desk flow with carried context and no duplicate transfer. The first LiveKit run exposed B3: reciprocal transfer tools were available during automatic entry inference, so the two exact announcements alternated four times without another caller turn. V3 now hides only `agent_transfer` tools during that inference through LiveKit's native `IGNORE_ON_ENTER` flag. A direct generated-context probe proved the booking desk receives the phone message plus the successful `lookup_customer` call and `cus_1001` result; an exact three-turn LiveKit text simulation then handed back and asked for the haircut time, not the phone. The final human retest confirmed one cue per transfer, no repeated phone question or greeting, continued booking, and zero runtime/provider errors. |
| `task-groups` | verified | verified | Fresh human bookings passed on both targets. Pipecat ran `manage_appointment` and every group step and business tool exactly once. Identification returned `cus_e0aad0a9`; selection returned `2026-08-17_haircut_0900`; booking received those exact values and returned `apt_c42f1701eb`; and the owner received all three typed task results. LiveKit completed the same three-step booking with no application error. LiveKit 1.6.10 INFO logs hide tool payloads, but its runtime copies the shared group context into each task and merges each completed task context, including tool results, before starting the next one. Pipecat logged its known harmless `LLMServiceMetadataFrame` ordering line before `StartFrame` reached the pipeline; there were no errors after startup. This example has no Langfuse tracing configured. |
| `outbound-reminder` | not run | not run | Ready for sequential real outbound calls. The former salon API dependency was removed; all three appointment outcomes are local Python fixtures. |
| `twilio-telephony-hello` | not run | not run | Browser calls prove the agent flow; the full pass also needs a telephony call. |
| `livekit-human-transfer` | not run | n/a | LiveKit only. A full pass needs a real SIP trunk and transfer destinations. |
| `pipecat-human-transfer-daily` | n/a | not run | Pipecat only. A full pass needs a deployed agent and Daily phone number. |
| `pipecat-human-transfer-twilio` | n/a | not run | Pipecat only. A full pass needs Twilio credentials and a telephony call. |

### Previous sweep

| Example | LiveKit | Pipecat | Notes |
|---|---|---|---|
| `salon-support` | verified | verified | Fixed by `reasoning_effort: "none"`. Full booking, both tools, zero 400s. |
| `mcp-example` | verified | verified | Needed the MCP timeout raised from 5 to 30. One malformed `sources` retry on each target, self corrected. |
| `multi-task` | verified | verified | Two defects, both fixed. LiveKit now calls `check_customer` exactly once, where it called it twice, then runs `manage_appointment`, `check_availability` and `book_appointment` carrying `customer_id: cus_e0aad0a9`, finishing `apt_05b7f78e68` with `status: booked` and zero errors. Pipecat did the same twice over. |
| `simple-prompt` | verified | verified | Full booking passed on both targets. All four tools ran once in order, the booking finished with `status: booked`, there were zero provider or runtime errors, and both traces reached Langfuse. |
| `subagents` | not verified | not verified | Handoff retest passed on both targets on 2026-08-16: each cue played once before activation, handback continued without another greeting, and Pipecat had zero errors or duplicate agent turns. The Pipecat call used a non-fixture phone number and stopped before availability and booking, so the full exact-ID script still needs one human run per target before either is marked verified. |
| `task-groups` | not run | not run | Shares the delegate machinery with `multi-task`, but on the `context_scope: shared` branch, which was already correct. Expect a pass with no new work. |
| `outbound-reminder` | not run | not run | |
| `twilio-telephony-hello` | not run | not run | |
| `livekit-human-transfer` | not run | n/a | LiveKit only. |
| `pipecat-human-transfer-daily` | n/a | not run | Pipecat only. |
| `pipecat-human-transfer-twilio` | n/a | not run | Pipecat only. |
