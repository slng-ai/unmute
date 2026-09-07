package spec

import (
	"encoding/json"
	"errors"
	"github.com/goccy/go-yaml"
	"slices"
	"strings"
	"testing"
)

func TestRetiredTaskFieldsGiveLocatedAdvice(t *testing.T) {
	for _, key := range []string{"result", "expect", "requires"} {
		var task Task
		err := yaml.UnmarshalWithOptions([]byte("name: choose\n"+key+": []\n"), &task, yaml.Strict())
		var located *PairError
		if !errors.As(err, &located) || located.Line != 2 || !strings.Contains(located.Msg, "assign") {
			t.Fatalf("%s: want located sharing advice, got %v", key, err)
		}
	}
	var handoff Handoff
	err := yaml.UnmarshalWithOptions([]byte("to: specialist\nexpect: []\n"), &handoff, yaml.Strict())
	var located *PairError
	if !errors.As(err, &located) || located.Line != 2 {
		t.Fatalf("handoff expect: %v", err)
	}
}

func TestToolSchemaMayNameResultAndExpect(t *testing.T) {
	var tool Tool
	if err := yaml.UnmarshalWithOptions([]byte("input:\n  type: object\n  properties:\n    result:\n      type: string\n    expect:\n      type: string\n"), &tool, yaml.Strict()); err != nil {
		t.Fatal(err)
	}
}

func TestRetiredFieldsAbsentFromAuthoringSchema(t *testing.T) {
	schema, err := Schema()
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(schema)
	if err != nil {
		t.Fatal(err)
	}
	var root any
	if err := json.Unmarshal(data, &root); err != nil {
		t.Fatal(err)
	}
	found := 0
	var walk func(any)
	walk = func(node any) {
		switch n := node.(type) {
		case []any:
			for _, value := range n {
				walk(value)
			}
		case map[string]any:
			props, _ := n["properties"].(map[string]any)
			_, task := props["assign"]
			_, handoff := props["to"]
			if task || handoff {
				found++
				for _, retired := range []string{"expect", "requires", "result"} {
					if _, ok := props[retired]; ok {
						t.Errorf("schema still publishes %s", retired)
					}
				}
			}
			for _, value := range n {
				walk(value)
			}
		}
	}
	walk(root)
	if found < 2 {
		t.Fatalf("did not find task/handoff schema: %d", found)
	}
}

func TestHistoryIsOptionalInAuthoringSchema(t *testing.T) {
	schema, err := Schema()
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(schema)
	if err != nil {
		t.Fatal(err)
	}
	var root any
	if err := json.Unmarshal(data, &root); err != nil {
		t.Fatal(err)
	}
	var walk func(any)
	walk = func(node any) {
		switch value := node.(type) {
		case []any:
			for _, item := range value {
				walk(item)
			}
		case map[string]any:
			props, _ := value["properties"].(map[string]any)
			required, _ := value["required"].([]any)
			if _, task := props["assign"]; task && slices.Contains(required, any("context")) {
				t.Error("task schema requires context; omitted history defaults to messages")
			}
			if _, context := props["history"]; context && slices.Contains(required, any("history")) {
				t.Error("context schema requires history; omitted history defaults to messages")
			}
			for _, item := range value {
				walk(item)
			}
		}
	}
	walk(root)
}

func TestInjectUsesOrderedPairs(t *testing.T) {
	var tool Tool
	err := yaml.UnmarshalWithOptions([]byte("inject:\n  - number: 0\n  - flag: false\n"), &tool, yaml.Strict())
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(tool.Inject)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(data), "[") {
		t.Fatalf("inject is not ordered: %s", data)
	}
}
