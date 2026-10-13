package web

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/devmetrics"
)

// TestMetricsGivesEachCallerTurnItsOwnReply runs the page's own callTurns over a
// typed LiveKit call. LiveKit opens a reply before its input commits, so a reply
// sorts ahead of the input it answers. Placed in arrival order, every reply
// landed one turn early and the last turn showed nothing. Node runs the function
// as shipped; without Node the test skips, the way the browser smoke does.
func TestMetricsGivesEachCallerTurnItsOwnReply(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not available")
	}
	source := page(t)
	start := strings.Index(source, "function callTurns(")
	if start < 0 {
		t.Fatal("index.html has no callTurns")
	}
	end := strings.Index(source[start:], "\n}\n")
	if end < 0 {
		t.Fatal("callTurns has no closing brace at column zero")
	}
	fn := source[start : start+end+2]

	raw, err := os.ReadFile("../devmetrics/testdata/typed-livekit.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	// The page keeps the latest revision of each record, sorted by order.
	latest := map[string]map[string]any{}
	scan := bufio.NewScanner(bytes.NewReader(raw))
	for scan.Scan() {
		_, payload, found := bytes.Cut(scan.Bytes(), []byte(devmetrics.Sentinel))
		if !found {
			continue
		}
		var rec map[string]any
		if err := json.Unmarshal(payload, &rec); err != nil {
			t.Fatal(err)
		}
		latest[rec["kind"].(string)+":"+rec["id"].(string)] = rec
	}
	records := make([]map[string]any, 0, len(latest))
	for _, rec := range latest {
		records = append(records, rec)
	}
	slices.SortFunc(records, func(a, b map[string]any) int { return int(a["order"].(float64) - b["order"].(float64)) })
	feed, err := json.Marshal(records)
	if err != nil {
		t.Fatal(err)
	}

	script := fn + `
const all=JSON.parse(require('node:fs').readFileSync(0,'utf8'));
for (const t of callTurns(all)) console.log((t.input?t.input.id:'greeting')+': '+t.responses.map(r=>r.id).join(' '));`
	cmd := exec.CommandContext(t.Context(), node, "-e", script)
	cmd.Stdin = bytes.NewReader(feed)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, out)
	}
	want := strings.Join([]string{
		"greeting: response-2",
		"input-7: response-5 response-10 response-12",   // book handoff, the announce, the verify ask
		"input-17: response-15 response-20 response-22", // find_or_create_customer, the filler, find_slots
		"input-29: response-27",                         // save_booking
	}, "\n") + "\n"
	if string(out) != want {
		t.Errorf("caller turns:\n%s\nwant:\n%s", out, want)
	}
}
