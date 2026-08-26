# Prompt: live-test every example

Use this prompt to start a fresh manual release sweep:

> Test every package under `examples/`, one at a time, on every target it
> declares. Start from the current branch and run `make build` before testing.
>
> For each package, read its `README.md`, `agent.yaml`, `targets.yaml`, and any
> connection files first. Run `unmute validate`, `unmute compile`, and then
> `unmute dev` for each declared target. Use separate ports when comparing
> LiveKit and Pipecat. Wait until each process is ready, then give the tester the
> local URL or tell them when to place the phone call.
>
> Test the behavior the package exists to show, not only its greeting. Supply
> explicit non-default values for every `call_start` variable. For tasks,
> groups, and handoffs, verify that context, variables, tool calls, and tool
> results reach the next step exactly once. For telephony, test every direction
> the package declares and test transfer behavior with real calls when needed.
> Restore any carrier number, webhook, SIP trunk, or external configuration
> changed during the run.
>
> Watch the generated `build/<target>/dev.log` files while the tester speaks.
> Inspect tool arguments, task transitions, errors, duplicate calls, and final
> results. If the package opts into tracing and the tester supplied their own
> credentials, use their tracing project as extra evidence: a Langfuse project
> for `provider: langfuse`, or the Coval simulation the call belongs to for
> `provider: coval`. Tracing is optional: never require access to a maintainer
> project, and never send a user's traces or credentials there by default.
>
> Traces can contain caller speech, model input and output, and tool arguments and results.
> Use only fake identities and fake customer data for release tests. Use a
> separate project on the tracing provider for the sweep.
>
> When something fails, reproduce it, find the shared root cause, add the
> smallest regression test, fix the shared compiler/runtime path, rebuild, and
> repeat the same live scenario on every affected target. Update source docs,
> generated README templates, public docs, and the bundled skill when emitted
> behavior changes.
>
> Keep the current sweep checklist and evidence in the task or PR. A package
> may also carry a small tracked release table when its own acceptance contract
> requires one. Stop each development process before moving to the next
> package. At the end, run the repository's full build, test, lint, Python,
> smoke, and documentation gates, then report what passed, what was fixed, and
> any real external prerequisite that remains.

## Salon concierge release gate

Restart `unmute dev` before every independent run; the salon worker starts with
an empty database. The local phone checks are documented in the package README.
The real carrier checks below are separate: run them only on reachable phone
routes.

First run this script with the manager answering, then with the manager
declining or not answering. Wait for each response.

1. “I need help with a complaint.”
2. “My name is Alex Test.”
3. “My phone number is plus one, five five five, zero one zero.” Pause, then
   say: “Eight eight four four.”
4. After the complete identity readback, say: “Yes, that is correct.”
5. “My haircut was uneven and I want to speak to a manager.”

An answered run needs observed two-way human audio. Carrier acceptance alone
does not prove that the manager answered. An unavailable run must end without a
new concierge greeting or a claim that the manager answered.

Restart the worker. Then run this combined script in the browser on both targets
and on each reachable phone route:

1. “I want to book a haircut tomorrow at three. My name is Robin Taylor.”
2. “My number is five five five zero one zero.” Pause, then say: “Eight eight
   four four.”
3. After the complete identity readback, say: “Yes, that is correct.”
4. After booking preparation starts: “Actually, my last haircut was uneven. I’d
   like the salon to fix it.”
5. Only after customer care says the complaint was saved: “I want to speak to a
   manager.”

Required action order:

```text
verify_customer
find_or_create_customer       exactly once
to_booking
manage_booking
get_current_date
check_availability
to_complaints                 from the active booking task
record_complaint              exactly once
to_manager                    exactly once
```

Expected state is one customer, zero bookings, and one complaint, with no
second verification, apply task, or booking mutation. Browser runs must state
that transfer needs an inbound call and start no carrier action. The inbound
combined run must reach the same state and action order before its terminal
transfer. Run the package's
[verification stress checks](../examples/salon-concierge/README.md#verification-stress-checks)
before filling in release evidence.

Restart the worker. Then run this compound-request script on both targets. The
scripts above raise the second request while a step that can route it is active;
this one asks for two things in one turn, so the second may still be owed when
the apply step, which carries no handoff, takes over.

1. “I want to book a haircut tomorrow at three. My name is Robin Taylor.”
2. “My number is five five five zero one zero.” Pause, then say: “Eight eight
   four four.”
3. After the complete identity readback, say: “Yes, that is correct.”
4. Answer the confirmation question with both requests in one turn: “Yes, go
   ahead, and also record my complaint about the uneven haircut I got last
   time.”
5. Only if the agent does not raise the complaint itself: “So what about my
   complaint?”

Two outcomes pass. The confirmation step may leave for customer care on step 4,
which saves no booking; or it may confirm, the apply step saves the booking and
ends with its own finish, and the booking specialist raises the complaint on its
own next turn. Either way `record_complaint` runs exactly once and the booking is
saved zero or one time. A run fails when the agent answers in place with a line
like “please contact the salon directly”: the apply step has no complaint route,
so it must finish, name the request in its result, and let the specialist take
it. Needing step 5 is a weak pass worth recording: the handback carried the
request and the specialist ignored it.

Record the result in the package's
[release evidence table](../examples/salon-concierge/README.md#release-evidence).
Use only the date/revision, target/case, sanitized trace or session ID, ordered
action counts, final SQLite counts/status, carrier child-leg or SIP outcome,
and pass/fail result. Never paste raw traces or caller data.

## Knowledge base retrieval gate

Whether the agent answers from the documents or from the model. A confident wrong
answer and a confident right answer sound identical on a call, so this gate reads
the log rather than trusting the voice.

Three things can fail independently, and separating them is the whole point of the
order below: retrieval can fail, the model can fail to call the tool, or the model
can call it and ignore the result.

### Step 0: prove retrieval without a call

Do this first, always. It takes seconds and it removes two layers.

```sh
unmute compile examples/salon-concierge
python scripts/check_knowledge_retrieval.py examples/salon-concierge/build/livekit \
  --quiet-index \
  refunds  "what does reference RC-2026-04 cover"                        "RC-2026-04" \
  services "what does a cut cost when I book it with a colour service"   "twenty-eight euros" \
  services "can I split the bill across three cards"                     "two cards" \
  refunds  "what happens if I asked for something against the stylist advice" "half price"
```

Every line must say `HIT`. A `MISS` means retrieval itself is wrong and no call
will tell you more than this did. An `ERROR` means the index never built: read the
startup log for a missing credential or an unreadable document.

Run it in the built image instead if you want the environment that ships:

```sh
docker build -t kb examples/salon-concierge/build/livekit
docker run --rm -e OPENAI_API_KEY="$OPENAI_API_KEY" -v "$PWD/scripts:/s" kb \
  python /s/check_knowledge_retrieval.py . --quiet-index \
  refunds "what does reference RC-2026-04 cover" "RC-2026-04"
```

### Step 0b: check the baked index, if the image carries one

A deployed image should have the index baked in, and the way to be sure is to start
it with a credential that cannot work. It should still index, because nothing is
embedded at startup:

```sh
docker build --build-arg KNOWLEDGE_BAKE=1 \
  --secret id=OPENAI_API_KEY,env=OPENAI_API_KEY \
  -t kb examples/salon-concierge/build/livekit
docker run --rm -e OPENAI_API_KEY=sk-invalid kb \
  python -c "import logging; logging.basicConfig(level=logging.INFO); \
             import knowledge; knowledge.build_indexes()"
```

Expect `loaded from the baked index, nothing embedded` per base. If it raises a 401
instead, the bake did not happen: check that both the build argument and the secret
were passed. `unmute dev` does not bake, so a dev run logs a warning saying it is
embedding at startup, which is correct there.

### Step 1: the live call, one target at a time

```sh
unmute dev examples/salon-concierge --target livekit --verbose
```

Before speaking, wait for one line per base. It is the proof that documents were
compiled in and indexed, and it names the settings actually in force:

```text
knowledge 'refunds': 12 passages indexed (mode hybrid, chunk_size 90, overlap 20, top_k 3)
knowledge 'services': 5 passages indexed (mode hybrid, chunk_size 220, overlap 40, top_k 3)
```

A passage count of 0, or no line at all, means stop: there is nothing to retrieve
and everything the agent says next is invented.

Then say these, waiting for each answer. Each one is a fact that exists only in
the documents, so a model cannot produce it from training:

1. “What does reference R C twenty twenty-six oh four cover?” → must say it is the
   refund and complaints policy.
2. “If I book a cut together with a colour, what does the cut cost?” → **twenty-eight
   euros**, not the full cut price.
3. “Can I split the bill across three cards?” → no, **two** at most.
4. “I asked for something the stylist advised against, and I signed the card. Do I
   get a refund?” → no refund, a redo at **half price**.
5. “Which morning is the quiet studio session?” → **Tuesday**.

Then the negative control, which matters as much as the rest:

6. “Do you fit hair extensions?” → the agent must say it does not have that
   information. The documents never mention extensions. If it invents a price or a
   duration here, retrieval is working and grounding is not.

While each answer plays, watch for the pair of lines that says a lookup actually
ran:

```text
knowledge 'services': returned 3 result(s) in 215 ms
```

That line carries a count and a duration and never the caller's words or the
passages, so it is safe to keep on in any environment.

### Step 2: the same script on Pipecat

```sh
unmute dev examples/salon-concierge --target pipecat --verbose
```

Same startup lines, same six questions, same expected answers. The knowledge
module is one shared file, so a difference between the two targets is a difference
in how the tool is registered, not in retrieval.

### Reading a failure

| What you see | What it means |
|---|---|
| Step 0 says `MISS` | Retrieval is wrong. Fix it there; a call adds nothing. |
| No `passages indexed` line | Nothing was compiled in, or the index died. Read further up the log. |
| Answer is right, no `returned N result(s)` line | The model answered from training and never called the tool. Its description is the thing to change, not the retrieval. |
| `returned N result(s)` present, answer still wrong | The tool was called and the result ignored or misread. A prompt problem. |
| `error": "lookup unavailable"` in the transcript | The lookup raised. The process log has the real reason; the model is deliberately told nothing it could read aloud. |
| Negative control answered confidently | Grounding problem, not a retrieval problem. |

### Trying the retrieval modes

`mode` is per base, and the difference is audible on the right question. Set
`mode: keyword` on `services` in `agent.yaml`, recompile, and ask question 2
again: it should still land, with no embedding call at all and a lookup under
2 ms. Then ask a question that shares no words with the document, such as “what
will it set me back for a trim and a blow dry”, and expect it to do worse than
`hybrid` did. That contrast is the reason the default is `hybrid`.
