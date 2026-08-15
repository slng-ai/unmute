package cli

import (
	"fmt"
	"strings"

	"github.com/slng-ai/unmute/internal/skill"
	"github.com/slng-ai/unmute/internal/style"
	"github.com/spf13/cobra"
)

// newSkillCmd builds the `skill` command. It sits outside the path from nothing
// to a spoken agent (constitution V): it touches no package, makes no network
// call, and nobody has to run it before init, validate, compile, or dev.
func newSkillCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "skill",
		Short: "Install the coding-agent skill into a project.",
		Args:  cobra.NoArgs,
		RunE:  func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	cmd.AddCommand(newSkillInstallCmd())
	return cmd
}

func newSkillInstallCmd() *cobra.Command {
	var (
		agents []string
		dir    string
		force  bool
	)
	cmd := &cobra.Command{
		Use:   "install",
		Short: "Write the Unmute skill so your coding assistant can build agents.",
		Long: "Write the Unmute skill into this project, so a coding assistant knows how to\n" +
			"author an Unmute package. The files travel with the repository, so a team\n" +
			"shares one skill. Nothing is downloaded: the skill ships inside this binary.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runSkillInstall(cmd, agents, dir, force)
		},
	}
	cmd.Flags().StringSliceVar(&agents, "agent", nil, "Assistants to install for: "+strings.Join(skill.AssistantNames(), ", ")+" (default all)")
	cmd.Flags().StringVar(&dir, "dir", ".", "Project directory to install into")
	cmd.Flags().BoolVar(&force, "force", false, "Overwrite files that changed after they were installed")
	return cmd
}

func runSkillInstall(cmd *cobra.Command, agents []string, dir string, force bool) error {
	dests, err := skill.Destinations(agents)
	if err != nil {
		return fmt.Errorf("skill install: %w", err)
	}

	bundle := skill.New(cmd.Root().Version)

	// Every destination is planned before any of them is written, so a refusal
	// names every offending file across the whole install rather than stopping
	// at the first one.
	plans := make([]skill.DestinationPlan, 0, len(dests))
	var refused []string
	for _, dest := range dests {
		plan, err := bundle.Plan(dir, dest, force)
		if err != nil {
			return fmt.Errorf("skill install %s: %w", dest.Rel(), err)
		}
		plans = append(plans, plan)
		refused = append(refused, plan.Refused()...)
	}
	if len(refused) > 0 {
		return fmt.Errorf("skill install: these files changed after they were installed:\n  %s\nrun `unmute skill install --force` to overwrite them", strings.Join(refused, "\n  "))
	}

	out := cmd.OutOrStdout()
	for _, plan := range plans {
		if err := bundle.Apply(plan); err != nil {
			return fmt.Errorf("skill install: %w", err)
		}
		reportPlan(cmd, plan)
	}

	fmt.Fprintf(out, "\nInstalled the Unmute skill for %s.\n", strings.Join(installedFor(agents), ", "))
	fmt.Fprintln(out, "Commit these files so your team's assistants get them too.")
	fmt.Fprintln(out, "Next: ask your assistant to build a voice agent, in a sentence.")
	return nil
}

// reportPlan prints one line per file with what happened to it. A file that was
// left alone is reported as left alone, because a silent no-op and a silent
// overwrite look identical from the outside.
func reportPlan(cmd *cobra.Command, plan skill.DestinationPlan) {
	out := cmd.OutOrStdout()
	header := "  " + plan.Destination.Rel() + "/"
	if plan.FromVersion != "" {
		header += fmt.Sprintf("  (upgraded from %s)", plan.FromVersion)
	}
	fmt.Fprintln(out, header)
	for _, file := range plan.Files {
		fmt.Fprintf(out, "    %-32s %s\n", file.Path, style.Dim(string(file.Action)))
	}
}

// installedFor names the assistants the user asked for, so the summary line
// answers "did this cover my editor" rather than naming directories again.
func installedFor(agents []string) []string {
	if len(agents) == 0 {
		return skill.Assistants()
	}
	var out []string
	for _, agent := range agents {
		name := strings.ToLower(strings.TrimSpace(agent))
		if name == "all" {
			return skill.Assistants()
		}
		out = append(out, name)
	}
	return out
}
