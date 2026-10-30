package cli

import (
	"github.com/spf13/cobra"

	"github.com/felipe9962/repsol-cli/internal/app"
)

// newOrderCommand builds the order command and its subcommands.
func newOrderCommand(state *rootState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "order",
		Short: "Place and inspect pay-on-delivery orders",
	}
	cmd.AddCommand(
		newOrderCreateCommand(state),
		newOrderListCommand(state),
		newOrderLastCommand(state),
		newOrderShowCommand(state),
	)
	return cmd
}

// orderCreateOptions are the flags of order create.
type orderCreateOptions struct {
	product  string
	quantity int
	date     string
	pay      string
	dryRun   bool
	force    bool
}

// newOrderCreateCommand builds order create.
func newOrderCreateCommand(state *rootState) *cobra.Command {
	opts := &orderCreateOptions{}
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Place a pay-on-delivery order",
		Long: `Place a real order, paid on delivery, with no confirmation prompt. Use
--dry-run to run every check and preview the result without placing it.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := state.getApp()
			if err != nil {
				return err
			}
			result, err := a.PlaceOrder(cmd.Context(), app.PlaceOrderInput{
				Product:  opts.product,
				Quantity: opts.quantity,
				Date:     opts.date,
				Pay:      opts.pay,
				DryRun:   opts.dryRun,
				Force:    opts.force,
			})
			if err != nil {
				return err
			}
			return state.render(result)
		},
	}
	flags := cmd.Flags()
	flags.StringVar(&opts.product, "product", "",
		"product alias or numeric material id (default: butano)")
	flags.IntVar(&opts.quantity, "qty", 1, "number of bottles")
	flags.StringVar(&opts.date, "date", "",
		"delivery date as YYYY-MM-DD (default: the earliest offered)")
	flags.StringVar(&opts.pay, "pay", "delivery",
		"payment method; orders are paid on delivery")
	flags.BoolVar(&opts.dryRun, "dry-run", false,
		"run every check and print the order without placing it")
	flags.BoolVar(&opts.force, "force", false,
		"place the order even when another delivery is pending")
	return cmd
}

// newOrderListCommand builds order list.
func newOrderListCommand(state *rootState) *cobra.Command {
	var limit int
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List the newest orders",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := state.getApp()
			if err != nil {
				return err
			}
			orders, err := a.ListOrders(cmd.Context(), limit)
			if err != nil {
				return err
			}
			return state.render(orders)
		},
	}
	cmd.Flags().IntVar(&limit, "limit", 10, "how many orders to list, at most 100")
	return cmd
}

// newOrderLastCommand builds order last.
func newOrderLastCommand(state *rootState) *cobra.Command {
	return &cobra.Command{
		Use:   "last",
		Short: "Show the newest order",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := state.getApp()
			if err != nil {
				return err
			}
			order, err := a.LastOrder(cmd.Context())
			if err != nil {
				return err
			}
			return state.render(order)
		},
	}
}

// newOrderShowCommand builds order show.
func newOrderShowCommand(state *rootState) *cobra.Command {
	return &cobra.Command{
		Use:   "show REFERENCE",
		Short: "Show one order by its eight-digit reference",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := state.getApp()
			if err != nil {
				return err
			}
			order, err := a.ShowOrder(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return state.render(order)
		},
	}
}
