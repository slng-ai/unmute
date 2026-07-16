package ir

import "testing"

func TestSystemVariableSources(t *testing.T) { // V20, V21
	want := []SystemVariableSource{
		SystemVariableSourceCallID,
		SystemVariableSourceRoomName,
		SystemVariableSourceJobID,
		SystemVariableSourceAgentID,
		SystemVariableSourceAgentName,
		SystemVariableSourcePhoneNumber,
		SystemVariableSourceFirstUserMessage,
		SystemVariableSourceCallEndReason,
		SystemVariableSourceTranscriptMessages,
		SystemVariableSourceCurrentDatetime,
	}
	if len(SystemVariableSources) != len(want) {
		t.Fatalf("source count = %d, want %d", len(SystemVariableSources), len(want))
	}
	for i, source := range want {
		if SystemVariableSources[i] != source {
			t.Errorf("source[%d] = %q, want %q", i, SystemVariableSources[i], source)
		}
		if !source.IsValid() {
			t.Errorf("%q should be valid", source)
		}
	}
	if SystemVariableSource("env").IsValid() {
		t.Error("env must not be a system variable source")
	}
}
