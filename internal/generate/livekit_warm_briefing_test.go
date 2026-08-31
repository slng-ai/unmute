package generate

import (
	"io/fs"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/ir"
	"github.com/slng-ai/unmute/internal/spec"
	"github.com/slng-ai/unmute/internal/target"
)

// A generated LiveKit project briefs the colleague it dials for a warm transfer
// and writes one line per phase of every attempt. These checks stay close to the
// generator because the behaviour only appears on a live phone call. On 2026-08-12 a
// deployed agent dialled a manager, said nothing, then greeted them like a
// stranger, and the deployment log carried not one line about it. Everything a
// live call cannot reach has to be pinned offline.
//
// Platform shapes here were verified by reading and exercising livekit-agents
// 1.6.9 inside the image built from a compiled examples/livekit-human-transfer on
// 2026-08-12. That reading is what caught the rename this file's first test
// guards: 1.6.4 called the instructions hook InstructionParts, 1.6.9 calls it
// WorkflowInstructions, and there is no alias between them.

// configuredLiveKitSIPTwoWarm is a package with two warm transfers, which is the
// only shape that can show whether the persona is emitted once or once per
// transfer (contract C2, C6).
func configuredLiveKitSIPTwoWarm(t *testing.T) (*ir.Agent, ir.Target) {
	t.Helper()
	pkg, err := spec.Load(filepath.Join("..", "testdata", "safe_core"))
	if err != nil {
		t.Fatal(err)
	}
	addColdHumanTransfer(pkg)
	inbound, outbound := true, false
	pkg.Agent.Channels["phone"] = spec.Channel{
		Kind: "telephony", Inbound: &inbound, Outbound: &outbound,
		RequiredControls: []string{"hangup"},
	}
	configured := pkg.Targets["livekit"]
	configured.Connection = "primary_phone"
	setConnectionRoute(pkg, "primary_phone", "sip", "twilio")
	pkg.Targets = map[string]spec.Target{"livekit": configured}
	connection := pkg.Connections["primary_phone"]
	connection.Environment = map[string]string{
		"sip_address": "SIP_TRUNK_HOSTNAME", "sip_username": "SIP_AUTH_USERNAME",
		"sip_password": "SIP_AUTH_PASSWORD", "from_number": "SIP_FROM_NUMBER",
	}
	pkg.Connections["primary_phone"] = connection

	human := pkg.Agent.Escalations["to_human"]
	human.Cold = nil
	human.Warm = &spec.WarmTransfer{Destination: "billing_line", Briefing: "Say who is calling and why."}
	pkg.Agent.Escalations["to_human"] = human
	second := spec.Escalation{
		When: "Caller asks for the manager by name.",
		Warm: &spec.WarmTransfer{Destination: "billing_line"},
	}
	pkg.Agent.Escalations["to_manager"] = second
	billing := pkg.Agent.Agents["billing"]
	billing.Escalations = append(billing.Escalations, "to_manager")
	pkg.Agent.Agents["billing"] = billing

	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	return agent, agent.Targets["livekit"]
}

// The two emitted imports both stories need, and the retired name that must not
// come back. `time` rides either kind of transfer, because both outcome lines
// carry elapsed seconds. WorkflowInstructions rides the warm one alone.
//
// The last assertion walks the working tree rather than git on purpose: it
// asserts what a template *contains*, not what is committed, so the
// constitution's git-not-working-tree rule for repository hygiene checks does
// not apply here. A template that names InstructionParts produces a project
// that dies at import on the pinned platform version, and nothing else in the
// offline suite would see it: the emitted text is what the suite reads, and the
// name is a valid Python identifier either way.
func TestWarmBriefingEmittedImports(t *testing.T) {
	warmAgent, warmTarget := configuredLiveKitSIP(t)
	warmArtifact, err := GenerateLiveKit(warmAgent, warmTarget, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	warmPy := artifactFile(t, warmArtifact, "agent.py")
	for _, want := range []string{
		"import time",
		"from livekit.agents.beta.workflows import WarmTransferTask, WorkflowInstructions",
	} {
		if !strings.Contains(warmPy, want) {
			t.Errorf("warm agent.py missing %q", want)
		}
	}

	coldAgent, coldTarget := configuredLiveKitSIPCold(t)
	coldArtifact, err := Generate(coldAgent, coldTarget, target.Default())
	if err != nil {
		t.Fatal(err)
	}
	coldPy := artifactFile(t, coldArtifact, "agent.py")
	if !strings.Contains(coldPy, "_refer_uri(") {
		t.Fatal("fixture is not a cold-only transfer package")
	}
	if !strings.Contains(coldPy, "import time") {
		t.Error("cold-only agent.py does not import time; its outcome lines carry elapsed seconds too")
	}
	if strings.Contains(coldPy, "WorkflowInstructions") {
		t.Error("cold-only agent.py imports WorkflowInstructions; only a warm transfer briefs anybody")
	}

	for _, artifact := range []Artifact{warmArtifact, coldArtifact} {
		for _, file := range artifact.Files {
			if strings.Contains(string(file.Content), "InstructionParts") {
				t.Errorf("%s names InstructionParts, which livekit-agents 1.6.9 does not define", file.Path)
			}
		}
	}
	err = fs.WalkDir(livekitV1Templates, ".", func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		content, readErr := livekitV1Templates.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		if strings.Contains(string(content), "InstructionParts") {
			t.Errorf("%s names InstructionParts; 1.6.9 renamed it to WorkflowInstructions with no alias", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// contracts/transfer-log.md, the warm half: four lines in order, the count of
// what was handed over, and elapsed seconds on whichever outcome happens.
func TestWarmTransferLogContract(t *testing.T) {
	agent, resolved := configuredLiveKitSIP(t)
	artifact, err := GenerateLiveKit(agent, resolved, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	agentPy := artifactFile(t, artifact, "agent.py")
	for _, want := range []string{
		`logger.info("human transfer fired: to_human (warm)")`,
		`"warm transfer dialling out: handing over %d conversation messages",`,
		"len(briefing_ctx.items),",
		`"warm transfer merged after %ds: %s",`,
		"int(time.monotonic() - started),",
		"started = time.monotonic()",
	} {
		if !strings.Contains(agentPy, want) {
			t.Errorf("warm agent.py missing log contract line %q", want)
		}
	}
	// The count and the handover read the same local. Two reads of self.chat_ctx
	// would let the logged number describe something other than what was sent,
	// which is the one thing this line exists to rule out (contract C4).
	if strings.Contains(agentPy, "chat_ctx=self.chat_ctx,") {
		t.Error("warm agent.py reads the conversation twice; the logged count could then describe a different context")
	}
	// Both unavailable branches log. The return_to_caller branch wrote nothing at
	// all until 2026-08-12, so the commonest warm failure was also invisible.
	returnAgent, returnTarget := configuredLiveKitSIP(t)
	returnHuman := returnAgent.Controls["to_human"].(*ir.HumanTransfer)
	returnHuman.OnUnavailable = ir.OnUnavailableReturn
	returnArtifact, err := GenerateLiveKit(returnAgent, returnTarget, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	hangupAgent, hangupTarget := configuredLiveKitSIP(t)
	hangupHuman := hangupAgent.Controls["to_human"].(*ir.HumanTransfer)
	hangupHuman.OnUnavailable = ir.OnUnavailableHangup
	hangupArtifact, err := GenerateLiveKit(hangupAgent, hangupTarget, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	for name, py := range map[string]string{
		"return_to_caller": artifactFile(t, returnArtifact, "agent.py"),
		"hangup":           artifactFile(t, hangupArtifact, "agent.py"),
	} {
		if !strings.Contains(py, `"warm transfer unavailable after %ds: %s",`) {
			t.Errorf("on_unavailable: %s leaves the failure branch unlogged", name)
		}
	}
}

// contracts/transfer-log.md, the cold half. Behaviour must not change: only the
// three lines are new (contract C11).
func TestColdTransferLogContract(t *testing.T) {
	agent, resolved := configuredLiveKitSIPCold(t)
	artifact, err := Generate(agent, resolved, target.Default())
	if err != nil {
		t.Fatal(err)
	}
	agentPy := artifactFile(t, artifact, "agent.py")
	for _, want := range []string{
		"(cold)\")",
		`logger.info("cold transfer referring the caller out")`,
		"started = time.monotonic()",
		`logger.info("cold transfer completed after %ds", int(time.monotonic() - started))`,
		`"cold transfer failed after %ds: %s",`,
	} {
		if !strings.Contains(agentPy, want) {
			t.Errorf("cold agent.py missing log contract line %q", want)
		}
	}
	// The referring line goes before the request, so a request that never
	// returns is still visible as a phase the transfer reached.
	referring := strings.Index(agentPy, `logger.info("cold transfer referring the caller out")`)
	request := strings.Index(agentPy, "await job_ctx.api.sip.transfer_sip_participant(request)")
	if referring < 0 || request < 0 || referring > request {
		t.Error("cold agent.py logs the referral after making it, so a hung request leaves no trace")
	}
	// The no-caller branch logs too. Added 2026-08-12 after a live test hit it
	// from the Agent Console, where there is no SIP leg to refer: the tool fired,
	// the agent said something vague, and the log showed line 1 and then nothing.
	// The contract used to say the absence of line 2 covered this, which is only
	// true for a reader who already knows to look for an absence.
	if !strings.Contains(agentPy, `logger.info("cold transfer skipped: no phone caller in the room")`) {
		t.Error("cold agent.py leaves the no-phone-caller branch unlogged; a live test read that silence as a broken transfer")
	}
	skipped := strings.Index(agentPy, "cold transfer skipped")
	if skipped < 0 || skipped > referring {
		t.Error("the skipped line must be in the early-return branch, before the referral")
	}
	// It has to say why, or the next reader repeats the same test from the
	// console and reaches the same wrong conclusion.
	if !strings.Contains(agentPy, "did not \"\n                \"arrive by phone") &&
		!strings.Contains(agentPy, "this session did not arrive by phone") {
		t.Error("the returned message does not tell the model why there is nobody to transfer")
	}
}

// contracts/transfer-log.md L5, L6 and L7, enforced structurally rather than by
// review: no log line anywhere in an emitted project may carry a destination, a
// credential, or the caller's words. The control name on the fired line already
// says which destination went out, and the participant identity on the merge
// line is the platform's own value, which is not a phone number (L8).
func TestTransferLogsCarryNoDestinationOrCredential(t *testing.T) {
	e164 := regexp.MustCompile(`\+\d{7,}`)
	for _, shape := range []struct {
		name     string
		artifact func(*testing.T) Artifact
	}{
		{"warm and outbound", func(t *testing.T) Artifact {
			agent, resolved := configuredLiveKitSIP(t)
			artifact, err := GenerateLiveKit(agent, resolved, nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			return artifact
		}},
		{"cold only", func(t *testing.T) Artifact {
			agent, resolved := configuredLiveKitSIPCold(t)
			artifact, err := Generate(agent, resolved, target.Default())
			if err != nil {
				t.Fatal(err)
			}
			return artifact
		}},
	} {
		t.Run(shape.name, func(t *testing.T) {
			agentPy := artifactFile(t, shape.artifact(t), "agent.py")
			calls := loggerCalls(agentPy)
			if len(calls) == 0 {
				t.Fatal("no logger calls found; the scanner is broken, not the template")
			}
			// The scanner has to reach the whole call, or a forbidden argument on
			// a later line passes by being invisible. One multi-line call per
			// shape, checked end to end.
			var multiline string
			for _, call := range calls {
				if strings.Contains(call, "%ds") {
					multiline = call
					break
				}
			}
			if multiline == "" || !strings.HasSuffix(multiline, ")") || !strings.Contains(multiline, "time.monotonic() - started") {
				t.Fatalf("the scanner did not reach the end of a multi-line log call:\n%s", multiline)
			}
			for _, call := range calls {
				for _, forbidden := range []string{"os.environ", "_refer_uri", "_sip_number", "_sip_trunk", "text_content"} {
					if strings.Contains(call, forbidden) {
						t.Errorf("a log line reads %s:\n%s", forbidden, call)
					}
				}
				if e164.MatchString(call) {
					t.Errorf("a log line carries something shaped like a phone number:\n%s", call)
				}
			}
		})
	}
}

// loggerCalls returns the source text of every logger.<level>(...) call in the
// emitted module, one string per call, parentheses balanced. String literals are
// skipped when balancing so a bracket inside a message cannot end a call early.
func loggerCalls(source string) []string {
	var calls []string
	for index := 0; ; {
		start := strings.Index(source[index:], "logger.")
		if start < 0 {
			return calls
		}
		start += index
		open := strings.Index(source[start:], "(")
		if open < 0 {
			return calls
		}
		cursor, depth, quote := start+open, 0, byte(0)
		for ; cursor < len(source); cursor++ {
			char := source[cursor]
			if quote != 0 {
				if char == '\\' {
					cursor++
					continue
				}
				if char == quote {
					quote = 0
				}
				continue
			}
			switch char {
			case '"', '\'':
				quote = char
			case '(':
				depth++
			case ')':
				depth--
			}
			if depth == 0 && char == ')' {
				cursor++
				break
			}
		}
		calls = append(calls, source[start:cursor])
		index = cursor
	}
}

// contracts/emitted-briefing.md C1 through C6: the supported instructions hook,
// the persona by reference, the authored briefing in the slot that keeps the
// platform's default when there is none, and one persona per package.
func TestWarmBriefingInstructionsHook(t *testing.T) {
	agent, resolved := configuredLiveKitSIP(t)
	artifact, err := GenerateLiveKit(agent, resolved, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	agentPy := artifactFile(t, artifact, "agent.py")
	for _, want := range []string{
		"instructions=WorkflowInstructions(",
		"persona=_BRIEFING_PERSONA,",
		`extra="Say who is calling and why.",`,
		"_BRIEFING_PERSONA = \"\"\"\\",
	} {
		if !strings.Contains(agentPy, want) {
			t.Errorf("warm agent.py missing %q", want)
		}
	}
	// C1: the deprecated parameter is gone from every emitted file, not just
	// from agent.py. The prebuilt warns and ignores it once instructions is
	// given, so leaving it anywhere is a lie about what the package does.
	for _, file := range artifact.Files {
		if strings.Contains(string(file.Content), "extra_instructions") {
			t.Errorf("%s still names extra_instructions", file.Path)
		}
	}

	// C3: a warm transfer with no authored briefing passes no extra at all,
	// because a no-op argument is a thing a reader of the emitted code has to
	// check. It is cosmetic and the contract says so: extra defaults to "" and
	// the platform holds no default text for that slot, so omitting it and
	// passing "" resolve to byte-identical instructions (measured inside 1.6.9 on
	// 2026-08-12). persona is the field where the distinction is real: it has a
	// NOT_GIVEN sentinel with a platform default behind it.
	plainAgent, plainTarget := configuredLiveKitSIPWarmOnly(t)
	plainArtifact, err := GenerateLiveKit(plainAgent, plainTarget, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	plainPy := artifactFile(t, plainArtifact, "agent.py")
	if !strings.Contains(plainPy, "persona=_BRIEFING_PERSONA,") {
		t.Error("a warm transfer with no authored briefing lost its persona too")
	}
	if strings.Contains(plainPy, "extra=") {
		t.Error("a warm transfer with no authored briefing passes extra=, which deletes the platform's own section")
	}

	// C2 and C6: one persona per package, however many warm transfers use it.
	twoAgent, twoTarget := configuredLiveKitSIPTwoWarm(t)
	twoArtifact, err := GenerateLiveKit(twoAgent, twoTarget, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	twoPy := artifactFile(t, twoArtifact, "agent.py")
	if got := strings.Count(twoPy, "_BRIEFING_PERSONA = "); got != 1 {
		t.Errorf("agent.py defines _BRIEFING_PERSONA %d times, want 1", got)
	}
	if got := strings.Count(twoPy, "persona=_BRIEFING_PERSONA,"); got != 2 {
		t.Errorf("agent.py references the persona %d times, want one per warm transfer (2)", got)
	}
}

// contracts/emitted-briefing.md section 2: the persona has to say four things,
// and every one of them answers a defect seen on a live call rather than a
// preference. A phrase per part, so an edit that drops one fails here instead of
// on somebody's phone. P4 is User Story 3's only emitted change.
func TestWarmBriefingPersonaSaysWhatItMust(t *testing.T) {
	agent, resolved := configuredLiveKitSIP(t)
	artifact, err := GenerateLiveKit(agent, resolved, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	agentPy := artifactFile(t, artifact, "agent.py")
	for part, want := range map[string]string{
		"P1 open with the handover, never a greeting": "Your first words are the handover, not a greeting.",
		"P1 do not wait to be asked":                  "never wait to be asked what the call is about",
		"P2 name the cue that joins the calls":        "you will put the caller through as soon as they say they are ready",
		"P3 what to say when the transcript is thin":  "say that\nsomeone is on hold asking for a person",
		"P3 never greet instead":                      "Never greet instead.",
		"P4 decline rather than leave it open":        "decline the transfer and give their reason",
		"P4 why declining matters":                    "the caller is holding for the whole of this conversation",
	} {
		if !strings.Contains(agentPy, want) {
			t.Errorf("the emitted persona no longer says %s: missing %q", part, want)
		}
	}
	// C7: the comment above it carries the version and date it was checked
	// against, because the class it feeds was renamed between two patch releases.
	if !strings.Contains(agentPy, "Verified against livekit-agents 1.6.9 on 2026-08-12") {
		t.Error("the persona comment does not say what version and date it was verified against")
	}
	// C8: a prompt is emitted text like any other. No destination, no credential.
	persona := agentPy[strings.Index(agentPy, "_BRIEFING_PERSONA = "):]
	persona = persona[:strings.Index(persona, "\"\"\"\n")+4]
	for _, forbidden := range []string{"os.environ", "+1", "SIP_", "TWILIO_"} {
		if strings.Contains(persona, forbidden) {
			t.Errorf("the persona text carries %q", forbidden)
		}
	}
}

// C6, the other half: a cold-only package gains neither the persona nor the
// hook. Cold refers the caller's own leg out and briefs nobody, so a persona in
// its agent.py would be dead prose in every deployment that read it.
func TestColdOnlyPackageGetsNoBriefing(t *testing.T) {
	agent, resolved := configuredLiveKitSIPCold(t)
	artifact, err := Generate(agent, resolved, target.Default())
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range artifact.Files {
		for _, forbidden := range []string{"_BRIEFING_PERSONA", "WorkflowInstructions"} {
			if strings.Contains(string(file.Content), forbidden) {
				t.Errorf("cold-only %s contains %q", file.Path, forbidden)
			}
		}
	}
}
