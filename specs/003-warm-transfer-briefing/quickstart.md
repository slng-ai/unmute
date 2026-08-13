# Quickstart: validating the manager briefing

Two layers. The offline one runs anywhere and pins the emitted shape. The live one is the
only thing that proves the feature, and it needs a real LiveKit Cloud project, a carrier
SIP trunk and two phones.

## Before anything

[Research R1](./research.md#r1-the-version-gap) is closed: every platform fact is verified
against `livekit-agents` 1.6.9, the version the deployment runs. It found one rename that
would have shipped an unimportable project.

Keep the recipe, because it is the cheapest source of truth in this repository for anything
the emitted project depends on:

```sh
CID=$(docker create --entrypoint sh unmute-lk-fixed:latest)
docker cp "$CID":/usr/local/lib/python3.12/site-packages/livekit/agents/beta/workflows/warm_transfer.py .
docker rm "$CID"
```

and to exercise a shape rather than read it:

```sh
docker run --rm --entrypoint python unmute-lk-fixed:latest -c "<check>"
```

The image is built from a compiled `examples/human-transfer`, so it holds exactly the
dependency versions a deploy would.

## Layer 1: offline, runs in CI

```sh
make fmt
make lint
make build
make test
```

Then read what changed rather than trusting the suite:

```sh
git diff internal/generate/testdata/golden/
```

The goldens with a warm transfer move. Read the diff and check it against
[contracts/emitted-briefing.md](./contracts/emitted-briefing.md) line by line. Do not
regenerate blind. `livekit_v1_sip-inbound-trunk.json` and `livekit_v1_sip-dispatch-rule.json`
must not move at all.

What the offline layer proves, and what it cannot:

| Proves | Cannot prove |
|---|---|
| The deprecated parameter is gone from every emitted file | That the manager hears a briefing |
| The persona constant is emitted once, only for packages with a warm transfer, and says all four things | That a small model follows it |
| The conversation is read once into a local and both uses share it | How many messages a real call produces |
| Every log line exists with its level, and no destination value appears in any of them | That the log reads well on a real deployment |
| A cold-only package gains neither the persona nor the extra import | |
| A package written before this change compiles with no edit | |

## Layer 2: live, the only end-to-end proof

```sh
make build
./bin/unmute compile examples/human-transfer
cd examples/human-transfer/build/livekit
lk agent deploy
```

No secret changes, so no secret update is needed. Then, in one terminal:

```sh
lk agent logs
```

### Run A: the briefing arrives and the call is handed over (three consecutive times)

Call the agent's number. Give a name, say which stylist, say what went wrong. Ask for a
manager. Answer the supervisor's phone and say "hello".

Expected on the supervisor's phone: the **first** sentence names the caller and the
complaint. No greeting, no "how can I help". It ends with a question. When you say you can
take it, the caller is put through and the agent says its one goodbye line and goes quiet.

Expected in the log:

```text
human transfer fired: escalate_to_supervisor (warm)
warm transfer dialling out: handing over <n> conversation messages
warm transfer merged after <n>s: <identity>
```

`<n>` messages must be more than one. If it is zero or one, the briefing had nothing to
work with and that is the defect, not the prompt.

### Run B: nobody answers

Ask for a manager and do not answer the supervisor's phone.

Expected: the caller comes back to the agent, or the call ends, whichever the package
declared. The log shows the dial line and then `warm transfer unavailable after <n>s`,
with a duration close to the package's ring timeout.

### Run C: answered and never decided (three times)

Answer the supervisor's phone, say hello, and then talk about anything except taking the
call. Do not say yes and do not say no.

Expected: the agent asks again, and then declines on your behalf and gives a reason. The
caller comes back to the agent. The log shows `warm transfer unavailable` with a duration
well past the ring timeout.

**This run can fail and the feature can still ship.** There is no hard bound after the
answer, only a prompt telling the model to decline. If the caller is left on hold with
music and no line 3 appears, the exit did not hold, and the two ways to get a real bound
are in the plan's Complexity Tracking. Run it three times and record all three: SC-005
asks for that count because the reliability of a best-effort exit is a number nobody has
yet, and one lucky run is not it.

### Run D: the cold transfer still works

**This run needs a real phone call, not the Agent Console.** Cold refers the caller's
existing SIP leg out, and a console session has no SIP leg, so from the console the tool
logs `cold transfer skipped: no phone caller in the room` and the agent carries on. That
is by design, the platform's own example carries the same guard, and it was learned the
hard way on 2026-08-12 (research R8). Runs A through C do work from the console, because
warm dials out.

Before the first cold run, the inbound path must exist once: an inbound trunk for the
package's number, and a dispatch rule that names **this** package's agent. Both files are
in the build directory and the generated README's "Create the LiveKit SIP resources"
section holds the exact commands. Check with `lk sip dispatch list` that a rule points at
`agentName: livekit`; a rule pointing at another agent name sends the call elsewhere and
this worker never sees it. The Twilio number's origination URI must point at the LiveKit
project's SIP endpoint, and the trunk the call arrives on must have Call Transfer (SIP
REFER) and PSTN transfer enabled.

Then call the number and ask about an invoice. The caller should be referred out.

Expected log: the fired line, `cold transfer referring the caller out`, then
`cold transfer completed after <n>s`. Nothing else about cold transfer changed, and its
golden proves that offline.

## What to record

Whatever the runs do, write down for each one: the first sentence the supervisor heard,
the message count, the outcome line and the duration. That is the record the next change
to this area starts from, and it is the record that did not exist for the three live calls
before this feature.
