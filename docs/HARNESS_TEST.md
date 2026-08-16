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
> Langfuse credentials, use their Langfuse project as extra evidence. Langfuse
> is optional: never require access to a maintainer project, and never send a
> user's traces or credentials there by default.
>
> When something fails, reproduce it, find the shared root cause, add the
> smallest regression test, fix the shared compiler/runtime path, rebuild, and
> repeat the same live scenario on every affected target. Update source docs,
> generated README templates, public docs, and the bundled skill when emitted
> behavior changes.
>
> Keep the current sweep checklist and evidence in the task or PR, not in this
> prompt. Stop each development process before moving to the next package. At
> the end, run the repository's full build, test, lint, Python, smoke, and
> documentation gates, then report what passed, what was fixed, and any real
> external prerequisite that remains.
