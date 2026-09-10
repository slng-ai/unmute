package generate

import (
	"slices"

	"github.com/slng-ai/unmute/internal/ir"
)

// reportTaskDetail is one step that does something a reader cannot see from the
// package's task list: it ends on its own tool, or it opens by listening.
//
// A separate block rather than a shape change to `tasks`, which is a list of
// names every report has carried: a reader parsing that list keeps working, and
// a package writing none of spec 010's keys writes exactly the report it wrote
// before.
type reportTaskDetail struct {
	Name    string   `json:"name"`
	EndsOn  []string `json:"ends_on,omitempty"`
	Opening string   `json:"opening,omitempty"`
}

// reportTaskGroupStep is one step of a group, named only when the group may skip
// it. A group whose steps all always run is not reported at all.
type reportTaskGroupStep struct {
	Task              string `json:"task"`
	SkipWhenConfirmed string `json:"skip_when_confirmed,omitempty"`
}

type reportTaskGroup struct {
	Name  string                `json:"name"`
	Steps []reportTaskGroupStep `json:"steps"`
}

// reportTaskDetails is every task that ends on a tool or opens by listening, by
// name, and nil when the package has none.
func reportTaskDetails(agent *ir.Agent) []reportTaskDetail {
	var out []reportTaskDetail
	for _, name := range sortedKeys(agent.Tasks) {
		task := agent.Tasks[name]
		detail := reportTaskDetail{Name: name, EndsOn: task.EndsOnTools()}
		if task.Opening != "" && task.Opening != ir.OpeningGenerate {
			detail.Opening = string(task.Opening)
		}
		if len(detail.EndsOn) == 0 && detail.Opening == "" {
			continue
		}
		out = append(out, detail)
	}
	return out
}

// reportTaskGroups is every group holding a step the group may skip, and nil
// when the package has none.
func reportTaskGroups(agent *ir.Agent) []reportTaskGroup {
	var out []reportTaskGroup
	for _, name := range sortedKeys(agent.TaskGroups) {
		group := agent.TaskGroups[name]
		if !slices.ContainsFunc(group.Steps, func(step ir.GroupStep) bool { return step.SkipWhenConfirmed != "" }) {
			continue
		}
		reported := reportTaskGroup{Name: name}
		for _, step := range group.Steps {
			reported.Steps = append(reported.Steps, reportTaskGroupStep{
				Task: step.Task, SkipWhenConfirmed: step.SkipWhenConfirmed,
			})
		}
		out = append(out, reported)
	}
	return out
}
