package spec

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/goccy/go-yaml"
	"github.com/goccy/go-yaml/ast"
)

// Item is one entry of an agent's handoffs:, transfers: or flows: list: a
// block that defines one, or a bare name that refers to one another agent
// defines. Each is defined once across the package, the way a task is.
type Item[T any] struct {
	Ref string
	Def *T
}

// UnmarshalYAML accepts both shapes, as TaskItem does.
func (i *Item[T]) UnmarshalYAML(unmarshal func(any) error) error {
	var ref string
	if err := unmarshal(&ref); err == nil {
		i.Ref = ref
		return nil
	}
	var def T
	if err := unmarshal(&def); err != nil {
		return err
	}
	i.Def = &def
	return nil
}

// MarshalYAML writes a bare name back as the bare name the author wrote.
func (i Item[T]) MarshalYAML() (any, error) {
	if i.Def != nil {
		return i.Def, nil
	}
	return i.Ref, nil
}

// Transfer is one entry of an agent's transfers: the caller goes through to a
// person, at the number an environment variable holds. It is cold, and
// brief: makes it warm: the agent briefs the person before it drops out.
type Transfer struct {
	Name string `json:"name" yaml:"name"`
	// To names the environment variable holding the number.
	To            string `json:"to" yaml:"to"`
	When          string `json:"when,omitempty" yaml:"when,omitempty"`
	RingTimeout   string `json:"ring_timeout,omitempty" yaml:"ring_timeout,omitempty"`
	OnUnavailable string `json:"on_unavailable,omitempty" yaml:"on_unavailable,omitempty"`
	Brief         string `json:"brief,omitempty" yaml:"brief,omitempty"`
}

// topLevelMoved are agent.yaml keys that moved inside each agent.
var topLevelMoved = map[string]string{
	"handoffs":     "handoffs: moved inside the agent that hands off. Write it under that agent as - name: <handoff>, then to: and when:",
	"escalations":  "escalations: is transfers: now, inside the agent that transfers. Write - name: <transfer>, to: <ENV_VAR_WITH_THE_NUMBER>, when:",
	"task_groups":  "task_groups: is flows: now, inside the agent that runs it. Write - name: <flow>, when:, steps:",
	"destinations": "destinations: is gone. A transfer's to: names the environment variable holding the number",
}

// agentMoved are agent keys that were renamed or replaced.
var agentMoved = map[string]string{
	"instructions": "instructions: is called prompt: now",
	"task_groups":  "task_groups: is flows: now. Write the flow here as - name: <flow>, when:, steps:, or name one another agent defines",
	"escalations":  "escalations: is transfers: now. Write - name: <transfer>, to: <ENV_VAR_WITH_THE_NUMBER>, when:, or name one another agent defines",
}

// prefetchMoved, handoffMoved, groupMoved and greetingMoved are renamed keys.
var (
	prefetchMoved = map[string]string{
		"tool":   "tool: is called call: now",
		"args":   "args: is called with: now, and takes state.<name> or a literal",
		"assign": "assign: is called save: now",
	}
	handoffMoved = map[string]string{
		"announce": "announce: is called say: now, a list",
		"saves":    "saves: is called save: now",
	}
	groupMoved = map[string]string{
		"announce":  "announce: is called say: now",
		"arguments": "arguments: is called fills: now",
	}
	greetingMoved = map[string]string{
		"text": "text: is called say: now",
	}
)

func (a *AgentFile) UnmarshalYAML(node ast.Node) error {
	if err := removedKeys(node, topLevelMoved); err != nil {
		return err
	}
	if mapping, ok := node.(*ast.MappingNode); ok {
		for _, entry := range mapping.Values {
			if entry.Key.String() == "agents" {
				if _, isMap := entry.Value.(*ast.MappingNode); isMap {
					return &PairError{Line: entry.Key.GetToken().Position.Line,
						Msg: "agents: is a list now: write each agent as - name: <agent>, then its keys"}
				}
			}
		}
	}
	type plainAgentFile AgentFile
	return yaml.NodeToValue(node, (*plainAgentFile)(a), yaml.Strict())
}

func (d *AgentDef) UnmarshalYAML(node ast.Node) error {
	if err := removedKeys(node, agentMoved); err != nil {
		return err
	}
	type plainAgentDef AgentDef
	return yaml.NodeToValue(node, (*plainAgentDef)(d), yaml.Strict())
}

func (p *Prefetch) UnmarshalYAML(node ast.Node) error {
	if err := removedKeys(node, prefetchMoved); err != nil {
		return err
	}
	type plainPrefetch Prefetch
	return yaml.NodeToValue(node, (*plainPrefetch)(p), yaml.Strict())
}

func (g *TaskGroup) UnmarshalYAML(node ast.Node) error {
	if err := removedKeys(node, groupMoved); err != nil {
		return err
	}
	type plainGroup TaskGroup
	return yaml.NodeToValue(node, (*plainGroup)(g), yaml.Strict())
}

func (g *Greeting) UnmarshalYAML(node ast.Node) error {
	if err := removedKeys(node, greetingMoved); err != nil {
		return err
	}
	type plainGreeting Greeting
	return yaml.NodeToValue(node, (*plainGreeting)(g), yaml.Strict())
}

// lowerStructure fills the name-keyed views the compiler reads from what an
// author writes: the agents: list, each agent's handoffs:, transfers: and
// flows:, and the renamed keys. A handoff, transfer or flow is defined once,
// by the agent that writes its block, and any agent may name it.
func (p *Package) lowerStructure() error {
	agent := &p.Agent
	agent.Agents = map[string]AgentDef{}
	agent.Handoffs = map[string]Handoff{}
	agent.Escalations = map[string]Escalation{}
	agent.TaskGroups = map[string]TaskGroup{}
	agent.Destinations = map[string]string{}
	definedBy := map[string]string{}
	define := func(kind, name, owner string) error {
		if name == "" {
			return fmt.Errorf("%s: a %s of agent %q has no name:; write - name: <%s>", p.KeyLocation("agent.yaml", "agents"), kind, owner, kind)
		}
		if first, taken := definedBy[name]; taken {
			return fmt.Errorf("%s: %q is defined by agent %q and again by agent %q. A name is one name across the package: keep one definition and let the other agent name it, - %s",
				p.Location("agent.yaml", "name: "+name), name, first, owner, name)
		}
		definedBy[name] = owner
		return nil
	}
	refs := map[string][]string{} // kind -> names referred to, checked once all are defined
	for _, def := range agent.AgentList {
		if def.Name == "" {
			return fmt.Errorf("%s: an agent has no name:; write each agent as - name: <agent>", p.KeyLocation("agent.yaml", "agents"))
		}
		if _, taken := agent.Agents[def.Name]; taken {
			return fmt.Errorf("%s: two agents are named %q; give each a name of its own", p.Location("agent.yaml", "name: "+def.Name), def.Name)
		}
		def.Instructions = def.Prompt
		def.Handoffs, def.Escalations, def.TaskGroups = nil, nil, nil
		for _, item := range def.HandoffItems {
			if item.Def == nil {
				def.Handoffs = append(def.Handoffs, item.Ref)
				refs["handoff"] = append(refs["handoff"], item.Ref)
				continue
			}
			handoff := *item.Def
			if err := define("handoff", handoff.Name, def.Name); err != nil {
				return err
			}
			agent.Handoffs[handoff.Name] = handoff
			def.Handoffs = append(def.Handoffs, handoff.Name)
		}
		for _, item := range def.TransferItems {
			if item.Def == nil {
				def.Escalations = append(def.Escalations, item.Ref)
				refs["transfer"] = append(refs["transfer"], item.Ref)
				continue
			}
			transfer := *item.Def
			if err := define("transfer", transfer.Name, def.Name); err != nil {
				return err
			}
			agent.Destinations[transfer.Name] = transfer.To
			escalation := Escalation{When: transfer.When}
			if transfer.Brief != "" {
				escalation.Warm = &WarmTransfer{Destination: transfer.Name, Briefing: transfer.Brief, RingTimeout: transfer.RingTimeout, OnUnavailable: transfer.OnUnavailable}
			} else {
				escalation.Cold = &ColdTransfer{Destination: transfer.Name, RingTimeout: transfer.RingTimeout, OnUnavailable: transfer.OnUnavailable}
			}
			agent.Escalations[transfer.Name] = escalation
			def.Escalations = append(def.Escalations, transfer.Name)
		}
		for _, item := range def.FlowItems {
			if item.Def == nil {
				def.TaskGroups = append(def.TaskGroups, item.Ref)
				refs["flow"] = append(refs["flow"], item.Ref)
				continue
			}
			flow := *item.Def
			if err := define("flow", flow.Name, def.Name); err != nil {
				return err
			}
			agent.TaskGroups[flow.Name] = flow
			def.TaskGroups = append(def.TaskGroups, flow.Name)
		}
		for _, task := range def.Tasks {
			if task.Task != nil {
				task.Task.Instructions = task.Task.Prompt
			}
		}
		agent.Agents[def.Name] = def
	}
	for kind, names := range refs {
		for _, name := range names {
			if _, ok := definedBy[name]; !ok {
				return fmt.Errorf("%s: an agent names the %s %q, and no agent defines it; write it as a block under one agent, - name: %s", p.Location("agent.yaml", "- "+name), kind, name, name)
			}
		}
	}
	for i := range agent.Prefetch {
		for j, pair := range agent.Prefetch[i].Args {
			if text, ok := pair.Value.(string); ok && strings.HasPrefix(text, statePrefix) {
				agent.Prefetch[i].Args[j].Value = "{{" + text + "}}"
			}
		}
	}
	return nil
}

// toolFiles names every tool: one per tools/<name>.yaml, sorted. The folder is
// the list of tools; agent.yaml's top-level tools: holds only the tools that
// have behaviour on every agent, so a bare name there is refused, and an entry
// has to name a file.
func (p *Package) toolFiles() ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(p.Root, "tools"))
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	var names []string
	for _, entry := range entries {
		name, ok := strings.CutSuffix(entry.Name(), ".yaml")
		if !ok || entry.IsDir() || strings.Contains(name, ".") {
			continue
		}
		names = append(names, name)
	}
	slices.Sort(names)
	for _, entry := range p.Agent.Tools {
		where := p.Location("agent.yaml", entry.Name)
		if entry.Bare() {
			return nil, fmt.Errorf("%s: tools: lists %s with nothing about it. Every file in tools/ is a tool already. Keep an entry here only to give it outcomes, save, hidden or start", where, entry.Name)
		}
		if !slices.Contains(names, entry.Name) {
			return nil, fmt.Errorf("%s: tools: has an entry for %s, and there is no tools/%s.yaml", where, entry.Name, entry.Name)
		}
	}
	return names, nil
}
