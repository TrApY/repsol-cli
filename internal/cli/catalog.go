package cli

import (
	"github.com/spf13/cobra"
)

// newCatalogCommand builds the catalog command and its subcommands.
func newCatalogCommand(state *rootState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "catalog",
		Short: "Browse the products of the delivery agency",
	}
	cmd.AddCommand(newCatalogListCommand(state))
	return cmd
}

// newCatalogListCommand builds catalog list.
func newCatalogListCommand(state *rootState) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List the products the agency sells for the stored address",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := state.getApp()
			if err != nil {
				return err
			}
			items, err := a.Catalog(cmd.Context())
			if err != nil {
				return err
			}
			return state.render(items)
		},
	}
}
