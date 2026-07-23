# Tags and gating

Every feature in Unmute carries a **tag** that says how it behaves on a target. There are four tags. Learn them once here, and the rest of the docs can just use the words.

## The four tags

**`core`** works on all four targets, with no failures and no warnings. This is the safe stuff. If your whole agent is core, it runs everywhere, quietly.

**`warn`** works on all four, but at least one target prints a **warning** and keeps going. A warning goes to standard error and the command still succeeds (exit code 0). It means "this works, but not exactly the way you might expect on this platform". You should read it, but it does not block you.

**`gated`** **fails validation** on at least one target. The failure is a clear error that names the target and the reason. Gated does not mean broken; it means "works on some targets, not others". A guard on a handoff is gated: it works on Pipecat, it fails on Vapi.

**`provisional`** works on a target and runs cleanly, with no warning. It has not passed its automated credentialed test yet, but that does not block you or change how it runs. The provisional-versus-verified status is internal maturity tracking, recorded in `compile-report.json`, not something the CLI shows as a runtime warning.

## Failure is loud and early

A failure is always a clear error printed **before anything is generated or sent**. You never get a half-built project, and a feature never silently does nothing. If a target cannot honor a field, you learn it at `unmute validate`, not in production.

This is the same fail-loud rule from [how Unmute works](how-unmute-works.md), viewed from the field level: each field's tag is a promise about when it will stop you.

## Warnings are not optional reading

Because warnings still pass, it is tempting to ignore them. Do not. A `warn` tag means a real behavior difference on a real platform. Some examples you will meet:

- A minimum-words setting on interruptions works on Pipecat but is lossy on Deepgram (the model halts first), so Deepgram warns.
- Ignore-phrases for interruptions are dropped on Deepgram, with a warning.

On your chosen platform, a warning tells you the exact edge to test.

## How a feature earns its tag

A tag reflects the worst case across the targets you are compiling to. A feature that is `core` on four targets and `gated` on the fifth is `gated`, because on that fifth target it fails. When you compile to a single target, only that target's column matters, so a field that is gated "somewhere" may pass cleanly for you.

There is one detail worth knowing when you read the schema directly: a field inside a bigger feature inherits that feature's tag unless it says otherwise. For example, the fields inside a task are gated because tasks themselves are gated. You will not have to reason about this in the learn pages; the "what just got harder" section on each page states the outcome plainly.

## Where the tags come from

The tags are not opinions. They come from the Unmute schema (`SCHEMA.md` in the repository), which records, feature by feature and target by target, what each platform can do. Each entry was checked against the platform's own documentation. When these docs say "fails on Vapi", that is a row in the schema, not a guess.

Next: [tiers](tiers.md), which groups features into three levels of ambition and shows what each level costs you in portability.
