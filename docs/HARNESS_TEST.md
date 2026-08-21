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
