package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/slng-ai/unmute/internal/ir"
	"github.com/slng-ai/unmute/internal/scaffold"
	"github.com/slng-ai/unmute/internal/style"
	"github.com/slng-ai/unmute/internal/tui"
	"github.com/spf13/cobra"
)

func newInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init [name]",
		Short: "Scaffold a new v1 agent package.",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				if cmd.InOrStdin() == os.Stdin && (!isTTY(os.Stdin) || !isTTY(cmd.OutOrStdout())) {
					return fmt.Errorf("agent name required")
				}
				return runConsole(cmd, true)
			}
			dir := args[0]
			return writeScaffold(cmd, dir, scaffold.Data{Name: filepath.Base(dir), Tools: scaffold.DefaultTools()})
		},
	}
}

func runConsole(cmd *cobra.Command, createOnly bool) error {
	in := cmd.InOrStdin()
	run := func(in io.Reader, out io.Writer, accessible bool) (tui.Result, error) {
		return tui.RunConsole(in, out, accessible, consoleAction)
	}
	if createOnly {
		run = tui.RunCreate
	}
	result, err := run(in, cmd.OutOrStdout(), in != os.Stdin)
	if err != nil {
		return fmt.Errorf("console: %w", err)
	}
	if !result.Confirmed {
		return nil
	}
	dir := result.Agent.Path
	if err := writeScaffold(cmd, dir, result.Agent.Data); err != nil {
		return err
	}
	if result.Compile {
		if err := runCompile(cmd, dir, nil); err != nil {
			return fmt.Errorf("agent scaffold kept; fix it and run `unmute compile %s`: %w", dir, err)
		}
	}
	return nil
}

func consoleAction(action, path string, out io.Writer) error {
	cmd := &cobra.Command{}
	cmd.SetOut(out)
	cmd.SetErr(out)
	switch action {
	case "validate":
		return runValidate(cmd, path, nil)
	case "compile":
		return runCompile(cmd, path, nil)
	default:
		return fmt.Errorf("unknown console action %q", action)
	}
}

func writeScaffold(cmd *cobra.Command, dir string, data scaffold.Data) error {
	out := cmd.OutOrStdout()
	printHeader(out, "init "+displayDir(dir))
	// The scaffold seeds `name:` from the folder when it can, and writes none
	// when it cannot rather than guessing at a deployed identity. Preflight would
	// catch the result a moment later, but its message is about a field the
	// author never wrote; this one is about the argument they did type.
	if data.AgentName == "" && scaffold.AgentNameFrom(data.Name) == "" {
		return fmt.Errorf("init %s: %q cannot be an agent name, and it is all unmute has to go on: "+
			"a name is %s. Run `unmute init <name>` with one, or scaffold here and add a `name:` to agent.yaml yourself",
			dir, data.Name, ir.PackageNameShape)
	}
	if _, err := scaffold.Preflight(data); err != nil {
		return fmt.Errorf("init %s: preflight: %w", dir, err)
	}
	created, err := scaffold.Write(dir, data)
	if err != nil {
		return fmt.Errorf("init %s: %w", dir, err)
	}
	u := style.For(out)
	for _, path := range created {
		fmt.Fprintln(out, u.Dim("created"), dimPath(u, path))
	}
	return nil
}
