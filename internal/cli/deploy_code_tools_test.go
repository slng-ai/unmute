package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/generate"
)

// The answers a stub voiceai gives, by subcommand, written as a shell `case`.
func codeToolStub(list, get, run string) string {
	return `case "$1 $2" in
"tool list") printf '%s' '` + list + `' ;;
"tool get") printf '%s' '` + get + `' ;;
"tool create"|"tool update") printf '{}' ;;
"tool run") printf '%s' '` + run + `' ;;
"tool publish") printf '{"published": true, "version_number": 3}' ;;
esac
`
}

func codeToolFiles(t *testing.T) []generate.File {
	t.Helper()
	body, err := json.Marshal(codeToolBody{Name: "book_slot", ToolType: "code", Description: "Book one time.", CodeSrc: "def book_slot(): ...\n"})
	if err != nil {
		t.Fatal(err)
	}
	return []generate.File{
		{Path: "agent.json", Content: []byte(`{}`)},
		{Path: "tools/book_slot.json", Content: body},
		{Path: "samples/book_slot.json", Content: []byte(`{"time": "10:00"}`)},
	}
}

func TestDeployPublishesALocalToolAsACodeTool(t *testing.T) {
	const succeeded = `{"status": "succeeded"}`
	for _, tc := range []struct {
		name, list, get, run string
		dryRun               bool
		wantCalls            []string
		wantOut, wantErr     string
		wantPending          bool
	}{
		{
			name: "a new tool is created, run on its sample, and published",
			list: `[]`, run: succeeded,
			wantCalls: []string{"tool list", "tool create", "tool run", "tool publish"},
			wantOut:   "published code tool book_slot v3",
		},
		{
			name: "a tool whose code has changed is updated",
			list: `[{"name": "book_slot", "scope": "organisation"}]`,
			get:  `{"tool_type": "code", "code_src": "old", "description": "Book one time.", "latest_version": 2, "is_current_version": false}`,
			run:  succeeded, wantCalls: []string{"tool list", "tool get", "tool update", "tool run", "tool publish"},
			wantOut: "published code tool book_slot v3",
		},
		{
			name:      "a current tool is left alone",
			list:      `[{"name": "book_slot", "scope": "organisation"}]`,
			get:       `{"tool_type": "code", "code_src": "def book_slot(): ...\n", "description": "Book one time.", "latest_version": 2, "is_current_version": true}`,
			wantCalls: []string{"tool list", "tool get"},
			wantOut:   "code tool book_slot is current (v2)",
		},
		{
			name: "a dry run changes nothing and stops the preview",
			list: `[]`, dryRun: true,
			wantCalls: []string{"tool list"}, wantOut: "would create code tool book_slot", wantPending: true,
		},
		{
			name:      "a name held by another kind of tool is refused",
			list:      `[{"name": "book_slot", "scope": "organisation"}]`,
			get:       `{"tool_type": "api_request"}`,
			wantCalls: []string{"tool list", "tool get"}, wantErr: "already has a api_request tool named book_slot",
		},
		{
			name: "a failed sample run stops before publish",
			list: `[]`, run: `{"status": "failed", "error": "boom"}`,
			wantCalls: []string{"tool list", "tool create", "tool run"}, wantErr: "its sample run failed",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bin, log := stubVoiceai(t, codeToolStub(tc.list, tc.get, tc.run))
			runner := newVoiceaiRunner(bin, os.Environ(), "")
			var out bytes.Buffer
			pending, err := publishCodeTools(&out, runner, "slng", codeToolFiles(t), tc.dryRun)
			if tc.wantErr == "" && err != nil {
				t.Fatal(err)
			}
			if tc.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tc.wantErr)) {
				t.Fatalf("error = %v, want one containing %q", err, tc.wantErr)
			}
			if !strings.Contains(out.String(), tc.wantOut) {
				t.Errorf("output %q does not say %q", out.String(), tc.wantOut)
			}
			if (len(pending) > 0) != tc.wantPending {
				t.Errorf("pending = %v, want pending %v", pending, tc.wantPending)
			}
			var got []string
			for _, call := range calls(t, log) {
				fields := strings.Fields(call)
				got = append(got, fields[0]+" "+fields[1])
			}
			if strings.Join(got, ", ") != strings.Join(tc.wantCalls, ", ") {
				t.Errorf("voiceai calls = %v, want %v", got, tc.wantCalls)
			}
		})
	}
}
