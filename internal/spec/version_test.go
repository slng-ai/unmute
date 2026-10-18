package spec

import (
	"strings"
	"testing"
)

func TestVersionKeys(t *testing.T) {
	local := func(handler string) *ToolLocal { return &ToolLocal{Handler: handler} }
	for _, tc := range []struct {
		name string
		pkg  Package
		want string
	}{
		{"a version 2 agent key under version 1",
			Package{Agent: AgentFile{Version: 1, EntryBy: &EntryBy{Value: "caller_name"}}},
			"entry_by: is a version 2 key; write version: 2"},
		{"a version 2 key deep in a step under version 1",
			Package{Agent: AgentFile{Version: 1, Agents: map[string]AgentDef{"desk": {Tasks: []TaskItem{{Task: &Task{
				Finish: []FinishEntry{{Tool: "save", Confirm: []string{"Shall I?"}}}}}}}}}},
			"confirm: is a version 2 key"},
		{"a version 2 tool key under version 1",
			Package{Agent: AgentFile{Version: 1}, Tools: map[string]Tool{"find": {Local: local(""), Hidden: []string{"x"}}}},
			"hidden: is a version 2 key"},
		{"a handler naming a function under version 1",
			Package{Agent: AgentFile{Version: 1}, Tools: map[string]Tool{"find": {Local: local("tools/salon.py:find")}}},
			"names a function, which is a version 2 form"},
		{"a version 2 handler naming only a file",
			Package{Agent: AgentFile{Version: 2}, Tools: map[string]Tool{"find": {Local: local("tools/find.py")}}},
			"must be written file.py:function, as tools/find.py:find"},
		{"input: on a version 2 local tool",
			Package{Agent: AgentFile{Version: 2}, Tools: map[string]Tool{"find": {Local: local("tools/s.py:find"), Input: map[string]any{"type": "object"}}}},
			"a version 2 local tool takes no input:"},
		{"a precheck on a webhook tool",
			Package{Agent: AgentFile{Version: 2}, Tools: map[string]Tool{"find": {Webhook: &ToolWebhook{URLEnv: "U"}, Precheck: "tools/s.py:check"}}},
			"precheck runs before a local handler"},
		{"a precheck naming only a file",
			Package{Agent: AgentFile{Version: 2}, Tools: map[string]Tool{"find": {Local: local("tools/s.py:find"), Precheck: "tools/s.py"}}},
			"precheck \"tools/s.py\" must be written file.py:function"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.pkg.checkVersionKeys()
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v, want it to say %q", err, tc.want)
			}
		})
	}
}
