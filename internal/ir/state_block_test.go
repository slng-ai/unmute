package ir

import (
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	packagespec "github.com/slng-ai/unmute/internal/spec"
)

// numberedLine matches one line of the composed block: a number, a dot, a
// label, and the placeholder naming the value.
var numberedLine = regexp.MustCompile(`^\d+\. .*\{\{[a-z_]+\}\}$`)

func typedStateAgent(t *testing.T) (*packagespec.Package, *Agent) {
	t.Helper()
	pkg, err := packagespec.Load(filepath.Join("..", "testdata", "typed_state"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	return pkg, agent
}

// TestStateBlockReachesEveryPromptInDeclarationOrder is SC-004 and the whole of
// FR-005: the block is on every agent prompt and every task prompt, in an order
// the author can predict, and no prompt file was edited to put it there.
func TestStateBlockReachesEveryPromptInDeclarationOrder(t *testing.T) {
	pkg, agent := typedStateAgent(t)
	if len(agent.Agents) == 0 || len(agent.Tasks) == 0 {
		t.Fatal("the fixture has no agents or no tasks, so this gate proves nothing")
	}
	type promptSite struct {
		where  string
		site   string
		prompt string
	}
	var sites []promptSite
	for name, def := range agent.Agents {
		sites = append(sites, promptSite{"agent " + name, AgentPromptSite(name), def.Instructions})
	}
	for name, task := range agent.Tasks {
		sites = append(sites, promptSite{"task " + name, TaskPromptSite(name), task.Instructions})
	}
	for _, at := range sites {
		where, prompt := at.where, at.prompt
		if !strings.Contains(prompt, StateBlockHeading) {
			t.Errorf("%s carries no state block:\n%s", where, prompt)
			continue
		}
		if !strings.Contains(prompt, StateBlockNote) {
			t.Errorf("%s carries the block with no line saying what it is for", where)
		}
		// Declaration order, and numbered from one. Sorting is what makes a
		// numbered list move under a reader who adds a field.
		var order []string
		for _, line := range strings.Split(prompt, "\n") {
			if !numberedLine.MatchString(line) {
				continue
			}
			opens := strings.Index(line, "{{")
			closes := strings.Index(line, "}}")
			if opens < 0 || closes < opens {
				continue
			}
			order = append(order, line[opens+2:closes])
		}
		want := []string{}
		for _, name := range agent.VariableOrder {
			if agent.Variables[name].Shape == nil {
				continue
			}
			// A value awaiting the caller's agreement belongs in one prompt only,
			// and that rule has its own gate below. Here it only decides what
			// this site's numbering should be.
			if step := agent.Variables[name].Confirm; step != "" && at.site != confirmingSite(step) {
				continue
			}
			want = append(want, name)
		}
		if !slices.Equal(order, want) {
			t.Errorf("%s numbers the values %v, want %v", where, order, want)
		}
	}
	// And no prompt file was edited: the authored markdown says none of it.
	for path, source := range pkg.Markdown {
		if strings.Contains(source, StateBlockHeading) {
			t.Errorf("%s already contains the block, so this gate is proving the author wrote it", path)
		}
	}
}

// TestStateBlockOmitsUnconfirmedAtEveryOtherSite is refusal 16, reproduced, and
// it is the only thing standing where that refusal stands for authored prompts.
//
// checkTemplates walks the raw authored markdown and never sees a composed
// block, so nothing else stops a naive composer rendering an unconfirmed phone
// number into every prompt on the call. That is exactly the outcome refusal 16
// exists to prevent, and its own comment names the worst case: greeting a
// stranger by the account holder's name.
func TestStateBlockOmitsUnconfirmedAtEveryOtherSite(t *testing.T) {
	_, agent := typedStateAgent(t)
	var confirming, unconfirmed string
	for _, name := range agent.VariableOrder {
		if step := agent.Variables[name].Confirm; step != "" {
			unconfirmed, confirming = name, step
		}
	}
	if unconfirmed == "" {
		t.Fatal("the fixture declares no value awaiting confirmation, so this gate proves nothing")
	}
	block := agent.StateBlock(TaskPromptSite(confirming))
	if !strings.Contains(block, "{{"+unconfirmed+"}}") {
		t.Errorf("the confirming step's own block omits %q, which is the one prompt it belongs in:\n%s",
			unconfirmed, block)
	}
	for name := range agent.Agents {
		if got := agent.StateBlock(AgentPromptSite(name)); strings.Contains(got, "{{"+unconfirmed+"}}") {
			t.Errorf("agent %q renders %q, which the caller has not agreed to yet:\n%s", name, unconfirmed, got)
		}
	}
	for name := range agent.Tasks {
		if name == confirming {
			continue
		}
		if got := agent.StateBlock(TaskPromptSite(name)); strings.Contains(got, "{{"+unconfirmed+"}}") {
			t.Errorf("task %q renders %q, which the caller has not agreed to yet:\n%s", name, unconfirmed, got)
		}
	}
	// The site string is the one the validator uses, spelled once. Two spellings
	// is how the filter would stop matching without anything failing.
	if confirmingSite(confirming) != TaskPromptSite(confirming) {
		t.Errorf("the composer's confirming site %q is not the validator's %q",
			confirmingSite(confirming), TaskPromptSite(confirming))
	}
}

// TestStateBlockNamesNoDeclaredSecret is FR-012 where it is cheapest to break:
// the block goes to a model and into a trace, so a secret in it is a secret in
// both.
func TestStateBlockNamesNoDeclaredSecret(t *testing.T) {
	_, agent := typedStateAgent(t)
	if len(agent.Secrets) == 0 {
		t.Fatal("the fixture declares no secrets, so this gate proves nothing")
	}
	// The state a mistake would produce: a declared secret that also happens to
	// be a declared value. Nothing should let it into a block.
	secret := agent.Secrets[0]
	agent.Variables[secret] = Variable{Type: PrimitiveString, Shape: &TypeRef{Shaped: ShapedID}}
	agent.VariableOrder = append(agent.VariableOrder, secret)

	for name := range agent.Agents {
		if got := agent.StateBlock(AgentPromptSite(name)); strings.Contains(got, secret) {
			t.Errorf("agent %q renders the declared secret %q into its prompt:\n%s", name, secret, got)
		}
	}
	for name := range agent.Tasks {
		if got := agent.StateBlock(TaskPromptSite(name)); strings.Contains(got, secret) {
			t.Errorf("task %q renders the declared secret %q into its prompt:\n%s", name, secret, got)
		}
	}
}

// TestStateBlockIsEmptyForAPackageWithNoShapes is the FR-015 half at this seam:
// an empty block is what makes appendPromptSuffix return the prompt byte for
// byte, so every package written before this feature is untouched.
func TestStateBlockIsEmptyForAPackageWithNoShapes(t *testing.T) {
	agent, err := Build(loadSafeCore(t))
	if err != nil {
		t.Fatal(err)
	}
	for name := range agent.Agents {
		if got := agent.StateBlock(AgentPromptSite(name)); got != "" {
			t.Errorf("agent %q composed a block for a package declaring no shapes:\n%s", name, got)
		}
		if strings.Contains(agent.Agents[name].Instructions, StateBlockHeading) {
			t.Errorf("agent %q's prompt grew a block with nothing declared", name)
		}
	}
	for name := range agent.Tasks {
		if got := agent.StateBlock(TaskPromptSite(name)); got != "" {
			t.Errorf("task %q composed a block for a package declaring no shapes:\n%s", name, got)
		}
	}
}

// TestStateBlockNamesNoDottedPath holds decision 9: a placeholder in the block
// names a whole declared value.
//
// The emitted substitution regex tokenises flat identifiers only, so a dotted
// name is not substituted and survives into the prompt as literal text. That is
// silently wrong today rather than refused, so the block must never write one.
func TestStateBlockNamesNoDottedPath(t *testing.T) {
	_, agent := typedStateAgent(t)
	for name := range agent.Agents {
		for _, ref := range TemplateRefs(agent.StateBlock(AgentPromptSite(name))) {
			if strings.Contains(ref, ".") {
				t.Errorf("agent %q's block names {{%s}}, which no render path substitutes", name, ref)
			}
		}
	}
	for name := range agent.Tasks {
		for _, ref := range TemplateRefs(agent.StateBlock(TaskPromptSite(name))) {
			if strings.Contains(ref, ".") {
				t.Errorf("task %q's block names {{%s}}, which no render path substitutes", name, ref)
			}
		}
	}
}
