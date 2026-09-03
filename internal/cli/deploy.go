package cli

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/slng-ai/unmute/internal/generate"
	"github.com/slng-ai/unmute/internal/ir"
	"github.com/slng-ai/unmute/internal/target"
	"github.com/spf13/cobra"
)

// unmute opens no socket to the SLNG agents API, at compile time or any other
// time (internal/ir/validate_slng_test.go holds that). So `deploy` validates the
// package, compiles it, and hands the artifacts to the `voiceai` CLI, which is
// the tool that owns the account, the credential and the push.
//
// Shelling out is the whole design, not a shortcut. It means the push contract
// lives in one place — a released binary an author can run themselves — rather
// than being reimplemented here against an API this repository is not allowed to
// call. `--json` is parseable on success *and* on failure, which is what makes
// this readable rather than a screen-scrape.
const deployPushInstall = "brew install slng-ai/tap/voiceai"

// deployPushBinary is the tool that owns the account, the credential and every
// write. Named in internal/target because four documentation surfaces quote it
// and a second copy here is a second thing to get wrong.
var deployPushBinary = target.SlngPushBinary

type deployOptions struct {
	targets    []string
	dryRun     bool
	runSamples bool
	agentID    string
	label      string
	profile    string
	call       string
}

func newDeployCmd() *cobra.Command {
	var opts deployOptions
	cmd := &cobra.Command{
		Use:   "deploy [package-dir]",
		Short: "Compile a package and push it to SLNG.",
		Long: "Compile a package and push it to SLNG.\n\n" +
			"Validates the package, compiles each slng target, then pushes it with " +
			"`voiceai agents push`. Nothing on SLNG is created until every check passes, " +
			"so a run that reports problems has changed nothing.\n\n" +
			"The credential is read from " + target.SlngRouterKeyEnv + ", falling back to " +
			target.SlngPushCredentialEnv + " and then to whatever profile `voiceai login` " +
			"stored. The organisation a push resolved is always printed, because an " +
			"environment key and a stored profile can belong to different ones.\n\n" +
			"With no package-dir, the package is the current directory, so you can cd into " +
			"an agent and run this with no arguments.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := packageDir(cmd, args)
			if err != nil {
				return err
			}
			return runDeploy(cmd, dir, opts)
		},
	}
	f := cmd.Flags()
	f.StringSliceVar(&opts.targets, "target", nil, "slng target instance name (repeatable; default: every slng target)")
	f.BoolVar(&opts.dryRun, "dry-run", false, "check everything and report, changing nothing")
	f.BoolVar(&opts.runSamples, "run-samples", false, "run each tool's sample against your real dependencies")
	f.StringVar(&opts.agentID, "agent-id", "", "update this agent, when more than one has the package's name")
	f.StringVar(&opts.label, "label", "", "version label (default: the package name and a timestamp)")
	f.StringVar(&opts.profile, "profile", "", "voiceai credential profile to check and push with")
	f.StringVar(&opts.call, "call", "", "after a successful push, place one outbound call to this E.164 number")
	return cmd
}

func runDeploy(cmd *cobra.Command, dir string, opts deployOptions) error {
	out, errOut := cmd.OutOrStdout(), cmd.ErrOrStderr()
	printHeader(out, "deploy "+displayDir(dir))
	agent, selected, err := loadPackage(dir, opts.targets)
	if err != nil {
		return fmt.Errorf("deploy %s: %w", dir, err)
	}
	pushable := make([]ir.Target, 0, len(selected))
	for _, resolved := range selected {
		if resolved.Provider == ir.ProviderSlng {
			pushable = append(pushable, resolved)
		}
	}
	if len(pushable) == 0 {
		return fmt.Errorf("deploy %s: %s", dir, noSlngTargetGuidance(selected))
	}

	// Local refusals first. The slng target refuses things SLNG will not run, and
	// every one of them is cheaper to hear now than as a rejected push.
	report, validateErr := ir.Validate(agent, pushable, target.Default())
	printValidationReport(out, errOut, report)
	if validateErr != nil {
		return fmt.Errorf("deploy %s: %w", dir, validateErr)
	}
	// The generator warns about some of the same things the validator does, and
	// `deploy` is the one command that runs both. Printing a warning twice reads
	// as two problems.
	reported := map[string]bool{}
	for _, row := range report.PerTarget {
		for _, warning := range row.Warnings {
			reported[row.Name+": "+warning] = true
		}
	}

	// Looked up before anything is written, so a missing tool costs no compile.
	bin, lookErr := exec.LookPath(deployPushBinary)
	if lookErr != nil {
		return fmt.Errorf("deploy %s: %s", dir, missingPushToolGuidance())
	}
	// The same .env files `dev` reads, for the same reason: a key an author put in
	// an example's .env is the key they expect `deploy` to use.
	env := packageEnv(dir, errOut)
	key, keySource := deployCredential(env)
	if keySource == "" {
		warnf(errOut, "neither %s nor %s is set, so the push uses whatever profile `%s` stored; "+
			"check the organisation printed below is the one you meant\n",
			target.SlngRouterKeyEnv, target.SlngPushCredentialEnv, target.SlngLoginCommand)
	}

	// The environment the push will run under, handed to every account read too.
	// Reading with one credential and pushing with another would make every
	// finding a statement about an organisation this run never touches.
	pushEnv := env
	if key != "" {
		// Last duplicate wins in os/exec, so this overrides an inherited value.
		pushEnv = append(append([]string(nil), env...), target.SlngPushCredentialEnv+"="+key)
	}

	caps := target.Default()
	for _, resolved := range pushable {
		artifact, err := generate.Generate(agent, resolved, caps)
		if err != nil {
			return fmt.Errorf("deploy %s: %w", dir, err)
		}
		// The name this push writes, computed the same way the body was. The push
		// result does not carry it back, so reading it there yielded "".
		deployName := agent.DeployName(resolved)
		for _, warning := range artifact.Notes.Warnings {
			if reported[resolved.Name+": "+warning] {
				continue
			}
			warnf(errOut, "%s: %s\n", resolved.Name, warning)
		}

		// Generate wrote nothing: it returns an artifact and writeArtifactFiles
		// below is what puts it on disk. So the account is asked what it already
		// has here, between the two, and a run refused at this point leaves both
		// the build directory and the organisation exactly as it found them.
		runner := newVoiceaiRunner(bin, pushEnv, opts.profile)
		account, err := runPreflight(cmd, runner, resolved.Name, artifact.Requires, env)
		if err != nil {
			return fmt.Errorf("deploy %s: %w", dir, err)
		}

		outDir := filepath.Join(dir, "build", resolved.Name)
		if err := writeArtifactFiles(errOut, outDir, artifact.Files); err != nil {
			return fmt.Errorf("deploy %s: %w", dir, err)
		}
		if keySource != "" {
			fmt.Fprintf(out, "%s: credential from %s\n", resolved.Name, keySource)
		}
		fmt.Fprintf(out, "%s: compiled %s (%d files)\n", resolved.Name, outDir, len(artifact.Files))

		result, err := runPush(bin, outDir, env, key, opts)
		if err != nil {
			return fmt.Errorf("deploy %s: %w", dir, err)
		}
		if err := printPushResult(out, errOut, resolved.Name, deployName, outDir, keySource, account, result); err != nil {
			return fmt.Errorf("deploy %s: %w", dir, err)
		}
		// After the push and only after it succeeded, because both of these are
		// about an agent that now exists. A dry run created nothing, so there is
		// nothing to reach and nothing to call.
		if !opts.dryRun {
			in := cmd.InOrStdin()
			reportReach(in, out, errOut, runner, resolved.Name, deployName, result.Agent.ID, interactiveTerminal(in))
			if opts.call != "" {
				placeTestCall(out, errOut, runner, resolved.Name, result.Agent.ID, opts.call)
			}
		}
	}
	return nil
}

// runPreflight names the account, asks it what it has, and compares.
//
// The order matters. The organisation is printed before any finding, because a
// finding is a statement about one account and an environment key and a stored
// profile can belong to different ones. A run that cannot name the account at
// all stops here rather than reporting on an organisation it cannot identify.
func runPreflight(cmd *cobra.Command, runner *voiceaiRunner, name string, requires generate.Requirements, env []string) (slngAccount, error) {
	out, errOut := cmd.OutOrStdout(), cmd.ErrOrStderr()
	resources, err := readResources(runner, requires.ServerNames())
	if err != nil {
		return slngAccount{}, err
	}
	fmt.Fprintf(out, "%s: organisation %s\n", name, resources.Account)

	report := comparePreflight(requires, resources)
	// Between comparing and rendering, because a gap unmute can close should be
	// closed rather than reported. Secrets are the only kind it can: a missing
	// tool, MCP server or trunk is made in the dashboard, and for those the
	// report is the whole of what this command can do.
	in := cmd.InOrStdin()
	offerToFill(in, out, errOut, runner, &report, env, interactiveTerminal(in))
	return resources.Account, renderPreflight(out, errOut, name, report)
}

// noSlngTargetGuidance names what the package does declare and the block that
// would make it deployable. `deploy` pushes to SLNG and nowhere else: the other
// two targets emit a project somebody else's platform runs, which is
// `unmute compile` plus that platform's own deploy step.
func noSlngTargetGuidance(selected []ir.Target) string {
	declared := make([]string, 0, len(selected))
	for _, resolved := range selected {
		declared = append(declared, fmt.Sprintf("%s (%s)", resolved.Name, resolved.Provider))
	}
	have := "none"
	if len(declared) > 0 {
		have = strings.Join(declared, ", ")
	}
	return fmt.Sprintf("no slng target to deploy; this package declares %s\n"+
		"  deploy pushes to SLNG, which hosts the agent itself. Add a target to targets.yaml:\n"+
		"    targets:\n      slng:\n        provider: slng\n        deployment_region: any\n"+
		"  a livekit or pipecat target is compiled with `unmute compile` and deployed by that platform's own tool.", have)
}

func missingPushToolGuidance() string {
	return fmt.Sprintf("`%s` is not on your PATH, and it is the tool that pushes to SLNG\n"+
		"  install it: %s\n"+
		"  then sign in: %s\n"+
		"  the package is unchanged; nothing was compiled or pushed.",
		deployPushBinary, deployPushInstall, target.SlngLoginCommand)
}

// deployCredential resolves the key `deploy` hands to the push tool, out of the
// environment packageEnv merged.
//
// SLNG_API_KEY is read first because one SLNG key serves every SLNG role
// (target.SlngRouterKeyEnv) and it is the name an example's .env already
// carries, so an author who can run `unmute dev` can already deploy.
// VOICEAI_API_KEY is the name the push tool itself reads
// (target.SlngPushCredentialEnv), so a shell already set up for `voiceai` keeps
// working. With neither, the push tool falls back to its stored profile — which
// can belong to a *different* organisation, so the caller warns and the
// organisation the push resolved is always printed.
func deployCredential(env []string) (key, source string) {
	for _, name := range []string{target.SlngRouterKeyEnv, target.SlngPushCredentialEnv} {
		if value := strings.TrimSpace(envValue(env, name)); value != "" {
			return value, name
		}
	}
	return "", ""
}

// pushBlocker is one reason a push cannot proceed. The push tool reports every
// blocker together, each with what to do and the dashboard page that fixes it,
// which is why this is relayed rather than re-derived.
type pushBlocker struct {
	Kind   string   `json:"kind"`
	Items  []string `json:"items"`
	Detail string   `json:"detail"`
	URL    string   `json:"url"`
}

// pushResult is the `voiceai agents push --json` document. One struct covers all
// four of its shapes: a plan (--dry-run), an outcome, a blocked check, and a
// plain error. Fields absent from a given shape stay zero.
type pushResult struct {
	OK      bool   `json:"ok"`
	DryRun  bool   `json:"dry_run"`
	Changed bool   `json:"changed"`
	Error   string `json:"error"`

	Organisation struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"organisation"`

	// Agent carries no name: `voiceai agents push --json` reports the id and the
	// action only, and prints the name on its human stream alone. A Name field
	// here decoded as "" on every run, and "" compares equal to a free trunk's
	// empty in_use_by, which reported every unattached number as already
	// reaching the agent. The deployed name is ir.Agent.DeployName, which unmute
	// computed to build the body, so it is passed in rather than read back.
	Agent struct {
		ID     string `json:"id"`
		Action string `json:"action"`
	} `json:"agent"`

	Tools []struct {
		Name     string `json:"name"`
		Action   string `json:"action"`
		ToolType string `json:"toolType"`
		WillRun  bool   `json:"willRun"`
		Created  bool   `json:"created"`
		Updated  bool   `json:"updated"`
		Ran      string `json:"ran"`
		Error    string `json:"error"`
		// A version number when the tool published, false when publishing was
		// refused, absent when it was never reached.
		Published any `json:"published"`
	} `json:"tools"`

	Refs []struct {
		Name   string `json:"name"`
		Reused bool   `json:"reused"`
	} `json:"refs"`

	Removals []struct {
		Name string `json:"name"`
	} `json:"removals"`

	Overwrites []string      `json:"overwrites"`
	Blockers   []pushBlocker `json:"blockers"`

	// version is an object once a version was written and the string "unchanged"
	// when the push changed nothing, so it cannot be one Go type.
	Version json.RawMessage `json:"version"`
}

// runPush shells out and returns the parsed document. A non-zero exit is not an
// error here: the tool exits 1 whenever it refuses, and the reason is in the
// JSON. Only output that will not parse is an error, because that means the tool
// itself went wrong and there is nothing to report to the author.
func runPush(bin, dir string, env []string, key string, opts deployOptions) (pushResult, error) {
	args := []string{}
	if opts.profile != "" {
		// A root option, so it goes before the subcommand. After it, it is an
		// unknown flag; worse, a run that silently resolved a different account
		// from the one the preflight checked would make every finding a statement
		// about somewhere else.
		args = append(args, target.SlngProfileFlag, opts.profile)
	}
	args = append(args, "agents", "push", dir, "--json")
	if opts.dryRun {
		args = append(args, "--dry-run")
	}
	if opts.runSamples {
		args = append(args, "--run-samples")
	}
	if opts.agentID != "" {
		args = append(args, "--agent-id", opts.agentID)
	}
	if opts.label != "" {
		args = append(args, "--label", opts.label)
	}
	push := exec.Command(bin, args...)
	push.Env = env
	if key != "" {
		// Last duplicate wins in os/exec, so this overrides an inherited value.
		push.Env = append(push.Env, target.SlngPushCredentialEnv+"="+key)
	}
	var stdout, stderr bytes.Buffer
	push.Stdout, push.Stderr = &stdout, &stderr
	runErr := push.Run()

	var result pushResult
	if err := json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), &result); err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			detail = strings.TrimSpace(stdout.String())
		}
		if detail == "" && runErr != nil {
			detail = runErr.Error()
		}
		return result, fmt.Errorf("`%s %s` produced no readable result: %s",
			deployPushBinary, strings.Join(args, " "), detail)
	}
	return result, nil
}

// printPushResult renders one push. Facts go to stdout in the `name: fact` form
// the rest of the CLI uses; anything the author has to act on goes to stderr and
// comes back as an error, so the exit code matches what was printed.
func printPushResult(out, errOut io.Writer, name, deployName, outDir, keySource string, named slngAccount, result pushResult) error {
	// The organisation is named once per target, by the preflight, before any
	// finding: a finding is a statement about one account, so the reader needs
	// the account first. Restating it here was a second identical line.
	//
	// What is worth saying is a *difference*. The preflight reads with the
	// resolved credential and the push runs as its own process; if those ever
	// land in different organisations, every check just performed was about
	// somewhere else, and that is a warning rather than a duplicate.
	if org := organisationLine(result); org != "" && !sameOrganisation(named, result) {
		warnf(errOut, "%s: the checks ran against %s and the push reported %s. "+
			"Those are different organisations, so what was checked is not what was written\n",
			name, named, org)
	}
	// Pushing REPLACES: a reference or field the package no longer names is
	// removed, not merged. Which agent gets replaced is decided by the name in
	// the body, so the warning quotes that name and not this target's: they were
	// the same string until a package started naming its own deployments, and
	// printing the target here sent an author to rename the wrong thing.
	if result.Agent.Action == "update" {
		warnf(errOut, "%s: an agent named %q already exists%s, so this push replaces it rather than adding one; "+
			"the name is `name:` in agent.yaml joined to this target, so change `name:` or pass --agent-id to write a different agent\n",
			name, deployName, parenthesised(result.Agent.ID))
	}
	switch {
	case len(result.Blockers) > 0:
		printPushBlockers(errOut, name, outDir, result.Blockers)
		return fmt.Errorf("slng target %q: %s reported %s; nothing was created or changed",
			name, deployPushBinary, plural(len(result.Blockers), "problem"))
	case !result.OK:
		return pushFailure(errOut, name, keySource, result)
	case result.DryRun:
		printPushPlan(out, name, deployName, result)
	default:
		printPushOutcome(out, name, result)
	}
	return nil
}

// sameOrganisation compares by id, because that is the identity; a workspace can
// be renamed. A push that reported no id at all cannot be compared, and an
// unanswerable question is not a mismatch.
func sameOrganisation(named slngAccount, result pushResult) bool {
	if result.Organisation.ID == "" || named.Account.OrgID == "" {
		return true
	}
	return named.Account.OrgID == result.Organisation.ID
}

func organisationLine(result pushResult) string {
	switch {
	case result.Organisation.Name != "" && result.Organisation.ID != "":
		return fmt.Sprintf("%s (%s)", result.Organisation.Name, result.Organisation.ID)
	case result.Organisation.Name != "":
		return result.Organisation.Name
	default:
		return result.Organisation.ID
	}
}

// printPushBlockers relays each blocker as the push tool stated it: the items,
// its own `detail` sentence, and the dashboard page that fixes it.
//
// The kind is humanised rather than retitled. The push tool owns the wording of
// these, it lives in another repository, and no test here can hold a table of
// titles against it — so a kind unmute has never seen still reads correctly
// instead of rendering under a stale heading or none at all.
func printPushBlockers(errOut io.Writer, name, outDir string, blockers []pushBlocker) {
	fmt.Fprintf(errOut, "\nCannot deploy %s. %s:\n", name, plural(len(blockers), "problem"))
	for _, blocker := range blockers {
		fmt.Fprintf(errOut, "\n  %s (%d)\n", strings.ReplaceAll(blocker.Kind, "_", " "), len(blocker.Items))
		for _, item := range blocker.Items {
			fmt.Fprintf(errOut, "    %s\n", item)
		}
		if blocker.Detail != "" {
			fmt.Fprintf(errOut, "    %s\n", blocker.Detail)
		}
		if blocker.URL != "" {
			fmt.Fprintf(errOut, "    %s\n", blocker.URL)
		}
		if hint := blockerHint(blocker.Kind, outDir); hint != "" {
			fmt.Fprintf(errOut, "    %s\n", hint)
		}
	}
	fmt.Fprintln(errOut, "\n  nothing was created or changed.")
}

// blockerHint adds the one thing the push tool cannot know: where unmute put the
// files, and which unmute flag re-runs the step. Everything else a blocker needs
// to say is already in its own `detail`, and restating it here would be a second
// copy of a sentence this repository does not own.
func blockerHint(kind, outDir string) string {
	switch kind {
	case "sample_missing":
		return fmt.Sprintf("samples for this target belong in %s, and `unmute deploy --run-samples` runs them.",
			filepath.Join(outDir, "samples"))
	case "samples_not_enabled":
		return "re-run as `unmute deploy --run-samples`."
	case "agent_ambiguous":
		return "name the one to update with `unmute deploy --agent-id <id>`."
	default:
		return ""
	}
}

// pushFailure reports an `error` result: the push tool ran, and either could not
// read the account or stopped part-way through. `changed` is the load-bearing
// bit — a failure after the first write leaves tools behind, and saying so is
// the difference between a retry and a mess.
func pushFailure(errOut io.Writer, name, keySource string, result pushResult) error {
	message := result.Error
	if message == "" {
		message = "the push failed and reported no reason"
	}
	fmt.Fprintf(errOut, "\nCannot deploy %s:\n  %s\n", name, indentLines(message, "  "))
	for _, tool := range result.Tools {
		if tool.Error != "" {
			fmt.Fprintf(errOut, "  tool %s: %s\n", tool.Name, indentLines(tool.Error, "  "))
		}
	}
	if keySource == "" {
		fmt.Fprintf(errOut, "  set the key unmute reads and re-run:\n"+
			"    export %s=<your SLNG API key>\n"+
			"  or store a profile with `%s`. Keys: https://app.slng.ai/api-keys\n",
			target.SlngRouterKeyEnv, target.SlngLoginCommand)
	}
	if result.Changed {
		fmt.Fprintln(errOut, "  this push had already started writing, so some tools above exist on SLNG.")
	} else {
		fmt.Fprintln(errOut, "  nothing was created or changed.")
	}
	return fmt.Errorf("slng target %q: %s", name, message)
}

func printPushPlan(out io.Writer, name, deployName string, result pushResult) {
	fmt.Fprintf(out, "%s: agent %s — %s\n", name, deployName, result.Agent.Action)
	for _, tool := range result.Tools {
		run := "no run needed"
		if tool.WillRun {
			run = "will run its sample"
		}
		fmt.Fprintf(out, "%s: tool %s — %s, %s, %s\n", name, tool.Name, tool.Action, tool.ToolType, run)
	}
	for _, ref := range result.Refs {
		state := "new attachment"
		if ref.Reused {
			state = "existing attachment"
		}
		fmt.Fprintf(out, "%s: reference %s (%s)\n", name, ref.Name, state)
	}
	// Both of these are what an update destroys, so they are the point of a dry
	// run: pushing REPLACES, it does not merge.
	for _, removal := range result.Removals {
		fmt.Fprintf(out, "%s: would detach %s, which this package no longer names\n", name, removal.Name)
	}
	for _, field := range result.Overwrites {
		fmt.Fprintf(out, "%s: would overwrite %s, which differs from what the agent has now\n", name, field)
	}
	fmt.Fprintf(out, "%s: dry run, nothing was created or changed\n", name)
}

func printPushOutcome(out io.Writer, name string, result pushResult) {
	for _, tool := range result.Tools {
		fmt.Fprintf(out, "%s: tool %s %s\n", name, tool.Name, toolOutcome(tool.Created, tool.Updated, tool.Ran, tool.Published))
	}
	if result.Agent.ID != "" {
		fmt.Fprintf(out, "%s: agent %s %s\n", name, pastTense(result.Agent.Action), result.Agent.ID)
	}
	fmt.Fprintf(out, "%s: %s\n", name, versionLine(result.Version))
	fmt.Fprintf(out, "%s: deployed. Talk to it: %s\n", name,
		strings.Replace(target.SlngWebSessionCommand, "<agent_id>", result.Agent.ID, 1))
}

func toolOutcome(created, updated bool, ran string, published any) string {
	parts := make([]string, 0, 3)
	switch {
	case created:
		parts = append(parts, "created")
	case updated:
		parts = append(parts, "updated")
	}
	if ran != "" {
		parts = append(parts, "sample "+ran)
	}
	switch value := published.(type) {
	case float64:
		parts = append(parts, fmt.Sprintf("published v%d", int(value)))
	case bool:
		if !value {
			parts = append(parts, "NOT published")
		}
	}
	if len(parts) == 0 {
		return "unchanged"
	}
	return strings.Join(parts, ", ")
}

// versionLine reads the one field that is two types. The string form says the
// push changed nothing, which is a real outcome and not a failure: SLNG writes
// no version when a body matches what is already live.
func versionLine(raw json.RawMessage) string {
	var written struct {
		Number int    `json:"number"`
		Label  string `json:"label"`
	}
	if err := json.Unmarshal(raw, &written); err == nil && written.Number > 0 {
		return fmt.Sprintf("version %d labelled %q", written.Number, written.Label)
	}
	return "version unchanged, because nothing in this push changed the agent"
}

// pastTense renders the push tool's action word for a result line. An action
// this file has not seen is printed as it arrived rather than guessed at.
func pastTense(action string) string {
	switch action {
	case "create":
		return "created"
	case "update":
		return "updated"
	case "":
		return "written"
	default:
		return action
	}
}

func parenthesised(value string) string {
	if value == "" {
		return ""
	}
	return " (" + value + ")"
}

func plural(n int, noun string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", noun)
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

// indentLines keeps a multi-line message from breaking out of its block. The push
// tool's own errors are several lines when they list what it looked for.
func indentLines(text, indent string) string {
	return strings.ReplaceAll(strings.TrimSpace(text), "\n", "\n"+indent)
}

// --- what can actually reach the agent --------------------------------------

// reportReach says which number reaches the agent that was just pushed, and how
// to make it ring.
//
// Reporting only. Unmute buys no numbers and provisions no carrier state, so a
// trunk this names was attached by somebody in the dashboard, and a trunk it
// cannot find is not a trunk it will create.
//
// One read, and never during a preflight. Reading trunks enumerates every agent
// in the organisation, and `voiceai trunks get` costs the same as `trunks list`
// for a per-agent breakdown that `in_use_by` already answers.
func reportReach(in io.Reader, out, errOut io.Writer, runner *voiceaiRunner, name, agentName, agentID string, interactive bool) {
	trunks, notes, err := readTrunks(runner)
	for _, note := range notes {
		notef(errOut, "%s: %s\n", name, note)
	}
	if err != nil {
		// Never a deploy failure. The agent is live either way, and an unreadable
		// trunk listing says nothing about whether a call would connect.
		warnf(errOut, "%s: %s, so the numbers that reach this agent could not be read\n", name, err)
		return
	}

	// A candidate is an inbound trunk that is free *and* usable. Filtering on
	// usable matters: attaching a trunk the account reports as broken produces a
	// number that does not ring, and offering one as a choice invites exactly
	// that. Unusable free trunks are still worth naming, with the reason, because
	// "there is a number here and it does not work" is a different problem from
	// "there is no number", and the fix is in the dashboard either way.
	var attached, candidates, broken []slngTrunk
	for _, trunk := range trunks {
		switch {
		case trunk.InUseBy == agentName:
			attached = append(attached, trunk)
		case trunk.Direction != "inbound" || trunk.InUseBy != "":
			// An outbound trunk, or one already answering for another agent.
		case trunk.Usable:
			candidates = append(candidates, trunk)
		default:
			broken = append(broken, trunk)
		}
	}

	for _, trunk := range attached {
		fmt.Fprintf(out, "%s: %s trunk %s reaches this agent on %s%s\n",
			name, trunk.Direction, trunk.Name, numbersOf(trunk), unusableSuffix(trunk))
	}
	if len(attached) > 0 {
		return
	}

	// The ordinary state of a first deploy, and printing nothing here reads as a
	// failure to look rather than as an answer.
	fmt.Fprintf(out, "%s: no number reaches this agent yet\n", name)
	// Named whether or not there is anything to offer: an author looking for a
	// free number needs to know that one exists and is broken, rather than
	// concluding the organisation has none.
	for _, trunk := range broken {
		fmt.Fprintf(out, "%s:   %s on %s is free but cannot be used%s\n",
			name, trunk.Name, numbersOf(trunk), reasonSuffix(trunk))
	}
	if len(candidates) == 0 {
		fmt.Fprintf(out, "%s:   no usable free inbound trunk to attach. A number is bought and a trunk configured in the SLNG dashboard\n", name)
		return
	}
	offerTrunk(in, out, errOut, runner, name, agentID, candidates, interactive)
}

// offerTrunk asks which existing trunk should answer for this agent.
//
// The trunk already exists: somebody bought the number and configured the trunk
// in the dashboard, and unmute does neither. What is left is pointing the
// deployed agent at one of them, which is a single field on the agent and the
// last step between a successful deploy and a phone that rings.
//
// It runs after the push, not before, so it is idempotent and self-healing: if a
// push ever clears the field, the next deploy offers to set it again.
func offerTrunk(in io.Reader, out, errOut io.Writer, runner *voiceaiRunner, name, agentID string, candidates []slngTrunk, interactive bool) {
	if agentID == "" {
		// A dry run, or a push that reported no id. Nothing to attach to.
		return
	}
	if !interactive {
		fmt.Fprintf(out, "%s:   %s free. Attach one in the SLNG dashboard, or re-run this deploy from a terminal to choose:\n",
			name, plural(len(candidates), "inbound trunk"))
		for _, trunk := range candidates {
			fmt.Fprintf(out, "%s:     %s on %s%s\n", name, trunk.Name, numbersOf(trunk), unusableSuffix(trunk))
		}
		return
	}

	fmt.Fprintf(out, "\n%s free, and this agent has none. Which should answer for it?\n", plural(len(candidates), "inbound trunk is"))
	for index, trunk := range candidates {
		fmt.Fprintf(out, "  [%d] %s on %s%s\n", index+1, trunk.Name, numbersOf(trunk), unusableSuffix(trunk))
	}
	fmt.Fprintf(out, "  [0] none, leave it unattached\n")
	fmt.Fprint(out, "  choose [0]: ")

	answer, err := bufio.NewReader(in).ReadString('\n')
	if err != nil && strings.TrimSpace(answer) == "" {
		fmt.Fprintf(out, "\n  no answer to read, so no trunk was attached.\n")
		return
	}
	choice, convErr := strconv.Atoi(strings.TrimSpace(answer))
	switch {
	case strings.TrimSpace(answer) == "" || choice == 0:
		fmt.Fprintf(out, "  left unattached. `%s` shows the trunks again.\n", resourcesCommandName())
		return
	case convErr != nil || choice < 0 || choice > len(candidates):
		fmt.Fprintf(errOut, "  %q is not one of the choices, so no trunk was attached.\n", strings.TrimSpace(answer))
		return
	}

	trunk := candidates[choice-1]
	if err := runner.attachTrunk(agentID, trunk); err != nil {
		warnf(errOut, "%s: the agent deployed, but %s was not attached: %v\n", name, trunk.Name, err)
		return
	}
	fmt.Fprintf(out, "%s: %s attached. Call %s to reach this agent.\n", name, trunk.Name, numbersOf(trunk))
}

// resourcesCommandName is how an author sees the trunks again later, read from
// the command itself rather than written out here.
//
// A literal would be a second copy of a name newResourcesCmd already owns, and a
// rename would leave this diagnostic pointing at a command that does not exist.
// Cheap enough to derive that there is no reason not to.
func resourcesCommandName() string {
	return "unmute " + newResourcesCmd().Name()
}

// unusableSuffix relays the account's own reason a trunk will not work, rather
// than re-deriving one. A trunk that is both unusable and attached to no agent
// is withheld by the platform and appears in no listing at all, which is what
// the advisory on the error stream is about.
func unusableSuffix(trunk slngTrunk) string {
	if trunk.Usable {
		return ""
	}
	return " (not usable" + strings.TrimSuffix(reasonSuffix(trunk), ")") + ")"
}

// reasonSuffix is the account's own words for why a trunk will not work,
// relayed rather than re-derived. A trunk with no stated reason gets no
// invented one.
func reasonSuffix(trunk slngTrunk) string {
	if trunk.UnavailableReason == "" {
		return ""
	}
	return ": " + trunk.UnavailableReason
}

// numbersOf renders a trunk's numbers, and says so when it has none. An empty
// list printed bare reads as a formatting bug, and "no number" is usually the
// reason the trunk is unusable in the first place.
func numbersOf(trunk slngTrunk) string {
	if len(trunk.Numbers) == 0 {
		return "no number"
	}
	return strings.Join(trunk.Numbers, ", ")
}

// placeTestCall rings a phone, and only ever because this run was asked to.
//
// Telephony is verified on a deployed agent against a real carrier, and there is
// no local stand-in for it, so this is the last step of the only loop that
// proves a phone agent works. It is also a real call that costs real money, so
// it is never a default and never implied by a successful deploy.
func placeTestCall(out, errOut io.Writer, runner *voiceaiRunner, name, agentID, phone string) {
	if agentID == "" {
		warnf(errOut, "%s: the push reported no agent id, so no test call was placed\n", name)
		return
	}
	if err := runner.dispatchCall(agentID, phone); err != nil {
		// The deploy succeeded. A call that would not connect is worth saying and
		// is not a reason to report the deploy as failed.
		warnf(errOut, "%s: the agent deployed, but the test call to %s was not placed: %v\n", name, phone, err)
		return
	}
	fmt.Fprintf(out, "%s: calling %s from this agent now\n", name, phone)
}
