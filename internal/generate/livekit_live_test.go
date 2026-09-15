package generate

import (
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/ir"
)

// codeOnly strips comment lines and blank lines from emitted Python, so an
// assertion about what the module *does* cannot be satisfied by what it *says*.
//
// This is not caution for its own sake. The emitted live session carries a
// comment naming the four things it deliberately does not pass, so a plain
// substring search for `vad=` finds the comment and reports the absence as a
// presence. The repository has been bitten by the mirror image of this before:
// a dictated `<Parameter>` gate passed on the strength of the emitted comment
// that mentioned it, which is why that gate now looks for a real reader.
func codeOnly(module string) string {
	var kept []string
	for _, line := range strings.Split(module, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if hash := strings.Index(line, "  # "); hash >= 0 {
			line = line[:hash]
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "\n")
}

// TestLiveKitLiveEmitsOneServiceAndNothingItReplaces is the contract in
// specs/023 contracts/authoring.md §3, read off the emitted module.
func TestLiveKitLiveEmitsOneServiceAndNothingItReplaces(t *testing.T) {
	artifact := generateFor(t, "live_model", ir.ProviderLiveKit)
	module := artifactFile(t, artifact, "agent.py")
	code := codeOnly(module)

	for _, want := range []string{
		"from livekit.plugins.openai.realtime import GPTLiveModel",
		"llm=GPTLiveModel(",
		`api_key=os.environ["OPENAI_API_KEY"]`,
		`model="gpt-live-1"`,
		`voice="marin"`,
		// The backend the live entry names, reached through the vendor's own
		// delegation rather than through anything of ours.
		`delegation="responses"`,
		`responses_options={"model": "gpt-5.6-terra"}`,
		// The package's tools are registered the way a cascaded package
		// registers them, so the backend can call them.
		"async def lookup_customer(",
		"async def check_availability(",
	} {
		if !strings.Contains(code, want) {
			t.Errorf("agent.py is missing %q", want)
		}
	}

	// The absences are the contract, not a side effect: each is something a
	// reader of this module would otherwise reasonably expect to find, and each
	// would do damage rather than nothing. A synthesizer would give the call two
	// voices; a turn detector or a voice activity detector would move the turn
	// decision off the model, which is the whole point of this architecture.
	for _, absent := range []string{
		"stt=", "tts=", "turn_handling=", "vad=",
		"silero", "inference.TurnDetector", "TurnHandlingOptions",
		// session.say() is not a route here: the duplex adapter reports
		// supports_say=False, because there is nothing to say it with.
		"session.say(", "dev_say(",
	} {
		if strings.Contains(code, absent) {
			t.Errorf("agent.py carries %q, which architecture: live replaces or cannot use", absent)
		}
	}

	// The dependency follows the import. An import with no dependency is an
	// ImportError at worker startup; a dependency with no import is a model
	// download in every image that no longer loads one.
	pyproject := artifactFile(t, artifact, "pyproject.toml")
	if !strings.Contains(pyproject, "livekit-agents[openai]==") {
		t.Errorf("pyproject.toml does not install the openai extra:\n%s", pyproject)
	}
	if strings.Contains(pyproject, "livekit-plugins-silero") {
		t.Errorf("pyproject.toml still installs the silero plugin, which a live package never loads:\n%s", pyproject)
	}
}

// TestLiveKitLiveGreetsThroughTheModel: the greeting is put to the model as an
// instruction, because there is no synthesizer to speak it. Separate from the
// test above because this is the one piece of the session that would fail at
// runtime rather than at import if it were wrong, and it should fail loudly
// here instead.
func TestLiveKitLiveGreetsThroughTheModel(t *testing.T) {
	module := artifactFile(t, generateFor(t, "live_model", ir.ProviderLiveKit), "agent.py")
	code := codeOnly(module)
	if !strings.Contains(code, "self.session.generate_reply(") {
		t.Error("agent.py does not open the call through the model")
	}
	if !strings.Contains(code, "Open the call with this greeting") {
		t.Error("agent.py does not put the package's greeting to the model")
	}
	if !strings.Contains(code, "Hi, this is Sage and Stone Salon. How can I help?") {
		t.Error("agent.py does not carry the greeting the package wrote")
	}
	// The idle nudge takes the same route, and for the same reason.
	if !strings.Contains(code, "The caller went quiet") {
		t.Error("agent.py does not put the inactivity nudge to the model")
	}
	if !strings.Contains(code, "user_away_timeout=") {
		t.Error("agent.py arms no idle timer, so the nudge can never fire")
	}
	// The other half of the window, which nothing named. A live package builds
	// the same two-stage inactivity handling a cascaded one does: nudge first,
	// then end the call if the caller is still away. Only the nudge was held
	// here and in the smoke, so the end could have been dropped by the live
	// branch and both levels would have stayed green.
	if !strings.Contains(code, "async def _end_if_still_away(") {
		t.Error("agent.py builds no end window, so a caller who never comes back holds the call open")
	}
	if !strings.Contains(code, "session.shutdown()") {
		t.Error("agent.py arms an end window that ends nothing")
	}
	if !strings.Contains(module, "# inactivity.end_after") {
		t.Error("the end window does not say which authored key it came from")
	}
}

// TestLiveKitCascadeStillBuildsAllFour is the control for the absence
// assertions above. Without it they would pass just as well if the live branch
// swallowed every service on every package, which is the failure mode the
// compat digests catch at the whole-tree level and this catches by name.
func TestLiveKitCascadeStillBuildsAllFour(t *testing.T) {
	code := codeOnly(artifactFile(t, generateFor(t, "simple-prompt", ir.ProviderLiveKit), "agent.py"))
	for _, want := range []string{"stt=", "tts=", "turn_handling=", "vad=", "silero"} {
		if !strings.Contains(code, want) {
			t.Errorf("a cascaded package no longer emits %q; the live branch is leaking", want)
		}
	}
}

// TestLiveKitLiveRunbookNamesTheModelAndItsBackend holds the fourth item of
// contracts/authoring.md §3: the emitted runbook is one of the four surfaces
// this repository's docs rule names, and it is the only one generated per
// package, so it is the one that can name *this* package's backend.
//
// The turn-taking section is the control. A live package has no setting that
// decides the turn, so a runbook still explaining an endpointing floor and
// ceiling would be telling the reader to tune something that reaches nothing.
func TestLiveKitLiveRunbookNamesTheModelAndItsBackend(t *testing.T) {
	live := artifactFile(t, generateFor(t, "live_model", ir.ProviderLiveKit), "README.md")
	for _, want := range []string{
		"## The live model",
		"`GPTLiveModel` sits in the session",
		"run on `fast`",
		"in the model's own words",
	} {
		if !strings.Contains(live, want) {
			t.Errorf("the live runbook is missing %q", want)
		}
	}
	for _, absent := range []string{"## Turn taking", "endpointing_delay"} {
		if strings.Contains(live, absent) {
			t.Errorf("the live runbook carries %q, which no setting of a live package reaches", absent)
		}
	}

	cascade := artifactFile(t, generateFor(t, "simple-prompt", ir.ProviderLiveKit), "README.md")
	if !strings.Contains(cascade, "## Turn taking") {
		t.Error("a cascaded runbook lost its turn-taking section; the live branch is leaking")
	}
	if strings.Contains(cascade, "## The live model") {
		t.Error("a cascaded runbook describes a live model it does not build")
	}
}
