package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
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
const (
	deployPushBinary  = "voiceai"
	deployPushInstall = "brew install slng-ai/tap/voiceai"
)

type deployOptions struct {
	targets    []string
	dryRun     bool
	runSamples bool
	agentID    string
	label      string
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
	return cmd
}

func runDeploy(cmd *cobra.Command, dir string, opts deployOptions) error {
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

	out, errOut := cmd.OutOrStdout(), cmd.ErrOrStderr()
	printHeader(out, "deploy "+displayDir(dir))

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
		fmt.Fprintf(errOut, "warning: neither %s nor %s is set, so the push uses whatever profile `%s` stored; "+
			"check the organisation printed below is the one you meant\n",
			target.SlngRouterKeyEnv, target.SlngPushCredentialEnv, target.SlngLoginCommand)
	}

	caps := target.Default()
	for _, resolved := range pushable {
		artifact, err := generate.Generate(agent, resolved, caps)
		if err != nil {
			return fmt.Errorf("deploy %s: %w", dir, err)
		}
		for _, warning := range artifact.Notes.Warnings {
			if reported[resolved.Name+": "+warning] {
				continue
			}
			fmt.Fprintf(errOut, "warning: %s: %s\n", resolved.Name, warning)
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
		if err := printPushResult(out, errOut, resolved.Name, outDir, keySource, result); err != nil {
			return fmt.Errorf("deploy %s: %w", dir, err)
		}
	}
	return nil
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

	Agent struct {
		ID     string `json:"id"`
		Name   string `json:"name"`
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
	args := []string{"agents", "push", dir, "--json"}
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
func printPushResult(out, errOut io.Writer, name, outDir, keySource string, result pushResult) error {
	if org := organisationLine(result); org != "" {
		fmt.Fprintf(out, "%s: organisation %s\n", name, org)
	}
	// Pushing REPLACES: a reference or field the package no longer names is
	// removed, not merged. Which agent gets replaced is decided by the name in
	// the body, so the warning quotes that name and not this target's: they were
	// the same string until a package started naming its own deployments, and
	// printing the target here sent an author to rename the wrong thing.
	if result.Agent.Action == "update" {
		fmt.Fprintf(errOut, "warning: %s: an agent named %q already exists%s, so this push replaces it rather than adding one; "+
			"the name is `name:` in agent.yaml joined to this target, so change `name:` or pass --agent-id to write a different agent\n",
			name, result.Agent.Name, parenthesised(result.Agent.ID))
	}
	switch {
	case len(result.Blockers) > 0:
		printPushBlockers(errOut, name, outDir, result.Blockers)
		return fmt.Errorf("slng target %q: %s reported %s; nothing was created or changed",
			name, deployPushBinary, plural(len(result.Blockers), "problem"))
	case !result.OK:
		return pushFailure(errOut, name, keySource, result)
	case result.DryRun:
		printPushPlan(out, name, result)
	default:
		printPushOutcome(out, name, result)
	}
	return nil
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

func printPushPlan(out io.Writer, name string, result pushResult) {
	fmt.Fprintf(out, "%s: agent %s — %s\n", name, result.Agent.Name, result.Agent.Action)
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
