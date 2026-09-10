# Docs review: orchestration and tasks

A read of the public docs (`docs-site/`) with one question in mind:

> Can a developer who has never written a task start from one plain agent, add a
> task, and end up with tasks that say what success looks like?

Today they can, but only by reading five pages in an order the nav does not
suggest, and by working out a few things no page says out loud. The material is
nearly all there and most of it is well written. What is missing is a ladder:
one page that climbs from no tasks to `finish:`, on one package the reader
already has on disk.

These are recommendations only. Nothing in `docs-site/` was changed.

**The short version**

| | |
|---|---|
| Biggest win | one new page that walks no task → task → `assign:` → `finish:` → group, on the package `unmute init` writes (note 1) |
| Cheapest wins | put Tasks before Handoffs in the nav (note 2); move `finish:` up the Tasks page and reframe it as "what success is" (note 3) |
| Must fix | the salon worked example in `choosing-a-structure` is wrong three ways since PR #205 (note 7); `dev/overview` contradicts itself (note 9) |
| Biggest clarity win | one word per idea. Today "task", "step" and "flow" all name the same thing (Part 3) |
| What to cut | the "on a live call we saw…" stories, in four places (Part 6) |

Pages read: `docs-site/build/orchestration/*`, `docs-site/build/your-first-agent.mdx`,
`docs-site/build/how-a-package-fits-together.mdx`, `docs-site/build/variables.mdx`,
`docs-site/best-practices/*`, `docs-site/reference/agent-yaml.mdx`,
`docs-site/reference/variables.mdx`, `docs-site/start/*`, `docs-site/dev/overview.mdx`,
plus `examples/` and `internal/skill/assets/references/orchestration.md`.

---

## Part 1 — The main gap

### 1. There is no page that walks the ladder

This is the biggest one, and most of the smaller notes below fall out of it.

What exists today, in nav order:

| Page | Where the reader is left |
|---|---|
| `build/your-first-agent` | one agent, one tool, no task |
| `build/how-a-package-fits-together` | two agents, two tasks, handoffs and escalations, all at once |
| `build/orchestration/overview` | a table of three shapes |
| `build/orchestration/handoffs` | the one shape that does **not** return |
| `build/orchestration/tasks` | everything about tasks, in one long page |
| `build/orchestration/task-groups` | groups |
| `build/orchestration/choosing-a-structure` | how to decide |

The jump from page 1 to page 2 is the problem. Page 1 ends with a scaffolded
agent that has no task. Page 2 opens with a finished two-agent package that
already has tasks, handoffs, escalations, `assign:` and `context:`. Nothing in
between shows a reader adding one task to the agent they just made.

**Recommendation.** Add one page, `build/orchestration/add-your-first-task`,
placed between `how-a-package-fits-together` and the rest of the Orchestration
group. It takes the package `unmute init` writes and grows it one key at a
time. Each rung shows the diff, the command to run, and what changes in the
call:

1. **Start with no task.** One agent, one tool, one prompt. Say plainly that
   this is fine and that most agents should stay here.
2. **Add the tool that does the work.** Still no task.
3. **Say what you want to keep.** Declare one variable with a type.
4. **Add the task.** `name:`, `when:`, `instructions:`, `tools:`.
5. **Save the answer.** Add `assign:`. Run it, and show the value landing.
6. **Read it back.** Put `{{the_variable}}` in the owner's prompt.
7. **Say what success is.** Add `finish:` with a `success:` pair. Show the
   before and after: without it the model decides the step is done, with it the
   tool's own result decides.
8. **Two steps that must run in order.** Turn it into a task group, then add
   `skip_when_confirmed:`.

Every rung should be runnable in the reader's own `my-agent` directory. That is
the whole point: the reader has a package on disk from page 1 and never uses it
again.

Two things that would make the page carry its weight:

- **Tell the reader to run `unmute validate` after every rung.** That is the
  loop, and no page in the Build section says so as a habit.
- **Show the errors they will actually hit.** The compiler's refusals are good —
  they name the file, the line and what to write instead — but a reader who has
  never seen one does not know that. Three are worth quoting on the page,
  because a beginner hits them on their first or second try:

  ```text
  task "verify_customer" has no when: and no task group lists it in steps:, so
    nothing runs it. Give it a when: so its agent can decide to run it, or list
    it as a step of a task group that is reached
  ```

  ```text
  assign writes to "customer_phone", and it is not declared under the
    variables: block
  ```

  ```text
  finish names "create_booking" with no success:; a step cannot end on a result
    nothing checks
  ```

  Showing these turns the compiler into a teacher instead of an obstacle.

### 2. Put Tasks before Handoffs

The Orchestration group is ordered handoffs, tasks, task groups, choosing.
So the first shape the reader meets is the one that never comes back, and the
handoffs page opens by telling them to use a task instead — a thing they have
not read about yet.

**Recommendation.** Order the group: **Tasks → Task groups → Handoffs →
Choosing a structure**, and move the footer cards to match. A handoff is the
rarer shape and the one that costs the most; it should come after the reader
knows what returning looks like.

### 3. `finish:` is filed as a speed trick, not as "success defined"

On the Tasks page, `finish:` sits at line 194 of 314, under "End on a tool",
and its first sentence is about spending a model request. But `finish:` is the
answer to the question the reader actually has: *how does the step know it
worked?* Saving a round trip is a bonus.

**Recommendation.**

- Move `finish:` up so it sits directly after `assign:`. The two are one idea:
  `assign:` is *what the step keeps*, `finish:` is *when the step is done*.
- Rename the section "Say what success looks like", and keep "it also removes a
  model request" as the second paragraph, not the first.
- Add a two-row table the reader can look at once and remember:

  | | Who decides the step is finished |
  |---|---|
  | no `finish:` | the model, by calling `finish` |
  | with `finish:` | the tool's own result, checked against `success:` |

### 4. Two different things are both called "finish"

On the Tasks page, `finish` is the function the model calls to end a task, and
`finish:` is the key that lets a tool end it instead. The page uses both without
ever saying they are different. That is why line 237 has to say "**`finish` is
still there**" — the sentence exists to undo confusion the page created.

**Recommendation.** Say it once, plainly, the first time either word appears:

> Every task gets a `finish` call. The model uses it to say the step is done.
> The `finish:` key is different: it names tools whose own result ends the step,
> so the model does not have to make that call.

Then use "the `finish` call" and "the `finish:` key" consistently, never bare
"finish".

### 5. Nothing says what the model actually receives

A reader writing their first task prompt does not know:

- that Unmute appends text to their prompt (the "generated tail" is named once,
  at `tasks.mdx:241`, with no explanation of what it is);
- that they should **not** write "call finish when you are done" themselves;
- that the appended text changes when `finish:` is present;
- that the escape rule for `unserved_request` is already written for them.

**Recommendation.** Add a short section to the Tasks page, "What the model gets
for a task", listing the four parts in order: your instructions file, then the
appended finish rule, then the values your placeholders named, then the history
you chose. Show the appended text in a fenced block so the reader can see it.
The text is built in `internal/generate/task_prompt.go`.

Also worth showing once: a task appears to the model as one more entry in its
function list, next to the agent's tools. That single sentence explains why the
model sometimes skips a step, which is the whole subject of
`best-practices/step-scoping`.

### 6. Four keys make a finished step, and they live on four pages

To build the step the salon example shows, a reader must visit:

| Key | Where it is taught |
|---|---|
| `assign:` | Tasks, and again in Variables |
| `finish:` | Tasks |
| `confirm:` | Variables reference, and the Prefetch page |
| `skip_when_confirmed:` | Task groups |

**Recommendation.** One short section on the Tasks page — "A step's contract" —
with all four in one table: what it declares, where it is written, and what
happens if you leave it out. Link each to its full page. A reader should be
able to see the whole contract on one screen.

---

## Part 2 — Things that are now wrong

These are facts, not taste. Each one can be checked in a minute.

### 7. The salon worked example in `choosing-a-structure.mdx` is wrong three ways

`docs-site/build/orchestration/choosing-a-structure.mdx:136-146`. All three
claims are now false against `examples/salon-concierge/agent.yaml`:

| The page says | The package does |
|---|---|
| "A task group would buy nothing extra, and the package declares none." | It declares `book`: `verify_customer` with `skip_when_confirmed: customer_phone`, then `manage_booking`, `context_scope: shared`, `then: return` (`agent.yaml:73`, `agent.yaml:156-166`) |
| "`manage_booking`'s own `when:` says it runs only once the caller is identified" | `manage_booking` has no `when:` at all (`agent.yaml:47-71`). It is a group step, which is exactly why it needs none |
| verification is a task the concierge picks | `verify_customer`'s `when:` now says to run it **only** when the caller corrects their phone number; every other route in goes through the group (`agent.yaml:17-21`) |

This matters more than a normal stale paragraph, because this is the page a
reader lands on to decide whether they need a task group, and its worked answer
is now the reverse of what the shipped example does.

**Recommendation.** Rewrite the section to the real answer. It is a better
teaching example than the old one:

- verification then booking really does have to hold that order, every time, so
  it is a group;
- the second booking on a call skips verification, because the number is
  already confirmed;
- the concierge makes one call to enter the group instead of choosing between
  two steps;
- and `manage_booking` has no `when:` precisely because the group decides.

That paragraph would also cover note 14 below.

### 8. `examples/README.md` describes the old salon

The table row still reads "Two agents, two tasks (one shared by both agents
from a single definition), handoffs, a guarded task, a cold manager transfer".

The salon now defines four tasks (`verify_customer`, `take_confirmation_contact`,
`manage_booking` on the concierge; `handle_complaint` on the specialist), shares
none of them, and adds a task group. The "shared task" claim is now the reverse
of what the package does on purpose.

**Recommendation.** Update the row, and mention the task group, since this table
is where a reader picks which example to open.

### 9. `dev/overview.mdx` contradicts itself about task rows

- Line 122: "A call into a task, or a handoff to another agent, gets its own
  row too, labelled `HANDOFF`, and carries no duration."
- Line 152: "Controls do not get a row."

Both are on the same page. The second is stale, and it also uses "controls", a
word that appears nowhere else in the reader-facing docs.

**Recommendation.** Delete or rewrite the second paragraph, keeping its
reasoning (a task does not return until its whole flow ends, so timing it as a
tool would be misleading) and attaching that reasoning to the row that does
exist. This matters more than its size suggests: this is the one page that tells
a reader how to *see* their task run.

### 10. "controls" survives in `start/how-unmute-works.mdx`

Line 25: "Model names point at model definitions, controls point at tasks or
agents". "Controls" is retired vocabulary. A first-time reader has no idea what
it means.

**Recommendation.** "Names in an agent's lists are resolved to the tasks,
agents and tools they point at."

### 11. "Three things follow" is followed by four bullets

`tasks.mdx:232`. Small, but it is in the section a reader is reading most
carefully.

### 12. The Tasks page snippets do not fit the package the reader has

`build/your-first-agent` leaves the reader with a scaffold whose models are
named `assistant_model` and `assistant_voice`. Every snippet on the Tasks page
uses `think: reasoning` and `speak: voice`, which are the salon's names.

A reader copying a snippet into their own `my-agent` gets a validation error on
their first attempt at their first task.

**Recommendation.** Either use the scaffold's names throughout the Build
section, or trim the task snippets so the `agents:` header and model lines are
not shown at all — only the `tasks:` block being added. The second is probably
better: it keeps the snippet about the one idea.

### 13. Nothing says where task prompt files go

Every example writes `instructions: tasks/verify-customer.md`, and no page says
that `tasks/` is a folder the reader creates next to `agent.yaml`, or that the
path is relative to the package.

**Recommendation.** One sentence on the Tasks page, at the first snippet.

### 14. A task with no `when:` is legal, and the guide never says why

The Task groups page defines three tasks with no `when:`
(`task-groups.mdx:26-45`), while every task on the Tasks page has one. The rule
is only in the reference and in the coding skill: a task with no `when:` is a
definition only, usable as a group step, and an agent naming it directly is
refused.

**Recommendation.** One line under that snippet: "These tasks have no `when:`.
The group decides when they run, so they need no trigger of their own. A task an
agent runs on its own does need one."

### 15. `tasks.mdx` says the generated tail stops asking for `finish`. It does not.

`tasks.mdx:241`: "**The step's prompt stops telling the model to finish.** The
generated tail says so."

The generated tail still opens with "When this step is complete, call `finish`
with: ..." and only afterwards adds the exception "do not call `finish` after
it" (`internal/generate/task_prompt.go:65-77`). So the sentence overstates what
changes, and a reader comparing it against their own compiled prompt will think
something is broken.

**Recommendation.** Say what actually happens: "The generated tail adds a line
saying the named tools end the step by themselves and that `finish` is not to
be called after one of them succeeds. Your own instructions should not
contradict it."

### 16. The history table says Pipecat refuses `summary`. SLNG refuses all of them.

`tasks.mdx:84`. The bigger fact is missing: on the SLNG target a task has
nowhere to run at all, so every history value is refused, `messages` included.

### 17. Nothing in the Orchestration section says tasks need a code target

The SLNG target writes one agent with one prompt. Tasks, task groups and
handoffs are all refused there. That fact appears once, in a table row on
`targets/slng.mdx:324`, and nowhere in the five orchestration pages.

A reader who picked SLNG in `unmute init` and then read this whole section
learns at the end that none of it applies to them.

**Recommendation.** One `<Note>` at the top of the Orchestration overview:
"These three shapes need a code target — Pipecat or LiveKit Agents. The SLNG
target compiles one agent with one prompt, so it refuses tasks, task groups and
handoffs." The refusal message itself is good and says what to do instead; quote
it.

### 18. Task `think:` is documented with no target caveat

`agent-yaml.mdx:456` documents a task's own `think:` with no warning. It is
refused on Pipecat and on SLNG, and the refusal explains why. It also falls back
to the **entry** agent's think profile, not the defining agent's, which is
surprising enough to write down.

### 19. `task-groups.mdx` field table is missing `announce:`

A task group takes `announce:` (`agent-yaml.mdx:545`), and the field table at
`task-groups.mdx:65-72` does not list it. Since the page's own advice is about
covering the wait when the caller is moved between steps, this is the field a
reader most wants there.

### 20. Two small claims that the compiler does not actually enforce

- `task-groups.mdx:63`: "Each `steps` entry names a task **already attached to
  the agent**." The compiler only requires the task to exist in the package; the
  group itself makes it reachable. A reader who follows the sentence writes a
  redundant `tasks:` list.
- `task-groups.mdx:66`: `when` is marked required. Nothing enforces it on a
  group.

Either enforce them or soften the wording. "Required" that is not required is
the kind of thing that teaches a reader to distrust the whole table.

### 21. `announce:` behaves differently on tasks than on handoffs and tools

A `{{placeholder}}` in a handoff `announce:` or a tool `announce:` is refused at
build time. In a task or task-group `announce:` it is not refused; the text is
passed through as written, so the braces would be spoken.

**Recommendation.** Decide which behaviour is intended, then say it on the page.
Right now the reference says "no `{{variables}}`" for a tool announce and says
nothing for a task announce, which reads like the task version supports them.

### 22. The coding skill describes the old salon too

`internal/skill/assets/references/orchestration.md:745-759`, "The shapes, as
packages", says three things that are no longer true:

- "two tasks nested in the concierge" — there are three;
- "a bare name that lets the complaint specialist run the same verification
  task" — the specialist deliberately does **not** list `verify_customer` any
  more, and the package carries a comment saying why;
- "Task groups have no package either" — `book` is one.

This one is worth fixing first, because it is what a coding assistant reads
before it writes somebody's package. Right now it will teach the shared-task
pattern the repo removed on purpose.

---

## Part 3 — Words

Same idea, several names. This is the thing that makes the docs feel harder than
they are. Fixing it is cheap and helps every page.

| Idea | Words in use today | Suggested one word |
|---|---|---|
| the returning unit of work | task, step, focused step, bounded piece of work, flow | **task**, and **step** only for a member of a task group |
| the multi-step construct | task group, flow, workflow, sequence | **task group** |
| the agent that called it | owner, owning agent, the agent above it, the agent that called it | **owner** |
| saved data | variable, declared state, saved value, saved state, call state | **saved value** in guides, **variable** for the declaration |
| ending a task | finish, complete, end the step, return control, reports back | **finish** (see note 4) |
| going to a person | escalation, human transfer, transfer | key is `escalations:`; page is "Human transfers"; pick one name |

Two cases worth calling out:

- **"step" is ambiguous.** `tasks.mdx` switches from "task" to "step" halfway
  down, at the `finish:` section, and never says they are the same thing.
  `best-practices/step-scoping` is titled "Making a step actually run" and uses
  "step" 41 times against "task" 7 times. A reader can easily believe a step is
  a fourth concept.
  **Recommendation.** Define it once, on the Tasks page: "A task is a step. When
  a task runs inside a task group we call it a step of that group." Then keep to
  it.

- **"confirmed" is not the same as "verified".** `skip_when_confirmed:` reads a
  real, compiler-tracked property that `confirm:` sets on a variable. But
  `when: ...once the caller is verified` and `...once the caller is identified`
  are ordinary prose with no runtime meaning. The docs use all three words in
  the same voice. Only `best-practices/state-design.mdx:113` warns that they
  differ, and that is a page a task-focused reader may never reach.
  **Recommendation.** On the Task groups page, next to `skip_when_confirmed:`,
  add: "Confirmed here is a real mark on the variable, set by `confirm:`. It is
  not the same as a prompt saying the caller was verified." And link `confirm:`
  from that sentence — it is currently unlinked, at `task-groups.mdx:91`.

---

## Part 4 — Links

The orchestration pages are nearly closed off from the pages that explain how to
use them well.

- **`task-groups.mdx` has one outbound link in the whole page** (the footer
  card). `confirm:`, `unserved_request` and `reset` are all used unlinked.
- **`tasks.mdx` never links to `best-practices/step-scoping`**, which is the
  page explaining why a task you declared never runs. The link exists in the
  other direction only.
- **`tasks.mdx` never links to `best-practices/state-design`**, which is the
  page about choosing what to `assign:`.
- **`best-practices/context-scope` is a hard dead end.** It is the last page of
  this journey and ends on a link into the reference, with no card grid and no
  route onward.
- **`how-a-package-fits-together` links nothing at the point of use.** It
  introduces `tasks:`, `assign:`, `context:`, `variables:` and `prefetch:` in
  prose with no links; the only links are four footer cards.
- **`choosing-a-structure` never points at Best practices**, even though it
  links `step-scoping` twice mid-body.

**Recommendation.** A pass that links every key at its first use on a page, and
a "Where to go next" block on `task-groups` and `context-scope`.

---

## Part 5 — A picture would help

There is no diagram anywhere in `docs-site/`. Orchestration is the one place
where one earns its keep, because the whole subject is "who is holding the
caller, and does control come back".

**Recommendation.** Four small sequence diagrams on the Orchestration overview,
one per shape, all in the same style: tool, task, task with `finish:`, handoff.
Mintlify renders Mermaid from a ```mermaid fence, so no image files are needed.
The task-with-`finish:` picture in particular says in four arrows what the
current prose needs two paragraphs for.

While that page is open: it repeats the five-lists table from
`how-a-package-fits-together` almost word for word. Cut the repeat and link
instead, or drop the overview to a short chooser with the four diagrams.

---

## Part 6 — What to cut

You asked for no war stories, no internal numbers, no "we saw this on a call".
Four places have them, and in each case the rule underneath is better without
the story.

| Where | What it says now | Suggested |
|---|---|---|
| `best-practices/step-scoping.mdx:35` | "On a live call it recorded the complaint correctly and gave the caller a sensible next step, but `complaints` stayed empty." | "If the agent already holds every tool the step holds, it will usually finish the job without entering the step. The step's `assign:` then never runs and nothing is saved." |
| `best-practices/step-scoping.mdx:164` | "Until it did, one call resolved the contradiction by apologising to the caller instead of booking." | "When two prompts disagree, the model picks one, and you cannot tell which." |
| `best-practices/prompt-writing.mdx:40` | "On a live call the agent opened with 'I've got your number as…'" | State the rule: never write a specimen value in a prompt; the model may read it out as if it were the caller's. |
| `examples/salon-concierge/README.md:85` and the comment at `agent.yaml:87-93` | "A live call had the complaint specialist run it again … costing two model requests and fourteen seconds" | "Only the concierge verifies. A step within reach beats a prompt rule, so the step is not listed on the specialist." |

The evidence for each rule already lives where maintainers can find it — the
gate table in `CLAUDE.md`, the tests, the commit messages. The public page only
needs the rule.

Same principle for numbers: the Tasks page and the salon README both count model
requests saved. A reader building their first agent does not need the count;
they need to know that `finish:` removes a request, and that the effect is
bigger the more steps a call runs through.

---

## Part 7 — Smaller notes

- **`tasks.mdx` "Update an older task definition"** (lines 291-303) is migration
  material sitting in the middle of a teaching page. A first-time reader meets
  `result:`, `expect:` and `requires:` — three keys that do not exist — before
  reaching "Try it". Move it to the end of the page, or to the reference.
- **"Try it" points at a repo the reader may not have.** Both Tasks and Handoffs
  end with `unmute validate examples/salon-concierge`. Add the one-line note
  that this needs a clone, and give a `my-agent` version alongside it.
- **Nothing tells the reader what to look at once `unmute dev` is running.**
  Add one line: the task shows up in the dev page as its own row, so you can see
  it was entered. Link `/dev/overview`.
- **`best-practices/state-design.mdx:87` "Finish as soon as the work succeeds"**
  is prompt advice for a problem `finish:` now solves structurally. Add a line
  and a link: "Better still, name the tool under `finish:` and the step ends by
  itself."
- **`best-practices/state-design.mdx:100` "Reuse verification deliberately"**
  should mention `skip_when_confirmed:`, which is the declared version of the
  same idea.
- **The before/after pair is invisible.** `salon-concierge-single-prompt` is the
  same salon with no tasks, no handoffs and no variables, and
  `salon-concierge` is the structured one. That is exactly the ladder this
  review is about, and the docs mention the pair once, in passing, on
  `models/llm.mdx`. Name it on the Orchestration overview: "read one against the
  other to see what the structure bought."
- **`customer-intake` is never mentioned in `docs-site/` at all.** It is the
  smallest example with tasks and typed values, which makes it the natural
  example for a first-task page.

---

## Suggested order of work

1. Fix the wrong facts: notes 7, 8, 9, 10, 11, 15, 16, 20, 22. Most are one
   sentence each, and each one costs a reader trust in the rest. Note 22 first:
   it is what a coding assistant reads before writing somebody's package.
2. Reorder the Orchestration group and move `finish:` up the Tasks page: notes
   2 and 3. No new writing.
3. Write the ladder page: note 1. This is the real work.
4. The word pass and the link pass: Parts 3 and 4.
5. Diagrams and cuts: Parts 5 and 6.

Two house rules for whoever writes the text. `internal/docsite/prose_test.go`
refuses an em or en dash used as punctuation outside a code fence, and refuses a
page that explains how a fact was checked ("we measured", "Measured on 20.."),
so the wording suggested above has to be rewritten with commas and full stops.
`internal/docsite/version_test.go` refuses a version number on a page.

A note on ordering: because a change to emitted behaviour has to reach all four
surfaces in the same commit, whatever lands on `docs-site/build/orchestration/`
should be checked against `internal/skill/assets/references/orchestration.md` in
the same sitting. Today the skill is ahead of the public docs in two places: it
documents a task with no `when:`, and a task's own `think:`. It is behind in
one, note 22.
