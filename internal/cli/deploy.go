package cli

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
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
// deployPushInstall has one owner in internal/target now, because the upgrade
// guidance for an incompatible CLI names it too and two spellings of an install
// command is one wrong install command.
const deployPushInstall = target.SlngPushInstall

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
		cache := newResolveCache()
		deployment, preflight, err := deployResolution(cmd, runner, cache, resolved.Name,
			artifact, agent, env, opts.dryRun)
		if err != nil {
			return fmt.Errorf("deploy %s: %w", dir, err)
		}
		account := deployment.Account

		// The refusal comes before the write, and that ordering is the promise
		// the whole command rests on: Generate returned an artifact and
		// writeArtifactFiles below is a separate step, so a run refused here has
		// touched neither the build directory nor the organisation.
		//
		// The blocked preview is therefore printed and not filed. renderPreflight
		// groups every gap with what asked for it and what fixes it, which is
		// the useful part; writing a report would mean creating the directory
		// this refusal exists to leave alone.
		if err := renderPreflight(out, errOut, resolved.Name, preflight); err != nil {
			return fmt.Errorf("deploy %s: %w", dir, err)
		}
		// The deferred notes go to the preview and to the report, and not to a
		// real run's output.
		//
		// They are facts rather than actions: "this value arrives when a call
		// starts" names nothing the reader has to do, and it would print on
		// every deploy of every package that injects a variable. That is the
		// shape this repository's own rule exists to stop, and the two places
		// that want it are the dry run, whose whole job is to describe what
		// will happen, and `deploy-report.json`, which is where a reader who
		// wants the detail looks.
		if opts.dryRun {
			for _, line := range deferredLines(deployment) {
				notef(errOut, "%s: %s\n", resolved.Name, line)
			}
		}

		outDir := filepath.Join(dir, "build", resolved.Name)

		// The staged copy carries the ids and versions this run checked. It is a
		// temporary directory, deleted on every exit, and it exists before the
		// build directory does because the guarded dry run below is what proves
		// the installed push tool can honour it.
		staged, err := stageResolvedBody(agent, resolved, deployment, outDir)
		if err != nil {
			return fmt.Errorf("deploy %s: %w", dir, err)
		}
		defer func() {
			// A staged body is a temporary directory holding one JSON document
			// and any tool samples. It is deleted on every exit, and a failure
			// to delete it is worth saying rather than swallowing: the next run
			// works either way, so this warns and never fails a deploy.
			if err := os.RemoveAll(staged); err != nil {
				warnf(errOut, "%s: the temporary resolved body at %s could not be removed: %v\n", resolved.Name, staged, err)
			}
		}()

		// One guarded dry run, always, before anything is written, and it does
		// three jobs at once.
		//
		// It proves the installed push tool implements the contract, by
		// returning the marker; an older one rejects the unknown option instead
		// and this is where an author hears about it. It surfaces the platform's
		// own blockers, which are a different set from the account checks above.
		// And it returns the agent this push would replace, which is where the
		// baseline read gets its id: reusing the push's own identity selection
		// rather than resolving the agent name a second time and possibly
		// differently.
		//
		// A real deploy therefore makes two pushes, one that changes nothing and
		// one that writes. That is deliberate: the alternative is reading the
		// agent AFTER replacing it, which would report the state this run just
		// created as the state it replaced, and every "previous version" in the
		// preview would be the version just attached.
		preview := opts
		preview.dryRun = true
		planned, err := runResolvedPush(bin, staged, env, key, account.Account.OrgID, preview)
		if err != nil {
			// A push that printed nothing readable, given the two options an
			// older tool has never heard of, is overwhelmingly an older tool:
			// it rejects the unknown option and writes to its error stream.
			// So the upgrade guidance rides along with what it actually said,
			// because the raw complaint on its own reads as a bug in unmute.
			return fmt.Errorf("deploy %s: slng target %q: %w\n  %s", dir, resolved.Name, err, target.SlngUpgradeGuidance)
		}
		if err := checkResolutionContract(planned); err != nil {
			return fmt.Errorf("deploy %s: slng target %q: %w", dir, resolved.Name, err)
		}
		readBaseline(runner, cache, &deployment, planned.Agent.ID)

		writeReport := func(report deployReport) {
			content, marshalErr := marshalDeployReport(report)
			if marshalErr != nil {
				warnf(errOut, "%s: the deployment report could not be written: %v\n", resolved.Name, marshalErr)
				return
			}
			if writeErr := os.WriteFile(filepath.Join(outDir, "deploy-report.json"), content, 0o644); writeErr != nil {
				warnf(errOut, "%s: the deployment report could not be written: %v\n", resolved.Name, writeErr)
			}
		}

		if err := writeArtifactFiles(errOut, outDir, artifact.Files); err != nil {
			return fmt.Errorf("deploy %s: %w", dir, err)
		}
		if keySource != "" {
			fmt.Fprintf(out, "%s: credential from %s\n", resolved.Name, keySource)
		}
		fmt.Fprintf(out, "%s: compiled %s (%d files)\n", resolved.Name, outDir, len(artifact.Files))

		// The dry run's own result is the preview. A real run pushes again, for
		// real, and its result is what actually happened.
		result := planned
		if !opts.dryRun && len(planned.Blockers) == 0 && planned.OK {
			result, err = runResolvedPush(bin, staged, env, key, account.Account.OrgID, opts)
			if err != nil {
				return fmt.Errorf("deploy %s: %w", dir, err)
			}
		}

		outcome := "deployed"
		switch {
		case len(result.Blockers) > 0 || !result.OK:
			outcome = "blocked"
			if result.Changed {
				outcome = "partial"
			}
		case opts.dryRun:
			outcome = "previewed"
		}
		report := buildDeployReport(resolved.Name, deployName, result.Agent.ID, result.Agent.Action,
			opts.dryRun, outcome, deployment, preflight)
		writeReport(report)
		if opts.dryRun {
			printResolvedPlan(out, resolved.Name, report)
		}
		// Before printPushResult, so the attached versions sit above the line
		// that closes the run rather than after it. Guarded on the push having
		// actually succeeded: a blocked or failed result has attached nothing,
		// and saying otherwise is the one thing this output must never do.
		if !opts.dryRun && result.OK && len(result.Blockers) == 0 {
			printAttachedVersions(out, resolved.Name, report)
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

// runResolvedPush hands the staged body to a push running in the guarded mode.
//
// Two flags separate it from runPush: --require-resolved, which says attach
// exactly what this body carries rather than resolving names again, and
// --expect-org, which says refuse before writing if the credential belongs to
// another organisation. A matching profile name is not evidence of that: an
// exported key and a stored profile can resolve to different organisations, and
// nothing else on screen would say which one was written to.
//
// runPreflight used to live here. deployResolution owns that flow now, because
// resolving a published version, checking a binding against it and reading the
// agent it would replace are all part of the same ordered run, and splitting
// them across two functions made the order the thing nobody could see.
func runResolvedPush(bin, staged string, env []string, key, organisation string, opts deployOptions) (pushResult, error) {
	// An empty organisation is refused here rather than sent. `--expect-org`
	// with nothing after it is a malformed command line, and the guarded push
	// refuses the flag's absence anyway, so the useful message is this one: the
	// account could not be identified, which is a credential problem rather than
	// a push problem.
	if organisation == "" {
		return pushResult{}, fmt.Errorf(
			"this run could not establish which organisation it resolved, and a checked deployment has to name it so the push refuses if its own credential belongs to another: check the key or the profile with `%s`",
			target.SlngWhoami)
	}
	return runPushWith(bin, staged, env, key, opts, []string{
		target.SlngRequireResolvedFlag, target.SlngExpectOrgFlag, organisation,
	})
}

// buildDeployReport assembles the report from what the run established.
func buildDeployReport(
	name, deployName, agentID, action string, dryRun bool, outcome string,
	deployment slngDeployment, preflight preflightReport,
) deployReport {
	// Every reference this deploy attaches, in one slice.
	//
	// Builtins as well as hosted references: both are attached, and a reader
	// asking "what is running" wants the curated capability named too. It is
	// one slice because the rows and the removals are two readings of the same
	// set, and giving them separate arguments is what let a package's
	// `end_call` be reported as attached and detached in one preview.
	attaching := append(append([]resolvedTool(nil), deployment.Resolution.Tools...), deployment.Resolution.Builtins...)
	return deployReport{
		Target: name, Organisation: deployment.Account.String(),
		Agent: deployName, AgentID: agentID, Action: action,
		DryRun: dryRun, Outcome: outcome,
		Tools: compareAttachments(attaching,
			deployment.Proposed,
			deployment.Live, deployment.LiveKnown, deployment.Previous),
		MCP: compareMCPAttachments(deployment.Resolution.MCP, deployment.Resolution.Servers,
			deployment.Refreshed, deployment.Live, deployment.LiveKnown),
		Removals:             attachmentRemovals(deployment.Live, deployment.LiveKnown, attaching, deployment.Resolution.MCP),
		Checks:               reportChecks(preflight.Findings, deferredBindings(deployment.Bindings)),
		SideEffects:          deployment.SideEffects,
		PreviousStateUnknown: !deployment.LiveKnown,
	}
}

// printAttachedVersions names what a successful deploy actually attached, which
// is the line an author needs after the fact: the push reports that it wrote,
// and this reports which version of each tool it wrote.
func printAttachedVersions(out io.Writer, name string, report deployReport) {
	for _, row := range report.Tools {
		label := row.Tool
		if row.Source != row.Tool {
			label = fmt.Sprintf("%s (%s)", row.Tool, row.Source)
		}
		fmt.Fprintf(out, "%s: attached %s v%d\n", name, label, row.ProposedVersion)
	}
	for _, row := range report.MCP {
		fmt.Fprintf(out, "%s: attached %s %s\n", name, row.Server, row.Tool)
	}
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

	// ResolutionContract is the marker a push implementing the guarded contract
	// returns, and its absence is what an older tool looks like from here.
	//
	// Zero means absent, and absent is a refusal rather than a fallback: a push
	// that does not honour `--require-resolved` resolves every name again and
	// attaches whatever is newest, which is not the version this run checked.
	ResolutionContract int `json:"resolution_contract"`
}

// runPush shells out and returns the parsed document. A non-zero exit is not an
// error here: the tool exits 1 whenever it refuses, and the reason is in the
// JSON. Only output that will not parse is an error, because that means the tool
// itself went wrong and there is nothing to report to the author.
func runPush(bin, dir string, env []string, key string, opts deployOptions) (pushResult, error) {
	return runPushWith(bin, dir, env, key, opts, nil)
}

// runPushWith is runPush plus whatever flags the caller adds after the standard
// ones, which is how the guarded mode gets its two without a second copy of the
// argv assembly.
func runPushWith(bin, dir string, env []string, key string, opts deployOptions, extra []string) (pushResult, error) {
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
	args = append(args, extra...)
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
		// The push has reported a removal with no name. Printing it produced
		// "would detach , which this package no longer names", which tells a
		// reader that something is being lost and refuses to say what. Unmute's
		// own preview above walks the live agent and names every attachment it
		// would detach, so the useful line here is the one that says this entry
		// added nothing rather than a sentence with a hole in it.
		if strings.TrimSpace(removal.Name) == "" {
			continue
		}
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
