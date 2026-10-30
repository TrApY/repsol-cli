package cli

import (
	"github.com/spf13/cobra"

	"github.com/felipe9962/repsol-cli/internal/app"
)

// newAddressCommand builds the address command and its subcommands.
func newAddressCommand(state *rootState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "address",
		Short: "Resolve and show the delivery address",
	}
	cmd.AddCommand(
		newAddressShowCommand(state),
		newAddressSetCommand(state),
	)
	return cmd
}

// newAddressShowCommand builds address show.
func newAddressShowCommand(state *rootState) *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: "Show the stored delivery address",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := state.getApp()
			if err != nil {
				return err
			}
			view, err := a.ShowAddress()
			if err != nil {
				return err
			}
			return state.render(view)
		},
	}
}

// addressSetOptions are the flags of address set.
type addressSetOptions struct {
	floor   string
	door    string
	details string
	pick    int
}

// newAddressSetCommand builds address set.
func newAddressSetCommand(state *rootState) *cobra.Command {
	opts := &addressSetOptions{}
	cmd := &cobra.Command{
		Use:   "set TEXT",
		Short: "Resolve a free-text address and store it on the profile",
		Long: `Resolve a free-text address the way the Repsol web does and store it on the
profile. When several suggestions match, the command lists them and needs
--pick N to choose one.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := state.getApp()
			if err != nil {
				return err
			}
			view, err := a.SetAddress(cmd.Context(), app.SetAddressInput{
				Text:    args[0],
				Floor:   opts.floor,
				Door:    opts.door,
				Details: opts.details,
				Pick:    opts.pick,
			})
			if err != nil {
				return err
			}
			return state.render(view)
		},
	}
	flags := cmd.Flags()
	flags.StringVar(&opts.floor, "floor", "", "floor of the delivery address")
	flags.StringVar(&opts.door, "door", "", "door of the delivery address")
	flags.StringVar(&opts.details, "details", "", "free-form delivery notes")
	flags.IntVar(&opts.pick, "pick", 0,
		"choose one of several matches, numbered from 1")
	return cmd
}
