package cli

import (
	"github.com/spf13/cobra"
)

// newScheduleCommand builds the schedule command and its subcommands.
func newScheduleCommand(state *rootState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "schedule",
		Short: "Show the delivery dates of the delivery agency",
	}
	cmd.AddCommand(newScheduleListCommand(state))
	return cmd
}

// newScheduleListCommand builds schedule list.
func newScheduleListCommand(state *rootState) *cobra.Command {
	var product string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List the delivery dates the agency offers for a product",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := state.getApp()
			if err != nil {
				return err
			}
			view, err := a.Schedule(cmd.Context(), product)
			if err != nil {
				return err
			}
			return state.render(view)
		},
	}
	cmd.Flags().StringVar(&product, "product", "",
		"product alias or numeric material id (default: butano)")
	return cmd
}
