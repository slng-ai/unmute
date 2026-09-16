---
paths:
  - "docs-site/**"
---

# Writing a docs-site page

These are gated in `internal/docsite/`. The full table is [`docs/GATES.md`](../../docs/GATES.md).

- **Plain words, short sentences.** No prose sentence runs past 40 words; the
  site's mean is 16. A sentence that trips the gate is nearly always two.
- **No em or en dash as punctuation** outside a code fence.
- **Never say how a fact was checked.** No "we measured", no "Measured on
  20..", no "Where this page's facts come from".
- **A guide page over 150 lines opens with an "On this page:" list** of its own
  H2s, in both directions. That slot is what a reader uses to decide they are
  on the right page, and it is the first thing lost as a page grows. Reference
  sections are exempt: they lead with the complete list instead.
- **A page showing an authored YAML block states that block's keys with
  `ParamField`, and every `ParamField` declares a type.** The type is where a
  reader learns the allowed values. Exemptions are a named list with a reason
  each; it may shrink and never grows.
- **Valid MDX.** An attribute value is quoted or braced, and a closing tag for
  an element spanning a blank line starts its own line. `mint dev` drops a page
  it cannot parse out of the navigation while every Go gate stays green.
- **Every internal link resolves**, to a page and to a heading, counting
  `<Card href>` as well as `](...)`. Most navigation here is a card.
- **Every link into this repo's tree on GitHub points at a path that exists.**
  Renaming an example renames a directory.
- **No page states a version**, by a literal or by rendering the release
  automation's marker. The changelog is generated from GitHub Releases; never
  hand-edit an entry.
- **No page quotes CLI output the CLI no longer prints.** A reader who copies a
  stale sample and waits for it cannot tell a stale doc from a broken install.
- **Any table that mirrors a Go table** (routes, transfers, tools, turn
  deciders, world parts, prefetch) is checked against it both ways. Change the
  Go table, change the page in the same commit.
