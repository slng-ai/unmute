package target

import (
	"strings"
	"testing"
)

// An omitted world part is LiveKit's plugin default and a TypeError in
// pipecat-slng 0.6.0, so the two frameworks part ways on exactly that case.
func TestParseSlngSpeechGateway(t *testing.T) {
	for _, tc := range []struct {
		fw       Provider
		params   map[string]any
		wantHost string
		wantErr  string
	}{
		{LiveKit, nil, "", ""},
		{Pipecat, nil, "", "params.world_part is required for SLNG speech on pipecat"},
		{LiveKit, map[string]any{"world_part": "eu-north"}, "eu-north.api.slng.ai", ""},
		{Pipecat, map[string]any{"world_part": "in"}, "in.api.slng.ai", ""},
		{Pipecat, map[string]any{"world_part": "eu"}, "", "replace legacy"},
		{Pipecat, map[string]any{"world_part": "in", "base_url": "in.api.slng.ai"}, "", "remove params.base_url"},
	} {
		gateway, err := ParseSlngSpeechGateway(tc.fw, tc.params)
		if tc.wantErr != "" {
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("%s %v: err %v, want %q", tc.fw, tc.params, err, tc.wantErr)
			}
			continue
		}
		if err != nil || gateway.Host() != tc.wantHost {
			t.Errorf("%s %v: host %q err %v, want %q", tc.fw, tc.params, gateway.Host(), err, tc.wantHost)
		}
	}
}
