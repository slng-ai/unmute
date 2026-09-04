package ir

import (
	"fmt"
	"slices"
	"strings"

	packagespec "github.com/slng-ai/unmute/internal/spec"
)

// Typed inputs, and the one place their resolution and their prompt block are
// written.
//
// A task or a handoff declares `input:`, a list of typed fields. The agent that
// runs the step or hands the caller over fills them from the conversation it
// heard, and the receiving prompt ends with a block naming each one. An input
// is not declared state: it is written to the call-state object for one visit,
// shown to the receiving prompt only, and gone after. That placement is what
// makes "the same on both targets" a property of seams that already exist,
// because every renderer on both targets reads that one object and nothing
// else. The compiler, not the runtime, keeps an input out of every other
// prompt: see checkTemplateSite.
//
// Composed here, above both drivers, for the same two reasons the state block
// is: one composer makes the block identical on both targets by construction,
// and appending it before checkTemplates walks the built prompts puts its
// placeholders into the names the router is given.

// InputBlockHeading opens the block. Distinct from the state block's, so the
// model can tell what the call has established from what it is being asked to
// do right now.
const InputBlockHeading = "Request:"

// InputBlockNote says what the lines below are: the brief for this visit, not
// a live record. The step hears any change of mind in its own conversation.
const InputBlockNote = "What you were handed for this visit. It does not change while you run."

// InputBlock composes the block for one list of inputs, in authored order.
// Returns "" for a site with none, which is what keeps appendPromptSuffix a
// byte-for-byte no-op for every package written before this existed.
func InputBlock(inputs []InputField) string {
	if len(inputs) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(InputBlockHeading)
	b.WriteString("\n")
	b.WriteString(InputBlockNote)
	b.WriteString("\n")
	for i, input := range inputs {
		// A flat placeholder, never a dotted path, for the reason the state
		// block gives: the emitted substitution regex tokenises flat identifiers.
		fmt.Fprintf(&b, "%d. %s: {{%s}}\n", i+1, stateBlockLabel(input.Name), input.Name)
	}
	return strings.TrimRight(b.String(), "\n")
}

// InputBlock is the block one prompt site ends with, for a reader that has the
// built agent rather than the field list: the runbook, and the gates.
func (a *Agent) InputBlock(site string) string {
	for name, def := range a.Agents {
		if AgentPromptSite(name) == site {
			return InputBlock(def.Inputs)
		}
	}
	for name, task := range a.Tasks {
		if TaskPromptSite(name) == site {
			return InputBlock(task.Inputs)
		}
	}
	return ""
}

// InputEmptyText is what an input with no value renders as. Not the state
// block's words: "none recorded yet" is wrong for a value nobody was asked to
// record, and "not given" tells the step the one thing it may ask for.
func InputEmptyText() string { return inputEmptyText }

const inputEmptyText = "not given."

// resolvedInputs is every input the package declares, by the site that
// declares it, plus each receiving agent's union.
type resolvedInputs struct {
	tasks    map[string][]InputField
	handoffs map[string][]InputField
	agents   map[string][]InputField
}

// buildInputs resolves every `input:` list and refuses the shapes that cannot
// work, naming the file, the line and what to do.
//
// The name checks matter more than they look. A placeholder is a flat
// identifier, so an input sharing a name with a variable would be ambiguous in
// every prompt; and an input lives on the call-state object for its visit, so
// the same name would overwrite the variable's value there. One name, one type
// across the whole package, for the same reason: the state object declares
// that field once.
func buildInputs(pkg *packagespec.Package, agent *Agent, shapes map[string]bool) (resolvedInputs, error) {
	out := resolvedInputs{
		tasks: map[string][]InputField{}, handoffs: map[string][]InputField{}, agents: map[string][]InputField{},
	}
	type owner struct {
		site string
		ref  *TypeRef
	}
	typesSeen := map[string]owner{}
	resolve := func(site string, fields []packagespec.Field) ([]InputField, error) {
		var inputs []InputField
		for _, field := range fields {
			name := field.Name
			at := locateInput(pkg, field)
			if !namePattern.MatchString(name) {
				return nil, fmt.Errorf("%s: %s input %q is not a name a prompt can read: write lowercase words joined by underscores, "+
					"because a {{placeholder}} is matched that way and nothing else", at, site, name)
			}
			clash := ""
			switch {
			case agent.Variables[name].Type != "" || slices.Contains(agent.VariableOrder, name):
				clash = "a declared variable"
			case slices.Contains(agent.Secrets, name):
				clash = "a declared secret"
			case shapes[name]:
				clash = "a declared shape"
			case reservedShapeName(name) != "":
				clash = "a word of the type grammar"
			case name == UnservedResultField:
				clash = "the reserved result field every finish takes"
			}
			if clash != "" {
				return nil, fmt.Errorf("%s: %s input %q is also %s. A prompt placeholder could not tell the two apart, and the "+
					"input would overwrite the other's value for the visit: rename the input", at, site, name, clash)
			}
			for _, earlier := range inputs {
				if earlier.Name == name {
					return nil, fmt.Errorf("%s: %s declares input %q twice, at %s and here. One entry per name",
						at, site, name, locateInput(pkg, packagespec.Field{Name: name, Type: earlier.Type.String()}))
				}
			}
			ref, err := resolveType(field.Type, shapes)
			if err != nil {
				return nil, fmt.Errorf("%s: %s input %q: %w", locateType(pkg, field.Type, "input:"), site, name, err)
			}
			if first, seen := typesSeen[name]; seen && !first.ref.Equal(ref) {
				return nil, fmt.Errorf("%s: %s input %q is declared as %s, and %s declares it as %s. An input is one field on "+
					"the call state for the whole package, so one name has one type: give the two the same type, or "+
					"different names", at, site, name, ref.String(), first.site, first.ref.String())
			}
			typesSeen[name] = owner{site: site, ref: ref}
			inputs = append(inputs, InputField{
				Name: name, Type: ref, Optional: ref.Optional, Description: strings.TrimSpace(field.Description),
			})
		}
		return inputs, nil
	}
	for _, name := range sortedKeys(pkg.Tasks) {
		inputs, err := resolve(fmt.Sprintf("task %q", name), pkg.Tasks[name].Input)
		if err != nil {
			return resolvedInputs{}, err
		}
		if len(inputs) > 0 {
			out.tasks[name] = inputs
		}
	}
	// A group is entered by one call, and there is no sound answer to which
	// step's inputs that call should carry. Refusing is Fail Loud; guessing
	// would be a silent downgrade.
	for _, group := range sortedKeys(pkg.Agent.TaskGroups) {
		for _, step := range pkg.Agent.TaskGroups[group].Steps {
			if len(out.tasks[step]) > 0 {
				return resolvedInputs{}, fmt.Errorf("%s: task %q declares input: and is a step of task group %q. A group is "+
					"entered by one call that cannot say which step a value is for, so a grouped task takes no inputs: "+
					"remove the input: list, or run the task on its own", pkg.Location("agent.yaml", "- "+step), step, group)
			}
		}
	}
	for _, name := range sortedKeys(pkg.Agent.Handoffs) {
		inputs, err := resolve(fmt.Sprintf("handoff %q", name), pkg.Agent.Handoffs[name].Input)
		if err != nil {
			return resolvedInputs{}, err
		}
		if len(inputs) == 0 {
			continue
		}
		out.handoffs[name] = inputs
		// The receiver's brief is the union over every handoff that targets it,
		// by handoff name and then each one's authored order, one entry per
		// name. The type check above already made a shared name one type.
		receiver := pkg.Agent.Handoffs[name].To
		for _, input := range inputs {
			if !slices.ContainsFunc(out.agents[receiver], func(f InputField) bool { return f.Name == input.Name }) {
				out.agents[receiver] = append(out.agents[receiver], input)
			}
		}
	}
	return out, nil
}

// locateInput is the line an input field sits on: the short form first, then
// the long form's name line, then the list itself.
func locateInput(pkg *packagespec.Package, field packagespec.Field) string {
	for _, needle := range []string{"- " + field.Name + ": " + field.Type, "name: " + field.Name, "input:"} {
		if at := pkg.Location("agent.yaml", needle); at != "agent.yaml" {
			return at
		}
	}
	return "agent.yaml"
}

// inputSites is every prompt site handed an input of this name, spelled the
// way checkTemplateSite spells a site, so the two cannot drift.
func inputSites(agent *Agent, name string) []string {
	var sites []string
	for _, task := range sortedKeys(agent.Tasks) {
		if slices.ContainsFunc(agent.Tasks[task].Inputs, func(f InputField) bool { return f.Name == name }) {
			sites = append(sites, TaskPromptSite(task))
		}
	}
	for _, def := range sortedKeys(agent.Agents) {
		if slices.ContainsFunc(agent.Agents[def].Inputs, func(f InputField) bool { return f.Name == name }) {
			sites = append(sites, AgentPromptSite(def))
		}
	}
	return sites
}

// checkToolInputReads decides which inputs a tool's `inject:` values and path
// may read, and refuses the two shapes that would send the wrong thing.
//
// An injected value is rendered from the call state when the model calls the
// tool, the way a variable is, so an input works there during its visit. Two
// things have to hold for that to be safe. The input must be required: an
// optional one may be absent for the whole visit, and an injected value cannot
// be asked for, so the request would carry the words a prompt renders for
// "not given". And every site the tool is attached to must be handed the
// input, because a tool on an agent as well as on its step is called from
// both, and the agent's call would inject nothing.
func checkToolInputReads(pkg *packagespec.Package, agent *Agent, tool string, raw packagespec.Tool) ([]string, error) {
	var texts []string
	for _, key := range sortedKeys(raw.Inject) {
		if text, ok := raw.Inject[key].(string); ok {
			texts = append(texts, text)
		}
	}
	if raw.Webhook != nil {
		texts = append(texts, raw.Webhook.Path)
	}
	file := "tools/" + tool + ".yaml"
	var allowed []string
	for _, text := range texts {
		for _, ref := range TemplateRefs(text) {
			if _, declared := agent.Variables[ref]; declared || len(inputSites(agent, ref)) == 0 {
				continue
			}
			for _, name := range sortedKeys(pkg.Agent.Agents) {
				if !slices.Contains(pkg.Agent.Agents[name].Tools, tool) {
					continue
				}
				if err := inputReadable(agent.Agents[name].Inputs, ref, fmt.Sprintf("agent %q", name)); err != nil {
					return nil, fmt.Errorf("%s: tool %q injects {{%s}}, %w", pkg.Location(file, "{{"+ref), tool, ref, err)
				}
			}
			for _, name := range sortedKeys(pkg.Tasks) {
				if !slices.Contains(pkg.Tasks[name].Tools, tool) {
					continue
				}
				if err := inputReadable(agent.Tasks[name].Inputs, ref, fmt.Sprintf("task %q", name)); err != nil {
					return nil, fmt.Errorf("%s: tool %q injects {{%s}}, %w", pkg.Location(file, "{{"+ref), tool, ref, err)
				}
			}
			allowed = append(allowed, ref)
		}
	}
	return allowed, nil
}

func inputReadable(inputs []InputField, name, site string) error {
	at := slices.IndexFunc(inputs, func(f InputField) bool { return f.Name == name })
	if at < 0 {
		return fmt.Errorf("and the tool is attached to %s, which is handed no %s. An injected value is read from the "+
			"call state when the tool runs, so every place the tool is attached has to be handed the input: declare "+
			"it there, or attach the tool only where it is", site, name)
	}
	if inputs[at].Optional {
		return fmt.Errorf("which %s declares as optional. An optional input may be absent for the whole visit, and an "+
			"injected value cannot be asked for, so the request would carry the words a prompt shows for a missing "+
			"one: make the input required, or let the model pass the value as a tool argument", site)
	}
	return nil
}
