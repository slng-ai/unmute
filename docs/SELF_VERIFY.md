# Verifying behaviour without a person on the phone

`make test` proves what a compiler can prove: that the right text reached the
right file. It cannot prove that a caller heard the right thing. That used to
mean waiting for a human to place a call, which is slow, unrepeatable, and easy
to over-read.

This is how to check runtime behaviour alone. It is not a replacement for the
live call in [HARNESS_TEST.md](HARNESS_TEST.md); it is what to do **before** you
ask for one, so the live call confirms rather than discovers.

Written after using it on the SLNG Context Router cache-scope collision, where it
turned a defect nobody could reproduce on demand into a two-line table.

---

## The rule that saves the most time

**Find the layer the defect lives in, and reproduce it there.**

The instinct is to rebuild the whole call: run the agent, expose it, point a
simulated caller at it, talk. That is weeks of harness for most bugs, and the
harness becomes the thing you are debugging.

Ask instead: *what is the smallest request that can exhibit this?* A provider
defect usually lives in one HTTP call. The router collision was a header on a
chat completion. It had nothing to do with audio, transport, turn detection, or
either driver's runtime, so reproducing it needed none of them. Four requests and
a header change did it, in seconds, repeatably.

Work down this list and stop at the first rung that can show the bug:

1. **A unit or golden test** — the emitted artifact is wrong. Cheapest, and it
   becomes a permanent gate.
2. **One provider request, replayed** — the artifact is right and the provider
   behaves unexpectedly. What the rest of this file is about.
3. **The compiled project in the browser** — the defect needs the framework's
   own runtime, e.g. frame ordering or a settings update landing late.
   `unmute dev <source-dir>` is this rung, and it is the only local one: it
   exercises the prompt, the tools, the models and the turn-taking with no phone
   involved.
4. **A call to a deployed agent** — the defect needs real audio, real timing, or
   a real carrier. Nothing local reaches this rung, because a carrier reaches an
   agent over publicly routable signalling and media ingress. Deploy, finish the
   carrier setup in the emitted README, and call the number.

Going straight to rung 4 for a rung 2 problem is the mistake this document
exists to prevent.

---

## Reproducing at the provider layer

### Read the request out of the artifact, never retype it

The replay has to send exactly what the agent sends. Paraphrasing a system prompt
changes the router's cache key and the run proves nothing. So read it:

- **`build/<target>/compile-report.json`** has the resolved bindings — `agent_id`,
  `model`, `params`, `upstream` — plus the agent and task names, as structured
  data.
- **`build/<target>/agent.py`** (LiveKit) or **`bot.py`** (Pipecat) has the prompt
  literals and the inline configuration. Parse them with `ast`, do not copy them.

[`scripts/replay_router_scopes.py`](../scripts/replay_router_scopes.py) does both.
It is the worked example for this whole section, and it works on any package with
a router think binding:

```sh
./bin/unmute compile examples/salon-concierge --target livekit
SLNG_API_KEY=... OPENAI_API_KEY=... \
  python3 scripts/replay_router_scopes.py examples/salon-concierge \
    --first concierge --second booking_specialist --summary
```

### Change exactly one variable

Two arms, identical but for the thing under test. The router run used one shared
cache scope against one scope per prompt site, nothing else different, so the
result could not be attributed to anything else.

Without a control arm you are measuring the provider's mood.

### Read the provider's own answer, not your inference

Providers usually say what they did, and it beats guessing from latency:

| Header | What it says |
|---|---|
| `x-slng-response-source` | `llm` (generated) or `cache` (served) |
| `x-slng-cache-layer` | which layer answered: `l1_exact`, `l2_exact` |
| `x-slng-request-id` | correlates the request with the router's own logs |

Latency is a **weak** signal. Recorded hits have been as fast as 1.27 ms and as
slow as 396 ms, which overlaps a fast generation. Never conclude "that was
cached" from a timing alone when a header will tell you.

If a provider gives you no such header, its logs are the fallback. For this
router that is Datadog, `service:slng-llm-router`, with `@cache_layer`,
`@ttft_ms` and `@request_id` as columns.

### Three runs, on fresh state

One run is not evidence. This repository has measured two runs of an identical
build differing by more than a second of mean silence, and the same variance
applies to which turns a router chooses to cache. Three, each on state that
cannot have been warmed by the last, and say so when fewer than all three agree.

### Never write into production state

Use a throwaway prefix for anything that scopes a cache, a namespace or a
tenant. A shared-arm run deliberately writes an answer under one scope and reads
it from another; doing that under a shipped package's `agent_id` would poison the
cache real callers hit.

### Machine gotcha

Python from python.org ships no CA bundle, so `urllib` raises
`CERTIFICATE_VERIFY_FAILED`. macOS keeps one at `/etc/ssl/cert.pem`; pass it as
`ssl.create_default_context(cafile=...)`. `curl` is unaffected.

---

## Having Coval judge the result

Coval evaluates conversations you **push** to it. Nothing has to dial in, so
nothing has to be publicly reachable and no tunnel is needed. This is the part
that makes self-verification possible at all.

`COVAL_API_KEY` goes in the `X-API-Key` header. Base `https://api.coval.dev/v1`.

| Step | Call |
|---|---|
| create a metric | `POST /metrics` with `metric_name`, `description`, `metric_type`, `prompt` |
| push a conversation | `POST /conversations:submit` with `transcript`, `agent_id`, `metrics` |
| pull the verdict | `GET /conversations/{id}/metrics` |
| save a scenario | `POST /test-sets`, then `POST /test-cases` with `test_set_id` |
| attach defaults | `PATCH /agents/{id}` with `metric_ids`, `test_set_ids` |

**A metric does not attach to a test case.** `metric_ids` on `POST /test-cases` is
accepted and silently ignored — no error, no field on the way back. Metrics bind
on a run, or fall back to the agent's defaults. So put them on the agent, or a run
that forgets the flag judges with whatever the agent already had and never reports
what you were looking for. Read the created object back and check the field is
there; a 201 is not proof the field landed.

A transcript is OpenAI-format messages. `metadata` and `tags` are yours for
filtering. The combined payload cap is 256 KB.

### What the judge can and cannot see

Found by getting it wrong three times, so do not repeat it:

**The judge reads `user` and `assistant` content. Nothing else.** Not
`metadata`, not `system` turns, not a message's `name` — all three are stored and
returned by the API, and none reach the judge. It says so when you rely on them:
*"The transcript does not show who the speaking_agent is."*

So a metric for a multi-agent defect has to be written against **what a caller
could hear**. Do not try to smuggle the role in; judge the symptom. For the
router collision that became "is the caller asked twice for the information that
identifies them", which needs no knowledge of which agent spoke.

### Write the metric narrow, and check its reasoning

A broad question gets a noisy answer. "Did the agent repeat itself" failed a
correct conversation three times out of six, and its explanations gave it away:
`"I'll take care of that."` was called a repeated request for a phone number, and
two conversations with byte-identical final turns got opposite verdicts.

So always read `explanation`, not just `value`. A judge that contradicts itself
in its own explanation has told you the metric is wrong, not the code. Narrow the
question until the reasoning is sound, and if a verdict still disagrees with a
measurement, **trust the measurement and report the disagreement**.

### Where the judge belongs in the argument

An LLM judge is corroboration. The conclusion should rest on things that are
measured: a response header, an exact string comparison, an exit code. Report the
judge alongside, including where it was wrong.

---

## Writing up what you found

- **State the method before the result**, including what was held constant.
- **Give the counts**: "6 of 6" and "0 of 6" beat "it works now".
- **Quote the words** when behaviour is the subject. A table showing the second
  agent speaking the first agent's line verbatim is the whole argument.
- **Say what it does not cover.** The router replay proves nothing about audio,
  transport or turn-taking, and saying so is what makes the rest credible.
- **Claim no latency or behaviour improvement that was not measured**, and never
  that something feels better.

Keep the raw output. `specs/<nnn>-<slug>/evidence/` is the local home for it, with
a `RESULT.md` that a reader can follow without rerunning anything.

---

## Skills this draws on

Installed skills, useful in roughly this order:

| Skill | For |
|---|---|
| `coval-resources` | the resource model: agents, test sets, metrics, runs, simulations, and the ID formats |
| `configure-metrics` | choosing and writing metrics, including the LLM-judge types |
| `build-test-suite` | turning a scenario into a saved test set |
| `setup-agent` | registering an agent, when a simulated caller really does have to dial in |
| `launch-run`, `watch-run`, `get-results` | the dial-in path: launch, poll, collect |
| `quick-eval` | those three in one pass |
| `diagnose`, `debug-traces` | reading a failed run |
| `distill-test-set` | narrowing a large set to the cases that discriminate |
| `find-docs` | current provider docs through the `ctx7` CLI, per the context7 rule |
| `ponytail` | the ladder that keeps a reproduction four requests instead of a harness |

The `launch-run` family and `setup-agent` assume the `coval` CLI
(`brew install coval-ai/tap/coval`). It is **not** installed here, so the REST
calls above are the working path; the skills are still the reference for what to
call and in what order.

Upstream: <https://github.com/coval-ai/coval-external-skills>. API spec is live at
`https://api.coval.dev/v1/openapi`, which lists per-area specs
(`/openapi/agents`, `/openapi/conversations`, `/openapi/metrics`) — fetch it
rather than trusting this table, which is a snapshot.
