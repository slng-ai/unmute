package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/slng/unmute/internal/scaffold"
	"github.com/slng/unmute/internal/tui"
	"github.com/spf13/cobra"
)

const slngWordmark = "\x1b[38;2;0;0;0;48;2;245;201;110m" +
	"  ____  _     _   _  ____       //  // \n" +
	" / ___|| |   | \\ | |/ ___|     //  //  \n" +
	" \\___ \\| |   |  \\| | |  _     //  //   \n" +
	"  ___) | |___| |\\  | |_| |   //  //    \n" +
	" |____/|_____|_| \\_|\\____|  //  //     \x1b[0m\n"

func newInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init [name]",
		Short: "Scaffold a new v1 agent package.",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				if cmd.InOrStdin() == os.Stdin && (!isCharDevice(os.Stdin) || !isCharDevice(cmd.OutOrStdout())) {
					return fmt.Errorf("agent name required")
				}
				return runInitWizard(cmd)
			}
			dir := args[0]
			return writeScaffold(cmd, dir, scaffold.Data{Name: filepath.Base(dir)})
		},
	}
}

func isCharDevice(value any) bool {
	file, ok := value.(*os.File)
	if !ok {
		return false
	}
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func runInitWizard(cmd *cobra.Command) error {
	in := cmd.InOrStdin()
	writeHeader(cmd.ErrOrStderr(), isCharDevice(cmd.ErrOrStderr()))
	result, err := tui.Run(in, cmd.OutOrStdout(), in != os.Stdin)
	if err != nil {
		return fmt.Errorf("init menu: %w", err)
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

func writeHeader(out io.Writer, tty bool) {
	if !tty || os.Getenv("NO_COLOR") != "" {
		return
	}
	fmt.Fprint(out, slngWordmark)
}

func writeScaffold(cmd *cobra.Command, dir string, data scaffold.Data) error {
	if _, err := scaffold.Preflight(data); err != nil {
		return fmt.Errorf("init %s: preflight: %w", dir, err)
	}
	created, err := scaffold.Write(dir, data)
	if err != nil {
		return fmt.Errorf("init %s: %w", dir, err)
	}
	for _, path := range created {
		fmt.Fprintln(cmd.OutOrStdout(), "created", path)
	}
	return nil
}
