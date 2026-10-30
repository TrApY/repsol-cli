package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/felipe9962/repsol-cli/internal/apperr"
	"github.com/felipe9962/repsol-cli/internal/cli/skills"
)

// newSkillsCommand builds the skills command and its subcommands.
func newSkillsCommand(state *rootState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "skills",
		Short: "Read the embedded agent skill documents",
	}
	cmd.AddCommand(
		newSkillsListCommand(state),
		newSkillsGetCommand(state),
	)
	return cmd
}

// newSkillsListCommand builds skills list.
func newSkillsListCommand(state *rootState) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List the embedded skills",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			list, err := skills.List()
			if err != nil {
				return err
			}
			return state.render(list)
		},
	}
}

// newSkillsGetCommand builds skills get. The document is markdown, so it is
// written as is whatever --format says.
func newSkillsGetCommand(state *rootState) *cobra.Command {
	return &cobra.Command{
		Use:   "get NAME",
		Short: "Print one skill document as raw markdown",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			list, err := skills.List()
			if err != nil {
				return err
			}
			for _, skill := range list {
				if skill.Name != name {
					continue
				}
				data, err := skills.FS.ReadFile(name + "/SKILL.md")
				if err != nil {
					return apperr.New(apperr.Generic, "skill_unreadable",
						fmt.Sprintf("the %q skill document could not be read", name)).WithCause(err)
				}
				_, err = state.sys.Out.Write(data)
				return err
			}
			return apperr.New(apperr.NotFound, "skill_not_found",
				fmt.Sprintf("no skill named %q", name)).
				WithHint(`run "repsol skills list" to see the available skills`)
		},
	}
}
