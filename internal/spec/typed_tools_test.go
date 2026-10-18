package spec

import (
	"strings"
	"testing"
)

func TestToolForms(t *testing.T) {
	local := func(handler string) *ToolLocal { return &ToolLocal{Handler: handler} }
	for _, tc := range []struct {
		name string
		pkg  Package
		want string
	}{
		{"a handler naming only a file",
			Package{Tools: map[string]Tool{"find": {Local: local("tools/salon.py")}}},
			"must be written file.py:function, as tools/salon.py:find"},
		{"a handler naming no file",
			Package{Tools: map[string]Tool{"find": {Local: local("find")}}},
			"must be written file.py:function, as tools/find.py:find"},
		{"input: on a local tool",
			Package{Tools: map[string]Tool{"find": {Local: local("tools/s.py:find"), Input: map[string]any{"type": "object"}}}},
			"a local tool takes no input:"},
		{"a precheck on a webhook tool",
			Package{Tools: map[string]Tool{"find": {Webhook: &ToolWebhook{URLEnv: "U"}, Precheck: "tools/s.py:check"}}},
			"precheck runs before a local handler"},
		{"a precheck naming only a file",
			Package{Tools: map[string]Tool{"find": {Local: local("tools/s.py:find"), Precheck: "tools/s.py"}}},
			"precheck \"tools/s.py\" must be written file.py:function"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.pkg.checkTools()
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v, want it to say %q", err, tc.want)
			}
		})
	}
}
