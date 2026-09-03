package spec

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// TestNoNewDictionaryInTheAuthoringSurface holds the house rule in CLAUDE.md:
// **a new authored field is a list, never a map.**
//
// An entry with a name carries the name as a field (`- name: caller`); a mapping
// from one name to one value is a pair list whose every item holds exactly one
// key (`- customer_phone: result.value`), decoded by Pair.UnmarshalYAML so no
// map reaches the Go type.
//
// Two reasons, and the second is the one that bites. A map has no order a reader
// can see, so a file listing three things says nothing about which runs first.
// And a map field cannot carry a per-entry comment where anybody will find it.
//
// This is a **ratchet, not a migration.** Every map field that exists today is
// on the allowlist below. The permanent section never moves. The debt section may
// shrink and must never grow.
//
// internal/ir is deliberately out of scope: it is the resolved shape, not
// something a person writes.
func TestNoNewDictionaryInTheAuthoringSurface(t *testing.T) {
	var found []string
	walkAuthoringStructs(reflect.TypeOf(Package{}), map[reflect.Type]bool{}, func(owner reflect.Type, field reflect.StructField) {
		if field.Type.Kind() != reflect.Map {
			return
		}
		// `yaml:"-"` is not part of the authoring surface: the decoder never
		// touches it, so nobody writes it. Package.Markdown, Handlers, Documents
		// and files are all of this kind, holding file content keyed by path.
		if yamlName(field) == "-:" || field.Tag.Get("yaml") == "" {
			return
		}
		key := owner.Name() + "." + field.Name
		if _, ok := permanentDictionaries[key]; ok {
			return
		}
		if _, ok := dictionaryDebt[key]; ok {
			return
		}
		found = append(found, fmt.Sprintf("%s (%s) is a %s", key, yamlName(field), field.Type))
	})
	sort.Strings(found)
	if len(found) > 0 {
		t.Errorf(`%d map-typed field(s) in the authoring surface are on neither allowlist:

  %s

A new authored field is a list, never a map (CLAUDE.md, "No dictionaries in the
authoring surface"). Write it one of the two ways that already exist:

  - an entry with a name carries the name as a field:   - name: caller
  - a mapping from one name to one value is a pair list: - customer_phone: result.value
    with []Pair as its Go type, so Pair.UnmarshalYAML refuses an item holding two
    keys or none, which is what a dropped indent produces.

If the field really is JSON Schema or provider passthrough, add it to
permanentDictionaries with the reason. Do not add anything to dictionaryDebt:
that section may shrink and never grow.`, len(found), strings.Join(found, "\n  "))
	}
}

// TestDictionaryDebtNeverGrows pins the size of the debt list, so adding a map
// field and allowlisting it as debt in the same change fails too. Without this,
// the ratchet has a hole exactly the shape of the fix somebody reaches for first.
func TestDictionaryDebtNeverGrows(t *testing.T) {
	// 20, down from 26. AgentFile.Delegates, AgentFile.Tasks and Delegate.Assign
	// were deleted by the tasks/delegates refactor: tasks now nest inside the
	// agent that defines them, so there is no top-level catalog and no Delegate
	// type left to hold a debt entry. KnowledgeDef.Retrieval, ToolMCP.Headers and
	// ToolWebhook.Headers were already gone before that refactor; this allowlist
	// had simply never been swept for them. The number this test pins is still
	// the one reflection reports, not a hand count.
	const settled = 20
	if len(dictionaryDebt) > settled {
		t.Errorf("dictionaryDebt has %d entries, and it had %d when the rule was written. "+
			"This section may shrink and never grow: write the new field as a list instead",
			len(dictionaryDebt), settled)
	}
	if len(dictionaryDebt) < settled {
		t.Logf("dictionaryDebt is down to %d from %d. Lower the constant in this test so it cannot climb back.",
			len(dictionaryDebt), settled)
	}
}

// permanentDictionaries are the map fields that stay maps. A JSON Schema object
// *is* a dictionary, so converting one would stop it being one, and provider
// passthrough is a shape somebody else owns. These never move.
var permanentDictionaries = map[string]string{
	"Tool.Input":  "`input:` is JSON Schema. The model reads it as an object; a list would not be one.",
	"Tool.Output": "`output:` is JSON Schema, same reason.",
	"Task.Result": "`result:` is the task's own output schema, read the same way.",
	"ModelDef.Params": "`params:` is provider passthrough: the keys are whatever the " +
		"provider accepts, and unmute deliberately does not know them.",
}

// dictionaryDebt is every other map field that existed when the rule was
// written. Each carries a one-line reason and a "migrate when".
//
// This section may shrink. **It must never grow.** A new authored field is a
// list; see TestNoNewDictionaryInTheAuthoringSurface for the two shapes.
var dictionaryDebt = map[string]string{
	"Package.Tools":       "one file per tool, keyed by filename. Migrate when a tool carries its own name: field.",
	"Package.Connections": "one file per connection, same shape. Migrate with Package.Tools.",
	"Package.Targets":     "the target instance name is the deploy identity. Migrate when a target carries name:.",

	"AgentFile.Variables":    "name-keyed. Migrate when a variable carries name:, which touches every example.",
	"AgentFile.Destinations": "name to env var name. Migrate to a pair list; two examples author it.",
	"AgentFile.Knowledge":    "name-keyed base. Migrate with AgentFile.Variables.",
	"AgentFile.Agents":       "name-keyed, and the entry_agent: pointer reads the key. Migrate last.",
	"AgentFile.Handoffs":     "name-keyed catalog, same reason.",
	"AgentFile.Escalations":  "name-keyed catalog, same reason.",
	"AgentFile.TaskGroups":   "name-keyed catalog; an agent's task_groups: list points at a key.",
	"AgentFile.Channels":     "kind-keyed. Migrate when a channel carries kind: as a field.",

	"ModelSections.Think":  "name-keyed model catalog; listen:/turn: point at a key.",
	"ModelSections.Speak":  "same.",
	"ModelSections.Listen": "same.",
	"ModelSections.Turn":   "same.",

	"Tool.Inject":            "request key to scalar. Migrate to a pair list, the shape Task.Assign already uses.",
	"Connection.Environment": "route setting to env var name. Migrate to a pair list.",
	"Target.Models":          "per-target override, keyed like ModelSections. Migrate with them.",
	"Target.Destinations":    "per-target override of AgentFile.Destinations. Migrate with it, or the two shapes disagree.",
	"Target.Pins":            "dependency name to version. Migrate to a pair list.",
}

// walkAuthoringStructs visits every exported and unexported field of every struct
// reachable from root, so a map hidden three levels down inside a pointer or a
// slice element is still found. Unexported fields are walked too: Package.files
// is one, and a rule that skipped them would let the next one through.
func walkAuthoringStructs(t reflect.Type, seen map[reflect.Type]bool, visit func(reflect.Type, reflect.StructField)) {
	for t.Kind() == reflect.Pointer || t.Kind() == reflect.Slice || t.Kind() == reflect.Array {
		t = t.Elem()
	}
	if t.Kind() == reflect.Map {
		walkAuthoringStructs(t.Elem(), seen, visit)
		return
	}
	if t.Kind() != reflect.Struct || seen[t] {
		return
	}
	seen[t] = true
	for i := range t.NumField() {
		field := t.Field(i)
		visit(t, field)
		walkAuthoringStructs(field.Type, seen, visit)
	}
}

// yamlName is the key an author actually writes, which is what a failure message
// has to say: nobody greps for a Go field name when their YAML is refused.
func yamlName(field reflect.StructField) string {
	tag := field.Tag.Get("yaml")
	if tag == "" {
		return "no yaml tag"
	}
	return strings.Split(tag, ",")[0] + ":"
}
