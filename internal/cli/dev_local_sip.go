package cli

import (
	"bytes"
	"crypto/rand"
	"fmt"
	"io"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/slng-ai/unmute/internal/generate"
	"github.com/slng-ai/unmute/internal/ir"
	"github.com/slng-ai/unmute/internal/target"
)

// The SIP plane: a real SIP stack on this machine, and the reason a default
// `unmute dev --telephony` needs no carrier account. The plane runs the same
// mechanism the route uses in production, a SIP trunk, so a call that works
// here works for the same reasons a deployed one does (FR-006).
//
// Everything a carrier would supply is supplied by the plane instead: the trunk
// the agent dials through, the credential a caller authenticates with, and the
// addresses transfers land on. None of it leaves the machine.

const (
	// The two Compose profiles. One process can advertise one address, and a
	// softphone on the host and a container on the plane's network need
	// different ones, so which profile runs decides what the SIP service says
	// about itself.
	planeProfileSoftphone = "softphone"
	planeProfileHeadless  = "headless"

	// planeLocalNumber is the number the plane answers on, owned by
	// internal/target because the emitted Compose file needs the same string:
	// the caller endpoint dials it and this command prints it.
	planeLocalNumber = target.LocalPlaneNumber

	// planeFixtureSeconds is how long every endpoint's audio lasts. An endpoint
	// hangs up the moment its input runs out, so a fixture that is too short
	// reads as a dropped leg, which is the most misleading failure this plane
	// can produce. Long enough for a real conversation, not just for the
	// unattended check: a person on a softphone talking to a transferred
	// destination is the case that needs the minutes.
	planeFixtureSeconds = 300
)

// dialCredential is the per-run credential a caller authenticates with. It is
// printed, because the developer has to type it into a softphone, and it is
// written to no file (gate S3).
//
// This is not a secret in the sense the constitution protects. It is minted
// fresh per run, it authorises nothing but a call to a port on the developer's
// own machine, and it dies when the run ends. A credential nobody can read
// would be useless here.
type dialCredential struct {
	Username string
	Password string
}

// credentialAlphabet leaves out the characters people misread when copying a
// password off a terminal into a softphone: no l, o, 0, 1.
const credentialAlphabet = "abcdefghijkmnpqrstuvwxyz23456789"

func newDialCredential() (dialCredential, error) {
	user, err := randomCredentialText(6)
	if err != nil {
		return dialCredential{}, fmt.Errorf("mint dial credential: %w", err)
	}
	password, err := randomCredentialText(12)
	if err != nil {
		return dialCredential{}, fmt.Errorf("mint dial credential: %w", err)
	}
	return dialCredential{Username: "dev-" + user, Password: password}, nil
}

func randomCredentialText(length int) (string, error) {
	out := make([]byte, length)
	for i := range out {
		index, err := rand.Int(rand.Reader, big.NewInt(int64(len(credentialAlphabet))))
		if err != nil {
			return "", err
		}
		out[i] = credentialAlphabet[index.Int64()]
	}
	return string(out), nil
}

// planeEndpoints returns the plan's endpoints in one role.
func planeEndpoints(plan *generate.TelephonyRuntimePlan, role string) []ir.TelephonyLocalEndpoint {
	var out []ir.TelephonyLocalEndpoint
	for _, endpoint := range plan.LocalEndpoints {
		if endpoint.Role == role {
			out = append(out, endpoint)
		}
	}
	return out
}

// planeEndpointURI is how the plane addresses one endpoint. An endpoint answers
// only a request URI whose host is local to itself, so this is an address and
// never a Compose service name: measured 2026-08-20, a name resolves and the
// endpoint then refuses the call.
func planeEndpointURI(endpoint ir.TelephonyLocalEndpoint) string {
	return fmt.Sprintf("sip:%s@%s:%d", endpoint.Name, endpoint.Address, endpoint.Port)
}

// planeTrunkHost is the host the agent's dial-out trunk points at: the plane's
// destinations endpoint, which routes an incoming call to the account matching
// the request URI's user part.
func planeTrunkHost(plan *generate.TelephonyRuntimePlan) string {
	destinations := planeEndpoints(plan, ir.TelephonyRoleDestination)
	if len(destinations) == 0 {
		return ""
	}
	return destinations[0].Address
}

// planeEnvironment is what a local SIP-plane run supplies for itself, and it is
// the whole of gate S7. The emitted agent dials transfers with a trunk
// configuration it takes inline from four environment names, so pointing those
// names at the plane's own endpoint is what an outbound trunk *is* on this
// route. Nothing is registered on the platform, which is deliberate and
// unchanged from how a deployment dials: the same code path, a different host.
//
// Supplying these is also what makes the default loop credential-free (SC-004).
// Every name here is one the author would otherwise have to put a real carrier
// value into before a local call could be placed at all.
func planeEnvironment(plan *generate.TelephonyRuntimePlan, credential dialCredential) map[string]string {
	values := map[string]string{}
	set := func(key, value string) {
		if name := plan.Environment[key]; name != "" && value != "" {
			values[name] = value
		}
	}
	set("sip_address", planeTrunkHost(plan))
	// The endpoint checks no credential: it is on a private network with one
	// caller. These are set because the emitted agent reads all four names and
	// would fail on a missing one, and they carry the run's own credential
	// rather than a placeholder so nothing in the run is a value with two
	// meanings.
	set("sip_username", credential.Username)
	set("sip_password", credential.Password)
	set("from_number", planeLocalNumber)
	// Each destination resolves to its endpoint. A full SIP URI, because the
	// cold-transfer path sends the destination as the Refer-To of a real SIP
	// REFER and the caller has to be able to route it: a bare number becomes
	// `tel:` there, which is exactly the failure a live cold transfer showed on
	// 2026-08-19 with no carrier to resolve it.
	for _, endpoint := range planeEndpoints(plan, ir.TelephonyRoleDestination) {
		if endpoint.EnvName != "" {
			values[endpoint.EnvName] = planeEndpointURI(endpoint)
		}
	}
	return values
}

// planeAdvertiseAddress finds an address on this machine that a softphone can
// send to and that the plane's own containers can route back to.
//
// It is deliberately not loopback. The plane advertises this address in its
// Contact header and its SDP, and a container that is told to send audio to
// 127.0.0.1 sends it to its own loopback, which is nowhere. A private address
// on a real interface is reachable from both sides, so one value serves the
// softphone on the host and the endpoints in containers.
//
// The plane's own subnet is skipped: once the network exists its gateway shows
// up as a local interface, and advertising that would work by accident on one
// platform and not on others.
func planeAdvertiseAddress(subnet string) (string, error) {
	var planeNet *net.IPNet
	if subnet != "" {
		if _, parsed, err := net.ParseCIDR(subnet); err == nil {
			planeNet = parsed
		}
	}
	interfaces, err := net.Interfaces()
	if err != nil {
		return "", fmt.Errorf("find an address for the plane to advertise: %w", err)
	}
	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addresses, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, address := range addresses {
			ipNet, ok := address.(*net.IPNet)
			if !ok {
				continue
			}
			ip := ipNet.IP.To4()
			if ip == nil || !ip.IsPrivate() {
				continue
			}
			if planeNet != nil && planeNet.Contains(ip) {
				continue
			}
			return ip.String(), nil
		}
	}
	return "", fmt.Errorf("no private address on any interface of this machine")
}

// planeRunID names this run's call directory. Recordings from one run never
// land on top of another's, which matters most when a run has failed and the
// question is what the previous one heard.
//
// The timestamp alone is not enough: it is accurate to the second, and two runs
// started in the same second would share a directory and overwrite each other's
// recordings. The suffix is what makes it per-run rather than per-second.
func planeRunID() string {
	stamp := time.Now().UTC().Format("20060102-150405")
	suffix, err := randomCredentialText(4)
	if err != nil {
		return stamp
	}
	return stamp + "-" + suffix
}

// planeCallDir is where this run's recordings go, relative to the build
// directory, in the form the emitted Compose file mounts.
func planeCallDir(outDir, runID string) string {
	return filepath.Join(outDir, "calls", runID)
}

// printPlaneReady is the dial instruction, and it prints before the run blocks
// (gate P4). Everything a developer needs to place a call is on it, because the
// alternative is reading a generated Compose file to find the port.
func printPlaneReady(out io.Writer, targetName string, plan *generate.TelephonyRuntimePlan, plane *planeRun, report *runReport) {
	instruction := fmt.Sprintf("sip:%s@%s:5060", planeLocalNumber, plane.advertise)
	if report != nil {
		report.DialInstruction = instruction
		report.DialCredential = plane.credential.Password
	}
	fmt.Fprintf(out, "\n  \033[1;32m▸\033[0m dial %s   user %s  pass %s\n",
		instruction, plane.credential.Username, plane.credential.Password)
	fmt.Fprint(out, "    from any softphone on this machine. `brew install baresip` is one that\n")
	fmt.Fprint(out, "    works here and is BSD licensed; any client that can place a call with\n")
	fmt.Fprint(out, "    registration turned off will do. The plane is not a registrar, and a\n")
	fmt.Fprint(out, "    client that insists on registering first cannot be used at all.\n")
	for _, endpoint := range planeEndpoints(plan, ir.TelephonyRoleDestination) {
		fmt.Fprintf(out, "    transfers to %s land on %s\n", endpoint.Name, planeEndpointURI(endpoint))
	}
	fmt.Fprintf(out, "    recordings: %s\n", plane.callDir)
	fmt.Fprint(out, "    ctrl-c to stop\n\n")
}

// printPlaneLocalDial says where a --to number goes on the plane. The number
// addresses an endpoint on this machine, not the public network, and a run that
// did not say so would look like it had placed a real call.
//
// It also says what the call does and does not prove. Measured 2026-08-20: an
// endpoint answers a request URI whose user part matches none of its accounts
// anyway, picking one of them, so a --to number is answered but the endpoint
// that answers is not the one the number names. That makes --to evidence that
// the dial-out path works, and no evidence at all about destination routing.
// The account parameter that sounds like it would fix this does not: a catchall
// account was tried and the unmatched call still went elsewhere.
func printPlaneLocalDial(out io.Writer, targetName, to string, plan *generate.TelephonyRuntimePlan) {
	names := make([]string, 0, len(plan.LocalEndpoints))
	for _, endpoint := range planeEndpoints(plan, ir.TelephonyRoleDestination) {
		names = append(names, endpoint.Name)
	}
	fmt.Fprintf(out, "%s: --to %s addresses an endpoint on this machine, not the public network.\n", targetName, to)
	fmt.Fprintf(out, "%s: one of the plane's endpoints answers it (%s), and which one is not\n", targetName, strings.Join(names, ", "))
	fmt.Fprintf(out, "%s: decided by the number. So this proves the agent can place a call and\n", targetName)
	fmt.Fprintf(out, "%s: that audio flows, and it proves nothing about which destination\n", targetName)
	fmt.Fprintf(out, "%s: a number reaches. A transfer is the check for that.\n", targetName)
}

// planeRun is one local plane run: the credential it minted, the address it
// advertises, the profile it brought up, and where its recordings go. It exists
// so the values that must agree are computed once, rather than by the Compose
// file and the printed instruction separately.
type planeRun struct {
	credential dialCredential
	advertise  string
	profile    string
	runID      string
	callDir    string
	// supplied is every environment name the plane sets for itself, so the
	// caller can stop demanding them from the author.
	supplied []string
	env      map[string]string
}

func planeIsSIP(plan *generate.TelephonyRuntimePlan) bool {
	return plan != nil && plan.LocalPlane == string(target.LocalPlaneSIP)
}

// startPlaneRun decides everything about this run that does not need the build
// directory: the credential, the address to advertise, and the environment the
// plane supplies in a carrier's place.
func startPlaneRun(plan *generate.TelephonyRuntimePlan, env []string) (*planeRun, error) {
	credential, err := newDialCredential()
	if err != nil {
		return nil, err
	}
	// An address already set wins. It is the escape hatch for a machine whose
	// interfaces this cannot read correctly, and for the loopback fallback,
	// which works for a softphone and not for a transfer.
	advertise := envValue(env, "UNMUTE_PLANE_ADVERTISE_IP")
	if advertise == "" {
		found, err := planeAdvertiseAddress(plan.PlaneSubnet)
		if err != nil {
			return nil, fmt.Errorf("%w; set UNMUTE_PLANE_ADVERTISE_IP to an address on this machine a softphone can reach", err)
		}
		advertise = found
	}
	run := &planeRun{
		credential: credential, advertise: advertise, profile: planeProfileSoftphone,
		env: planeEnvironment(plan, credential),
	}
	for name := range run.env {
		run.supplied = append(run.supplied, name)
	}
	slices.Sort(run.supplied)
	return run, nil
}

// prepare makes this run's call directory, under the build directory so the
// recordings sit with the package they came from.
func (run *planeRun) prepare(outDir string) error {
	run.runID = planeRunID()
	run.callDir = planeCallDir(outDir, run.runID)
	if err := os.MkdirAll(run.callDir, 0o755); err != nil {
		return fmt.Errorf("make this run's call directory: %w", err)
	}
	// The audio every endpoint plays. Written beside the build rather than in
	// the run's directory, because it is read-only input that is identical every
	// time, and rewritten only when it is missing or the wrong size, so a run
	// does not spend a second regenerating three megabytes of samples it already
	// has.
	if err := ensurePlaneFixture(outDir, planeFixtureSeconds); err != nil {
		return err
	}
	return nil
}

// ensurePlaneFixture writes the endpoints' audio if it is not already there at
// the right length.
func ensurePlaneFixture(outDir string, seconds int) error {
	path := filepath.Join(outDir, "plane-fixture.wav")
	want := int64(wavHeaderSize + callAudioRate*seconds*2)
	if info, err := os.Stat(path); err == nil {
		if info.Size() == want && !info.IsDir() {
			return nil
		}
		// A directory here is Docker's work, not ours: bring the Compose stack up
		// by hand before a run has ever written the fixture and the bind mount
		// creates one. Left in place, every endpoint answers and then fails to
		// start its audio, which reads as a broken plane rather than a missing
		// file. Removing it is safe: nothing else is ever called this.
		if info.IsDir() {
			if err := os.RemoveAll(path); err != nil {
				return fmt.Errorf("remove the empty directory Docker left in place of the fixture: %w", err)
			}
		}
	}
	samples := make([]int16, callAudioRate*seconds)
	for i := range samples {
		samples[i] = callFixtureSample(i)
	}
	if err := writeWAV(path, samples, callAudioRate); err != nil {
		return fmt.Errorf("write the endpoints' fixture: %w", err)
	}
	return nil
}

// apply puts the plane's decisions into the child environment. Called twice,
// because the call directory is only known once the build directory is, and
// setting a name twice is cheaper than threading two environments around.
func (run *planeRun) apply(env []string) []string {
	env = setChildEnv(env, "UNMUTE_PLANE_ADVERTISE_IP", run.advertise)
	// Every Compose invocation in the run has to see the same profile, or one
	// of them treats the other's containers as orphans and removes them.
	env = setChildEnv(env, "COMPOSE_PROFILES", run.profile)
	if run.runID != "" {
		env = setChildEnv(env, "UNMUTE_RUN_ID", run.runID)
	}
	for _, name := range run.supplied {
		env = setChildEnv(env, name, run.env[name])
	}
	return env
}

// credentialOrNone is the credential this run minted, or the zero value when
// there is no plane. Carrier mode has no per-run credential: the trunk it
// reaches is the carrier's, and the carrier's own credential guards it.
func (run *planeRun) credentialOrNone() dialCredential {
	if run == nil {
		return dialCredential{}
	}
	return run.credential
}

// --- transfer progress ------------------------------------------------------

// transferWatcher reads the log stream going past and says what a transfer did,
// in the seven outcomes the report distinguishes (gate P8, research R4).
//
// The log is the only source a developer's run has: the agent decides the
// transfer, and it says what happened in lines this file matches. Two of the
// seven cannot come from here and are deliberately absent rather than guessed:
//
//   - destination_reached on a *cold* transfer. The caller leaves for the
//     destination through the carrier, and nothing the agent can see says
//     whether they arrived. That is research R4, and it is why cold is
//     asserted at accepted and not at completion.
//   - not_acted_on. A caller that accepts a REFER and then ignores it looks
//     identical, from here, to one that acted on it. So an accepted cold
//     transfer prints what to look for instead of claiming a result.
//
// The unattended check reads the plane and the endpoints directly and does not
// need any of this, which is how it reports what a log cannot.
type transferWatcher struct {
	out        io.Writer
	targetName string
	report     *runReport
	pending    []byte
	current    *transferRecord
}

func (w *transferWatcher) Write(payload []byte) (int, error) {
	if w == nil {
		return len(payload), nil
	}
	w.pending = append(w.pending, payload...)
	for {
		index := bytes.IndexByte(w.pending, '\n')
		if index < 0 {
			// A partial line is kept rather than matched: a substring that
			// straddles two writes would otherwise be missed, and the log
			// arrives in whatever chunks the pipe hands over.
			if len(w.pending) > 64*1024 {
				w.pending = nil // a stream with no newlines is not our log
			}
			return len(payload), nil
		}
		line := string(w.pending[:index])
		w.pending = w.pending[index+1:]
		w.line(line)
	}
}

func (w *transferWatcher) line(text string) {
	switch {
	case strings.Contains(text, "human transfer fired:"):
		shape := coldTransfer
		if strings.Contains(text, "(warm)") {
			shape = warmTransfer
		}
		w.current = &transferRecord{Shape: shape, Outcome: transferRequested}
		w.say("transfer requested  shape=%s", shape)
	case strings.Contains(text, "cold transfer skipped: no phone caller"):
		// Not one of the seven. The transfer never left, and the usual cause is
		// a session that did not arrive by phone at all.
		w.say("transfer not attempted: no phone caller in the room")
	case strings.Contains(text, "cold transfer referring the caller out"):
		w.say("referring the caller out")
	case strings.Contains(text, "cold transfer completed"):
		w.advance(transferAccepted)
		w.say("transfer accepted. On a cold transfer that is where this can see: if")
		w.say("your softphone did not then dial the destination, it accepted the")
		w.say("request and ignored it, which is a transfer not acted on by the caller")
	case strings.Contains(text, "cold transfer failed"), strings.Contains(text, "warm transfer unavailable"):
		w.advance(transferUnavailableReturn)
		// The reason, verbatim, because "unavailable" covers a destination that
		// did not answer and a request that was refused before anything rang,
		// and on a live call those looked identical and cost half an hour.
		w.say("destination unavailable: %s", transferFailureReason(text))
		if strings.Contains(text, "cold transfer failed") {
			// The overwhelmingly likely cause on this plane, and it is not a
			// fault: a cold transfer hands the destination's address to the
			// *caller*, and a softphone on the host cannot route the plane's
			// container network. The headless profile, where every leg is a
			// container, is where a cold transfer can complete.
			w.say("a cold transfer is routed by the caller, and a softphone on this")
			w.say("machine cannot reach the plane's own network. The REFER above")
			w.say("carried the right destination, which is where this product's")
			w.say("responsibility ends; `make rig` completes the leg in containers")
		}
	case strings.Contains(text, "warm transfer dialling out"):
		w.advance(transferAccepted)
		w.say("transfer accepted, dialling the destination")
	case strings.Contains(text, "warm transfer merged"):
		// A merge is only reachable through the destination having answered, so
		// both steps are recorded: the state machine refuses the shortcut, and
		// a report that skipped one would be a report claiming less than the
		// log said.
		w.advance(transferDestinationReached)
		w.say("destination reached")
		w.advance(transferMerged)
		w.say("transfer merged")
	}
}

func (w *transferWatcher) advance(outcome transferOutcome) {
	if w.current == nil {
		// A transfer whose request line never arrived: report it rather than
		// dropping the outcome, because a lost first line is exactly when
		// somebody needs to see the rest.
		w.current = &transferRecord{Shape: coldTransfer, Outcome: transferRequested}
	}
	if err := w.current.advance(outcome); err != nil {
		w.say("transfer progress unexpected: %v", err)
		return
	}
	if w.report != nil {
		w.report.Transfers = append(w.report.Transfers, *w.current)
	}
}

func (w *transferWatcher) say(format string, args ...any) {
	fmt.Fprintf(w.out, "%s: %s\n", w.targetName, fmt.Sprintf(format, args...))
}

// printPlaneRecordings names the recordings this run produced, on the way out.
// Printed at the end as well as the directory at the start, because the useful
// moment is after the call: the question then is which legs were recorded, and
// a leg with no file is as much of an answer as one with.
func printPlaneRecordings(out io.Writer, targetName string, plan *generate.TelephonyRuntimePlan, plane *planeRun, report *runReport) {
	if plane == nil || plane.callDir == "" {
		return
	}
	for _, endpoint := range plan.LocalEndpoints {
		path := filepath.Join(plane.callDir, endpoint.Recording)
		info, err := os.Stat(path)
		switch {
		case err != nil:
			// Only worth a line for a leg that should have one. The caller
			// endpoint does not run on the softphone profile at all.
			continue
		case info.Size() <= wavHeaderSize:
			fmt.Fprintf(out, "%s: recording %s is empty, so no audio reached %s\n", targetName, path, endpoint.Name)
		default:
			fmt.Fprintf(out, "%s: recording %s\n", targetName, path)
		}
		if report != nil {
			report.Recordings = append(report.Recordings, path)
		}
	}
}

// transferFailureReason is the part of a transfer failure line after the colon
// that follows its duration, which is where the agent puts the cause. The whole
// line is the fallback: a reason nobody can read is worse than a long one.
func transferFailureReason(line string) string {
	marker := "s: "
	index := strings.Index(line, marker)
	if index < 0 {
		return strings.TrimSpace(line)
	}
	reason := strings.TrimSpace(line[index+len(marker):])
	// Log lines arrive as JSON on this route, so a trailing quote and comma
	// would otherwise be read out as part of the cause.
	return strings.TrimRight(reason, `",`)
}
