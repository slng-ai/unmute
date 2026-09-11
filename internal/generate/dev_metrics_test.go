package generate

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/devmetrics"
	"github.com/slng-ai/unmute/internal/ir"
	"github.com/slng-ai/unmute/internal/spec"
	"github.com/slng-ai/unmute/internal/target"
)

// The measurement producers are the one place where a Go constant and generated
// Python have to agree on a literal string. Nothing else in the tree can catch a
// disagreement: the Python is emitted, `examples/*/build/` is gitignored, and a
// producer reading the wrong switch name is not a crash, it is a feature that
// silently prints nothing while every other test stays green. So the switch name
// and the sentinel are asserted here, against the constants that own them.
func TestDevMetricsProducerAgreesWithTheGoContract(t *testing.T) {
	for _, tc := range []struct {
		name     string
		pkg      string
		provider ir.Provider
		entry    string
		wiring   string
	}{
		{"pipecat", "safe_core", ir.ProviderPipecat, "bot.py", "dev.observers()"},
		{"livekit", "remy", ir.ProviderLiveKit, "agent.py", "install_dev_metrics(session, call_id=ctx.room.name)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			artifact := generateFor(t, tc.pkg, tc.provider)

			producer := artifactFile(t, artifact, "dev_metrics.py")
			if !strings.Contains(producer, `METRICS_ENV = "`+devmetrics.Env+`"`) {
				t.Errorf("producer does not read %s, so the dev loop cannot switch it on", devmetrics.Env)
			}
			if !strings.Contains(producer, `SENTINEL = "`+strings.TrimSpace(devmetrics.Sentinel)+`"`) {
				t.Errorf("producer does not print the sentinel %q the decoder looks for", devmetrics.Sentinel)
			}
			// The framing has to survive a container prefix, which means the
			// sentinel and the payload are separated by exactly one space.
			if !strings.Contains(producer, `f"{SENTINEL} {json.dumps(record, separators=(',', ':'))}"`) {
				t.Error("producer does not emit the sentinel followed by one space and compact JSON")
			}
			if !strings.Contains(producer, "flush=True") {
				t.Error("producer does not flush, so records can arrive long after the turn")
			}

			entry := artifactFile(t, artifact, tc.entry)
			if !strings.Contains(entry, tc.wiring) {
				t.Errorf("%s does not wire the producer: want %q", tc.entry, tc.wiring)
			}
		})
	}
}

// Emission must not depend on configuration. A producer emitted only when some
// feature is on would make build/<target>/ depend on which command last ran, so
// the dev loop would stop exercising the file that ships.
func TestDevMetricsIsEmittedEvenWhenNothingElseIs(t *testing.T) {
	for _, tc := range []struct {
		name     string
		pkg      string
		provider ir.Provider
	}{
		{"livekit unconfigured", "remy", ir.ProviderLiveKit},
		{"pipecat", "safe_core", ir.ProviderPipecat},
	} {
		t.Run(tc.name, func(t *testing.T) {
			artifact := generateFor(t, tc.pkg, tc.provider)
			if !artifactHasFile(artifact, "dev_metrics.py") {
				t.Error("artifact does not carry dev_metrics.py")
			}
		})
	}
}

// agentFor loads and builds a testdata package, for tests that mutate the agent
// before generating.
func agentFor(t *testing.T, pkgName string) *ir.Agent {
	t.Helper()
	pkg, err := spec.Load(filepath.Join("..", "testdata", pkgName))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	return agent
}

func generateFor(t *testing.T, pkgName string, provider ir.Provider) Artifact {
	t.Helper()
	agent := agentFor(t, pkgName)
	artifact, err := Generate(agent, targetByProvider(t, agent, provider), target.Default())
	if err != nil {
		t.Fatal(err)
	}
	return artifact
}

// The switch reaching the producer is a second, separate contract. A LiveKit
// worker runs inside a container and a Pipecat phone route can too, so setting
// the variable on the child process is not enough: a container receives only
// what its compose service declares. This shipped once with the producer wired
// correctly and the variable never arriving, which looks exactly like a target
// that reports nothing, so the compose files are pinned here. The local-run
// marker must also arrive or LiveKit skips its local worker startup settings.
func TestEveryComposeThatRunsAnAgentForwardsTheSwitch(t *testing.T) {
	for _, tc := range []struct {
		name     string
		pkg      string
		provider ir.Provider
		composes []string
	}{
		{"pipecat", "safe_core", ir.ProviderPipecat, []string{"compose.dev.yaml"}},
		{"livekit", "remy", ir.ProviderLiveKit, []string{"compose.dev.yaml"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			artifact := generateFor(t, tc.pkg, tc.provider)
			for _, name := range tc.composes {
				compose := artifactFile(t, artifact, name)
				// Bare name, no value: compose forwards the host's value when it
				// is set and omits the variable entirely when it is not, which is
				// what keeps a deployed artifact silent.
				for _, env := range []string{devmetrics.Env, LocalRunEnv} {
					if !strings.Contains(compose, "\n      - "+env+"\n") {
						t.Errorf("%s does not forward %s, so the container misses the dev settings", name, env)
					}
					if strings.Contains(compose, env+"=") {
						t.Errorf("%s pins a value for %s; it must pass through so it is absent when unset", name, env)
					}
				}
			}
		})
	}
}

// A delegate and a transfer reach both targets as function tools, so both
// producers would time them like tools. On LiveKit a delegate does not return
// until the flow it started has finished, which reported a 51-second "tool".
// Dropping the row instead left the model call the control causes with nothing
// naming its cause, so a reply that ran one delegate read as two duplicate
// model calls. A control now gets a row typed handoff and no duration. The set
// is generated per package, so the check is that every control the package
// declares is in it, every real tool is not, and the producer types the row
// rather than returning early. `make smoke` proves the behaviour.
func TestAControlIsShownWithoutADuration(t *testing.T) {
	for _, tc := range []struct {
		name     string
		pkg      string
		provider ir.Provider
		handoffs []string
		tools    []string
		guard    string
	}{
		{
			"livekit", "remy", ir.ProviderLiveKit,
			[]string{"to_reservations", "to_events", "back_to_greeter", "do_reserve", "do_event"},
			[]string{"check_availability", "send_confirmation"},
			"if update.call_id in self.control_tools:",
		},
		{"pipecat", "safe_core", ir.ProviderPipecat, []string{"to_billing"}, nil, `if tool["control"]:`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			producer := artifactFile(t, generateFor(t, tc.pkg, tc.provider), "dev_metrics.py")
			set := producer[strings.Index(producer, "HANDOFF_CONTROLS"):]
			set = set[:strings.Index(set, ")")]
			for _, name := range tc.handoffs {
				if !strings.Contains(set, `"`+name+`"`) {
					t.Errorf("%s is a control but is not in the set, so its flow duration is reported as a tool", name)
				}
			}
			// A real tool in the set would be worse than the bug: a measurement
			// that silently disappears rather than one that reads oddly.
			for _, name := range tc.tools {
				if strings.Contains(set, `"`+name+`"`) {
					t.Errorf("%s is a real tool and must still be timed", name)
				}
			}
			// The set is only worth generating if something consults it.
			if !strings.Contains(producer, "in HANDOFF_CONTROLS") {
				t.Error("producer never consults the control set")
			}
			// The row, and the guard that keeps a duration off it.
			if !strings.Contains(producer, `"handoff"`) {
				t.Error("producer emits no handoff row, so a control has no visible cause")
			}
			duration := strings.Index(producer, `"tool_duration"`)
			if duration < 0 {
				t.Fatal("producer measures no tool duration at all")
			}
			if guard := strings.Index(producer, tc.guard); guard < 0 || guard > duration {
				t.Errorf("no %s above the tool duration, so a control is timed as a tool", tc.guard)
			}
		})
	}
}

// TestPipecatReportsWhereAReplysTimeWent holds the producer to the 1.9.0
// breakdown: every part the framework names, the anchor it measured from, and
// the framework's own total beside them.
//
// The total matters more than it looks. Re-deriving it from the parts would make
// the decoder's "the parts add up" check unfailable, and a part dropped later
// would then be a timeline quietly missing time. Carrying the framework's number
// is what makes that check able to fail.
//
// The deprecated accessor is refused by name: `chronological_events` lists only
// the services that reported a metric, is deprecated for removal in 2.0.0, and
// prints a framework warning on every call.
func TestPipecatReportsWhereAReplysTimeWent(t *testing.T) {
	producer := artifactFile(t, generateFor(t, "safe_core", ir.ProviderPipecat), "dev_metrics.py")
	for _, want := range []string{
		"on_latency_breakdown",
		"breakdown.contributions",
		`"measured_from": str(breakdown.measured_from)`,
		`"total_secs": breakdown.total_secs`,
		`self._put("breakdown"`,
		`"owner_kind": str(part.owner_kind)`,
	} {
		if !strings.Contains(producer, want) {
			t.Errorf("the producer does not read %q, so the page cannot say where a reply's time went", want)
		}
	}
	if strings.Contains(producer, "chronological_events") {
		t.Error("the producer calls the deprecated event list, which warns on every call and names only the services that reported a metric")
	}
	if strings.Contains(producer, "sum(part[") {
		t.Error("the producer re-derives the total from the parts, which makes the decoder's sum check unfailable")
	}
	// The record the page reads is the one the Go decoder types.
	if !strings.Contains(producer, devmetrics.KindBreakdown) {
		t.Errorf("the producer names no %q record", devmetrics.KindBreakdown)
	}
}

// TestPipecatNamesWhyAToolProducedNoResult: a handler that raised and one that
// ran past its deadline are different answers, and the framework reports them
// differently. The deadline is read off the cancel frame's `run_llm`, which the
// framework sets on exactly that one cancellation, rather than off log wording
// that is nobody's contract.
func TestPipecatNamesWhyAToolProducedNoResult(t *testing.T) {
	producer := artifactFile(t, generateFor(t, "safe_core", ir.ProviderPipecat), "dev_metrics.py")
	for _, want := range []string{
		`getattr(frame, "run_llm", False)`,
		`self._end_tool(tool, "timed_out"`,
		`getattr(frame, "error", None)`,
		`self._end_tool(tool, "failed", reason=str(error))`,
		`self._end_tool(tool, "cancelled")`,
		`self._end_tool(tool, "returned")`,
	} {
		if !strings.Contains(producer, want) {
			t.Errorf("the producer does not carry %q, so a tool with no result says nothing about why", want)
		}
	}
	// What ended the call, and how long the bot spoke: two things no service
	// reports and the page had no answer for.
	for _, want := range []string{
		`self._update("call", "call", state="error",`,
		`f"{processor.name}: {frame.error}"`,
		`"speech_duration"`,
		"BotStartedSpeakingFrame",
		"BotStoppedSpeakingFrame",
	} {
		if !strings.Contains(producer, want) {
			t.Errorf("the producer does not carry %q", want)
		}
	}
	// LiveKit reports its own breakdown and its own tool states, and this change
	// is Pipecat's observer. Its producer must not have grown any of it.
	livekit := artifactFile(t, generateFor(t, "remy", ir.ProviderLiveKit), "dev_metrics.py")
	for _, absent := range []string{"on_latency_breakdown", "breakdown.contributions", devmetrics.KindBreakdown} {
		if strings.Contains(livekit, absent) {
			t.Errorf("the LiveKit producer grew %q, which is the Pipecat observer's", absent)
		}
	}
}
