package tui

import (
	"github.com/slng-ai/unmute/internal/scaffold"
	"github.com/slng-ai/unmute/internal/spec"
	"path/filepath"
	"reflect"
	"testing"
)

func TestMaintainKeepsOrderedInjection(t *testing.T) {
	root := filepath.Join(t.TempDir(), "pkg")
	pairs := []spec.Pair{{Key: "label", Value: "fixed"}, {Key: "count", Value: uint64(0)}, {Key: "enabled", Value: false}}
	data := scaffold.Data{Name: "pkg", AgentName: "injection", Instructions: "Help the caller.", Tools: []scaffold.Tool{{Name: "lookup", Description: "Look up.", Execution: "webhook", URLEnv: "LOOKUP_URL", Input: `{"type":"object","properties":{}}`, Inject: pairs}}}
	data.SetTarget("livekit")
	if _, err := scaffold.Write(root, data); err != nil {
		t.Fatal(err)
	}
	first, err := loadMaintained(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.data.Tools) != 1 || !reflect.DeepEqual(first.data.Tools[0].Inject, pairs) {
		t.Fatalf("lost inject pairs: %+v", first.data.Tools)
	}
	again := filepath.Join(t.TempDir(), "again")
	if _, err := scaffold.Write(again, first.data); err != nil {
		t.Fatal(err)
	}
	second, err := loadMaintained(again)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first.data.Tools[0].Inject, second.data.Tools[0].Inject) {
		t.Fatal("second rewrite lost injection")
	}
}
