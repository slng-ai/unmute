package cli

import (
	"fmt"
	"io"
	"os/exec"

	"github.com/slng-ai/unmute/internal/target"
	"github.com/spf13/cobra"
)

// You cannot name what you cannot see.
//
// A package references a curated tool and an MCP server by exact name, and both
// are created outside unmute, in the SLNG dashboard. Without a way to look, an
// author writes a name from memory, compiles clean, and finds out at the push.
// This is the read-only half of the preflight, pointed at an author who is still
// writing rather than at one who is deploying.
//
// It reads no package and writes nothing. Not on the deploy path.

func newResourcesCmd() *cobra.Command {
	var profile string
	cmd := &cobra.Command{
		Use:   "resources",
		Short: "List the tools, MCP servers and phone numbers your SLNG organisation offers.",
		Long: "List the tools, MCP servers and phone numbers your SLNG organisation offers.\n\n" +
			"Names are shown in the exact spelling a package must use: SLNG matches them " +
			"exactly and is case-sensitive everywhere. Nothing is written, and no package " +
			"is read, so this is safe to run from anywhere.\n\n" +
			"Tools and MCP servers are created in the SLNG dashboard, not from here. " +
			"`unmute deploy` checks a package against this same list before it pushes.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			bin, err := exec.LookPath(deployPushBinary)
			if err != nil {
				return fmt.Errorf("resources: %s", missingPushToolGuidance())
			}
			// The same environment `deploy` resolves, for the reason that command
			// prints the organisation on every run: a key in a package's .env and a
			// stored profile can belong to different organisations. Reading with
			// one and deploying with the other would show an author a list of tools
			// that has nothing to do with where their package is going. Measured
			// 2026-08-31: on this machine the two differ.
			//
			// This still reads no *package*. It reads the environment files `dev`
			// and `deploy` already read, and parses no YAML, so it works from
			// anywhere including outside a package.
			errOut := cmd.ErrOrStderr()
			env := packageEnv(".", errOut)
			if key, _ := deployCredential(env); key != "" {
				env = append(env, target.SlngPushCredentialEnv+"="+key)
			}
			return runResources(cmd.OutOrStdout(), errOut, newVoiceaiRunner(bin, env, profile))
		},
	}
	cmd.Flags().StringVar(&profile, "profile", "", "voiceai credential profile to read with")
	return cmd
}

func runResources(out, errOut io.Writer, runner *voiceaiRunner) error {
	// No package, so no requirements, so no MCP server to interrogate by name.
	// The servers are listed and each one's tools are read below instead.
	printHeader(out, "resources")
	resources, err := readResources(runner, nil)
	if err != nil {
		return fmt.Errorf("resources: %w", err)
	}
	fmt.Fprintf(out, "organisation %s\n", resources.Account)

	fmt.Fprintf(out, "\ntools (%d)\n", len(resources.Tools))
	if len(resources.Tools) == 0 {
		fmt.Fprintln(out, "  none")
	}
	for _, tool := range resources.Tools {
		// The type is what tells an author whether they reference this tool or
		// write one: a curated capability is reached with `builtin:`, and a code or
		// api_request tool of the same name would be created by a push.
		fmt.Fprintf(out, "  %-24s %s\n", tool.Name, tool.ToolType)
	}

	fmt.Fprintf(out, "\nmcp servers (%d)\n", len(resources.MCPServer))
	if len(resources.MCPServer) == 0 {
		fmt.Fprintln(out, "  none attached. An MCP server is attached in the SLNG dashboard.")
	}
	for _, server := range resources.MCPServer {
		fmt.Fprintf(out, "  %-24s %s, last probe %s\n", server.Name, server.Transport, probeStatus(server))
		var tools []slngMCPTool
		if err := runner.read(target.SlngMCPTools.With(server.Name), &tools); err != nil {
			warnf(errOut, "%v\n", err)
			continue
		}
		for _, tool := range tools {
			fmt.Fprintf(out, "    %s\n", tool.Name)
		}
	}
	// Said once, at the bottom of the section it qualifies. A stored probe is not
	// a live call, so a server can be listed here and be unreachable.
	if len(resources.MCPServer) > 0 {
		fmt.Fprintln(out, "  status and tool lists come from each server's last stored probe, not a live call.")
	}

	trunks, notes, err := readTrunks(runner)
	for _, note := range notes {
		notef(errOut, "%s\n", note)
	}
	if err != nil {
		warnf(errOut, "%v\n", err)
		return nil
	}
	fmt.Fprintf(out, "\nphone numbers (%d trunks)\n", len(trunks))
	if len(trunks) == 0 {
		fmt.Fprintln(out, "  none. An organisation with no agents cannot be enumerated at all.")
	}
	for _, trunk := range trunks {
		fmt.Fprintf(out, "  %-8s %-24s %-20s %s%s\n", trunk.Direction, trunk.Name,
			numbersOf(trunk), inUseBy(trunk), unusableSuffix(trunk))
	}
	return nil
}

func probeStatus(server slngMCPServer) string {
	if server.CapabilityStatus == "" {
		return "unknown"
	}
	return server.CapabilityStatus + parenthesised(server.CapabilityError)
}

func inUseBy(trunk slngTrunk) string {
	if trunk.InUseBy == "" {
		return "free"
	}
	return "in use by " + trunk.InUseBy
}
