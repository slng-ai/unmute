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

// TestMetricsShowsEachCallerTurnInTheOrderItRan runs the page's own functions
// over a typed LiveKit call. LiveKit opens a reply before its input commits, so
// a reply sorts ahead of the input it answers; placed in arrival order, every
// reply landed one turn early. And LiveKit records a TTS request only once
// synthesis ends, so a filler's speech sorted after the model call it covered.
// Node runs the functions as shipped; without Node the test skips, the way the
// browser smoke does.
func TestMetricsShowsEachCallerTurnInTheOrderItRan(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not available")
	}
	source := page(t)
	var script strings.Builder
	for _, name := range []string{"callTurns", "operationGroup", "inStartOrder"} {
		start := strings.Index(source, "function "+name+"(")
		if start < 0 {
			t.Fatalf("index.html has no %s", name)
		}
		// A function ends on its first line, or at the next brace in column zero.
		end := strings.Index(source[start:], "\n")
		if !strings.HasSuffix(source[start:start+end], "}") {
			end = strings.Index(source[start:], "\n}\n") + 2
		}
		script.WriteString(source[start : start+end])
		script.WriteString("\n")
	}
	script.WriteString(`
const all=JSON.parse(require('node:fs').readFileSync(0,'utf8'));
for (const t of callTurns(all)){
  const groups=new Set(t.responses.map(r=>'exchange:'+r.id));
  const ops=all.filter(r=>r.operation && groups.has(operationGroup(r.operation)));
  const rows=inStartOrder(all,ops).map(r=>r.operation.type==='tool'||r.operation.type==='handoff'?r.operation.type+':'+r.operation.name:r.operation.type);
  console.log((t.input?t.input.id:'greeting')+': '+t.responses.map(r=>r.id).join(' ')+' | '+rows.join(' '));
}`)

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

	cmd := exec.CommandContext(t.Context(), node, "-e", script.String())
	cmd.Stdin = bytes.NewReader(feed)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, out)
	}
	want := strings.Join([]string{
		"greeting: response-2 | tts",
		// The handoff, the step announce's speech, then the verify ask.
		"input-7: response-5 response-10 response-12 | llm handoff:book tts llm tts",
		// The filler is spoken while the next model call runs.
		"input-19: response-17 response-22 response-24 | llm tool:find_or_create_customer tts llm tool:find_slots llm tts",
		"input-33: response-31 | llm tool:save_booking llm tts",
	}, "\n") + "\n"
	if string(out) != want {
		t.Errorf("caller turns:\n%s\nwant:\n%s", out, want)
	}
}
