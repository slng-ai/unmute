package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"

	"github.com/slng-ai/unmute/internal/spec"
	"github.com/slng-ai/unmute/internal/target"
)

// Reading the account, for the same reason `deploy` shells out to push: unmute
// opens no socket to the SLNG API, at compile time or any other time
// (internal/ir/validate_slng_test.go holds that). The `voiceai` binary owns the
// account, the credential and every write, so every question about what the
// organisation already has is asked through it.
//
// This file shells out and decodes. It decides nothing. preflight.go decides and
// runs no subprocess, which is what lets every rule in this feature be tested
// table-driven with no network and no stub.

// unchecked is a read that could not be made: the binary is missing or too old,
// the account is unreachable, the caller lacks permission, or the output would
// not decode.
//
// It is a distinct type rather than a plain error because the difference between
// "the account does not have this" and "I could not find out" is the whole
// safety argument for this feature. A resource positively found absent stops a
// deploy. A read that failed must not, or an old `voiceai`, a restricted CI
// network or a missing scope becomes a deploy outage that the push itself would
// never have produced. Collapsing the two states is the one mistake here that
// makes things worse than before.
type unchecked struct {
	Command string // what was being asked, as an author would type it
	Reason  string // what went wrong, in the tool's own words where it had any
}

func (u *unchecked) Error() string {
	return fmt.Sprintf("`%s` could not be run: %s", u.Command, u.Reason)
}

// voiceaiRunner runs one `voiceai` subcommand. The zero value is not useful; use
// newVoiceaiRunner.
type voiceaiRunner struct {
	bin     string
	env     []string
	profile string
	// notes collects the advisory prose the commands write to their error stream,
	// separately from the JSON on their output stream. The trunk commands use it
	// to say that a trunk both unusable and attached to no agent is withheld and
	// appears nowhere, which is a caveat worth relaying and not data to parse.
	notes []string
	// reads counts invocations per command, so a test can hold the whole deploy
	// to one read per resource kind. Cost here is not academic: the trunk read is
	// organisation-wide and enumerates every agent.
	reads map[string]int
}

func newVoiceaiRunner(bin string, env []string, profile string) *voiceaiRunner {
	return &voiceaiRunner{bin: bin, env: env, profile: profile, reads: map[string]int{}}
}

// argv assembles one invocation. The profile is a *root* option on the CLI, so
// it goes before the subcommand. After it, it is an unknown flag on the
// subcommand, or worse, silently a different account from the one the push
// writes to, which would make every finding a statement about somewhere else.
func (r *voiceaiRunner) argv(command target.SlngCommand) []string {
	args := make([]string, 0, len(command)+3)
	if r.profile != "" {
		args = append(args, target.SlngProfileFlag, r.profile)
	}
	args = append(args, command...)
	return append(args, "--json")
}

// read runs one command and decodes its document into out.
//
// Every failure is an *unchecked, never a bare error: a non-zero exit, output
// that will not decode, and a binary that will not start all mean the same thing
// to the caller, which is that this question went unanswered.
func (r *voiceaiRunner) read(command target.SlngCommand, out any) error {
	r.reads[strings.Join(command, " ")]++

	args := r.argv(command)
	cmd := exec.Command(r.bin, args...)
	cmd.Env = r.env
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	runErr := cmd.Run()

	// Advisories arrive here whether or not the command succeeded, and they are
	// worth keeping either way.
	if note := strings.TrimSpace(stderr.String()); note != "" {
		r.notes = append(r.notes, note)
	}

	if err := json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), out); err != nil {
		return &unchecked{Command: command.String(), Reason: readFailure(stdout, stderr, runErr)}
	}
	// A command can exit non-zero and still print a usable document; `secret get`
	// does exactly that when a name is absent. But a decode that succeeded on a
	// failed run is only trustworthy if the tool meant to print it, and there is
	// no way to tell from here, so a non-zero exit is unchecked regardless.
	if runErr != nil {
		return &unchecked{Command: command.String(), Reason: readFailure(stdout, stderr, runErr)}
	}
	return nil
}

// readFailure picks the most informative thing the tool said. stderr first,
// because a tool that failed usually explains itself there; then stdout, which
// carries the explanation when the tool prints structured errors; then the exit
// status, which is all that is left when it printed nothing at all.
func readFailure(stdout, stderr bytes.Buffer, runErr error) string {
	for _, candidate := range []string{strings.TrimSpace(stderr.String()), strings.TrimSpace(stdout.String())} {
		if candidate != "" {
			return firstLine(candidate)
		}
	}
	if runErr != nil {
		return runErr.Error()
	}
	return "it printed nothing that could be read"
}

func firstLine(text string) string {
	if index := strings.IndexByte(text, '\n'); index >= 0 {
		return strings.TrimSpace(text[:index]) + " [...]"
	}
	return text
}

// --- the account's own shapes -----------------------------------------------
//
// Only the fields this feature reads are declared. Everything else the commands
// return is ignored on decode, so a field added upstream is not a breakage here.
// Captured documents live in testdata/voiceai; see that directory's README for
// which parts are real and which are synthesised.

// slngAccount is `voiceai whoami`. A lightweight auth probe that spends no
// credits, which is what makes it affordable on every deploy.
type slngAccount struct {
	OK      bool   `json:"ok"`
	Profile string `json:"profile"`
	Account struct {
		OrgID   string `json:"org_id"`
		OrgName string `json:"org_name"`
	} `json:"account"`
}

// String renders the account for the one line a run prints before it checks
// anything. An environment key and a stored profile can belong to different
// organisations, so this is not decoration.
func (a slngAccount) String() string {
	name := a.Account.OrgName
	switch {
	case name != "" && a.Account.OrgID != "":
		name = fmt.Sprintf("%s (%s)", name, a.Account.OrgID)
	case name == "":
		name = a.Account.OrgID
	}
	if a.Profile != "" {
		return fmt.Sprintf("%s, profile %s", name, a.Profile)
	}
	return name
}

// slngVaultEntry is one row of `voiceai secret list`. Three fields answer every
// vault question this feature asks, which is why there is no lookup per name.
type slngVaultEntry struct {
	Name string `json:"name"`
	// Kind is "secret" or "variable". They are created differently and used
	// differently, so an entry under the wrong one is a mismatch rather than a
	// hit: creating it again would not help.
	Kind string `json:"kind"`
	// HasValue is the populated bit. Values are never readable back, so this is
	// the only way to tell a real credential from a name someone reserved.
	HasValue bool `json:"has_value"`
}

// slngAccountTool is one row of `voiceai tool list`.
//
// The load-bearing fact about this command is that curated capabilities appear
// in it as ordinary tools, with ids and versions. That is what turns a builtin
// check from a guess into a decidable question.
type slngAccountTool struct {
	Name     string `json:"name"`
	ToolType string `json:"tool_type"`
	// LatestVersion is what makes the hosted-tool drift check free. This listing
	// carries it, so comparing a committed mirror's version against the
	// organisation costs no extra read: one `tool list` answers both "does the
	// name exist" and "has it moved". A read per hosted tool would have been the
	// alternative, on every deploy.
	LatestVersion int `json:"latest_version"`
}

// slngMCPServer is one row of `voiceai mcp list`.
type slngMCPServer struct {
	Name      string `json:"name"`
	Transport string `json:"transport"`
	// CapabilityStatus and the tool list both come from the last stored
	// capability probe, not from a live call. A server can be listed healthy and
	// be unreachable, so nothing built on this may be rendered as a promise that
	// the server works.
	CapabilityStatus string `json:"capability_status"`
	CapabilityError  string `json:"capability_error"`
}

// slngMCPTool is one row of `voiceai mcp tools <server>`, read from the same
// stored probe.
type slngMCPTool struct {
	Name string `json:"name"`
}

// slngTrunk is one row of `voiceai trunks list`.
type slngTrunk struct {
	Direction string `json:"direction"`
	// ID is what an attachment names. The listing is the only place it appears,
	// which is why attaching reads the list first rather than taking a name.
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Numbers []string `json:"numbers"`
	Status  string   `json:"status"`
	Usable  bool     `json:"usable"`
	// UnavailableReason is the account's own words for why a trunk is not usable,
	// relayed rather than re-derived.
	UnavailableReason string `json:"unavailable_reason"`
	// InUseBy is one agent name, or empty. A string and not a list: verified
	// against a live account, where a trunk in use decodes as a single name.
	// Matching the pushed agent's name against it answers "which number reaches
	// my agent" without the per-agent breakdown `trunks get` would cost.
	InUseBy string `json:"in_use_by"`
}

// slngResources is what one preflight learned about the account.
type slngResources struct {
	Account   slngAccount
	Vault     []slngVaultEntry
	Tools     []slngAccountTool
	MCPServer []slngMCPServer
	// MCPTools is per server name, populated only for servers that exist. A
	// server that is already a finding is not interrogated further: there is
	// nothing to ask, and asking would report a second problem caused by the
	// first.
	MCPTools map[string][]slngMCPTool
	// Unchecked is every read that could not be made. Kept beside the answers
	// rather than returned as an error, because a preflight with three answers
	// and one gap is still worth reading, and the gap must never render as a
	// pass.
	Unchecked []*unchecked
	Notes     []string
}

// readResources asks the account everything the preflight needs, in a fixed
// number of reads: one for the account itself, one per resource kind, then one
// per distinct MCP server the package actually names and the account actually
// has.
//
// Failing to name the account is the one fatal case. Every finding a preflight
// could report would otherwise be a statement about an organisation this run
// cannot identify, and the author has no way to tell which one.
func readResources(runner *voiceaiRunner, servers []string) (slngResources, error) {
	resources := slngResources{MCPTools: map[string][]slngMCPTool{}}

	if err := runner.read(target.SlngWhoami, &resources.Account); err != nil {
		return resources, fmt.Errorf("cannot tell which SLNG organisation this would deploy to: %w", err)
	}

	// Each of the three listings is independent: one failing leaves the other two
	// worth having, so a failure is recorded and the run continues.
	resources.Unchecked = appendUnchecked(resources.Unchecked, runner.read(target.SlngSecretList, &resources.Vault))
	resources.Unchecked = appendUnchecked(resources.Unchecked, runner.read(target.SlngToolList, &resources.Tools))
	mcpErr := runner.read(target.SlngMCPList, &resources.MCPServer)
	resources.Unchecked = appendUnchecked(resources.Unchecked, mcpErr)

	if mcpErr == nil {
		for _, name := range servers {
			if !hasMCPServer(resources.MCPServer, name) {
				// Absent servers are the preflight's finding to report, not a read to
				// attempt.
				continue
			}
			var tools []slngMCPTool
			if err := runner.read(target.SlngMCPTools.With(name), &tools); err != nil {
				resources.Unchecked = appendUnchecked(resources.Unchecked, err)
				continue
			}
			resources.MCPTools[name] = tools
		}
	}

	resources.Notes = runner.notes
	return resources, nil
}

// readTool fetches one tool's whole definition, which is what a hosted
// reference mirrors.
//
// It decodes into spec.Mirror rather than a shape of its own, and that is
// deliberate: the same struct is the file `unmute pull` writes, so the platform
// field names have one owner and there is no second place for them to drift.
// The fields the platform returns and the mirror does not keep are listed on
// that type, each with why.
//
// Going through runner.read means a failure is an *unchecked, so "this
// organisation has no such tool" and "I could not ask" stay different answers.
// That difference is the whole safety argument next door in the preflight, and
// it matters as much here: a pull that wrote an empty mirror because a read
// failed would commit a lie.
func readTool(runner *voiceaiRunner, name string) (spec.Mirror, error) {
	var mirror spec.Mirror
	if err := runner.read(target.SlngToolGet.With(name), &mirror); err != nil {
		return spec.Mirror{}, err
	}
	return mirror, nil
}

// readTrunks is deliberately not called by readResources, and no preflight ever
// reads trunks.
//
// Two callers need it and neither is the preflight: `deploy` reads it after a
// successful push to say which number reaches the agent, and `resources` lists
// it while an author is still writing the package. Keeping the reader here and
// its callers in their own commands is what lets both work without either
// depending on the other, and what keeps a preflight from paying for an
// organisation-wide enumeration it has no use for.
func readTrunks(runner *voiceaiRunner) ([]slngTrunk, []string, error) {
	var trunks []slngTrunk
	if err := runner.read(target.SlngTrunksList, &trunks); err != nil {
		return nil, runner.notes, err
	}
	return trunks, runner.notes, nil
}

func appendUnchecked(list []*unchecked, err error) []*unchecked {
	var missed *unchecked
	if errors.As(err, &missed) {
		return append(list, missed)
	}
	return list
}

func hasMCPServer(servers []slngMCPServer, name string) bool {
	for _, server := range servers {
		if server.Name == name {
			return true
		}
	}
	return false
}

// createSecret creates or fills one vault entry.
//
// Two paths, and the difference is who handles the value.
//
// With no value in hand, unmute runs `voiceai secret create` with the terminal
// attached and lets that tool prompt. It masks the input, it owns the vault, and
// the value never enters unmute's address space at all. That is a stronger
// guarantee than careful handling, and it costs no dependency: implementing a
// masked prompt here would mean adding golang.org/x/term for one field.
//
// With a value already read from the package's own .env, it is piped on stdin.
// There is deliberately no --value flag on the CLI, because argv lands in shell
// history and in `ps`, so stdin is the only way to pass one without publishing
// it.
func (r *voiceaiRunner) createSecret(name, kind, value string, overwrite bool, stdin io.Reader, stdout, stderr io.Writer) error {
	command := target.SlngSecretCreate.With(name)
	if kind == "variable" {
		command = command.With("--kind", "variable")
	}
	if overwrite {
		// The CLI reads the vault first and never overwrites silently: an entry
		// that already exists is named and confirmed, and a non-interactive run
		// refuses rather than guessing. Filling an empty entry is exactly that
		// case, so the consent is given in advance.
		command = command.With("--overwrite")
	}

	cmd := exec.Command(r.bin, r.write(command)...)
	cmd.Env = r.env
	if value != "" {
		cmd.Stdin = strings.NewReader(value)
	} else {
		cmd.Stdin = stdin
	}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("`%s`: %w", command, err)
	}
	return nil
}

// write is argv for a command that changes something, as opposed to read's,
// which appends --json. Kept separate so that nothing can accidentally ask a
// write for machine-readable output and get a prompt instead.
func (r *voiceaiRunner) write(command target.SlngCommand) []string {
	args := make([]string, 0, len(command)+2)
	if r.profile != "" {
		args = append(args, target.SlngProfileFlag, r.profile)
	}
	return append(args, command...)
}

// dispatchCall rings a real phone, so nothing calls this unasked.
//
// The response is decoded into `any` because nothing here reads a field of it.
// What matters is whether the call was accepted, which is the exit status, and
// pinning a shape this code does not use would turn an upstream field rename
// into a failed call that actually connected.
func (r *voiceaiRunner) dispatchCall(agentID, phone string) error {
	var accepted any
	return r.read(target.SlngCallDispatch.With(agentID, "--phone", phone), &accepted)
}

// attachTrunk points one agent at one SIP trunk.
//
// This is a PATCH (`voiceai agents update` is partial), so it sets one field and
// touches nothing else on the agent. That matters: the alternative would be
// reading the agent, editing it and writing it back, which races with anything
// else changing that agent and would let a stale read clobber it.
//
// It attaches an existing trunk. It does not create one, buy a number, or
// configure anything carrier-side: those stay in the SLNG dashboard, and unmute
// has no command for them.
func (r *voiceaiRunner) attachTrunk(agentID string, trunk slngTrunk) error {
	field := "sip_inbound_trunk_id"
	if trunk.Direction == "outbound" {
		field = "sip_outbound_trunk_id"
	}
	body, err := json.Marshal(map[string]string{field: trunk.ID})
	if err != nil {
		return err
	}
	// `--file -` reads the body from stdin. The trunk id is not a secret, but
	// keeping the body off argv is the same habit that keeps values off it.
	command := target.SlngAgentUpdate.With(agentID, "--file", "-")
	cmd := exec.Command(r.bin, r.write(command)...)
	cmd.Env = r.env
	cmd.Stdin = bytes.NewReader(body)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("`%s`: %s", command, readFailure(stdout, stderr, err))
	}
	return nil
}
