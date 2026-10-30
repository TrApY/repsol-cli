package cli

import (
	"os"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"

	"github.com/felipe9962/repsol-cli/internal/apperr"
	"github.com/felipe9962/repsol-cli/internal/mcpserver"
)

// newMCPCommand builds the mcp command and its install subcommand.
func newMCPCommand(state *rootState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "Serve the MCP tools on stdio, or install the server entry in an agent harness",
		Long: `Serve the MCP tools on stdio. The tools are catalog_list, address_show,
schedule_list, order_create, order_list and order_last, and their results are the
same JSON the CLI prints.

Use "repsol mcp install TARGET" to write the server entry into the configuration
of Claude Code, Codex or Cursor.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := state.getApp()
			if err != nil {
				return err
			}
			server := mcpserver.NewServer(a, Version)
			return server.Run(cmd.Context(), &mcp.StdioTransport{})
		},
	}
	cmd.AddCommand(newMCPInstallCommand(state))
	return cmd
}

// newMCPInstallCommand builds mcp install. It writes the entry for the running
// executable, so the harness starts the same binary the owner installed.
func newMCPInstallCommand(state *rootState) *cobra.Command {
	return &cobra.Command{
		Use:   "install TARGET",
		Short: "Write the repsol server entry into an agent harness config",
		Long: `Write the repsol MCP server entry into the configuration of TARGET:
claude-code, codex or cursor. Other entries are preserved, and running the
command twice leaves an up-to-date file untouched.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			command, err := os.Executable()
			if err != nil {
				return apperr.New(apperr.Generic, "executable_unresolved",
					"the repsol executable path could not be resolved").WithCause(err)
			}
			home, err := os.UserHomeDir()
			if err != nil {
				return apperr.New(apperr.Generic, "home_unresolved",
					"the user home directory could not be resolved").WithCause(err)
			}
			result, err := mcpserver.Install(args[0], command, []string{"mcp"}, home, state.sys.Getenv)
			if err != nil {
				return err
			}
			return state.render(result)
		},
	}
}
