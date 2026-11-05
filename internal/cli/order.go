package cli

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/felipe9962/repsol-cli/internal/app"
)

// defaultWait is how long --wait waits when it is given without a value.
const defaultWait = 15 * time.Minute

// newOrderCommand builds the order command and its subcommands.
func newOrderCommand(state *rootState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "order",
		Short: "Place and inspect orders, paid on delivery or with Waylet",
	}
	cmd.AddCommand(
		newOrderCreateCommand(state),
		newOrderPaymentCommand(state),
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
	wait     time.Duration
	dryRun   bool
	force    bool
}

// newOrderCreateCommand builds order create.
func newOrderCreateCommand(state *rootState) *cobra.Command {
	opts := &orderCreateOptions{}
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Place an order, paid on delivery or with Waylet",
		Long: `Place a real order, paid on delivery, with no confirmation prompt. Use
--dry-run to run every check and preview the result without placing it.

With --pay waylet the order is paid on a Waylet page instead: the command
prints the payment link and does not place anything until the owner pays it.
Add --wait to stay until Repsol confirms the payment and print the order. A
duration needs the = form, as in --wait=30m; --wait alone waits 15 minutes.

While a Waylet payment is unpaid, no other order is placed: the command exits 8
with payment_pending until the payment is confirmed (repsol order payment ID
--wait) or --force is given.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := state.getApp()
			if err != nil {
				return err
			}
			in := app.PlaceOrderInput{
				Product:  opts.product,
				Quantity: opts.quantity,
				Date:     opts.date,
				Pay:      opts.pay,
				Wait:     opts.wait,
				DryRun:   opts.dryRun,
				Force:    opts.force,
			}
			if opts.wait > 0 {
				// The result only comes once the payment is confirmed, so the
				// owner needs the link while the command is still waiting.
				in.OnPaymentLink = func(l app.PaymentLink) {
					fmt.Fprintf(state.sys.Err, "open this page to pay with Waylet (waiting up to %s):\n%s\n",
						opts.wait, l.URL)
				}
			}
			result, err := a.PlaceOrder(cmd.Context(), in)
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
		"payment method: delivery (pay the driver) or waylet (pay on a Waylet page)")
	flags.DurationVar(&opts.wait, "wait", 0,
		"with --pay waylet, wait for the payment to be confirmed; --wait=DURATION, default 15m")
	flags.Lookup("wait").NoOptDefVal = defaultWait.String()
	flags.BoolVar(&opts.dryRun, "dry-run", false,
		"run every check and print the order without placing it")
	flags.BoolVar(&opts.force, "force", false,
		"place the order even when another delivery is pending or a Waylet payment is unpaid")
	return cmd
}

// newOrderPaymentCommand builds order payment.
func newOrderPaymentCommand(state *rootState) *cobra.Command {
	var wait time.Duration
	cmd := &cobra.Command{
		Use:   "payment PAYMENT_ID",
		Short: "Check whether a Waylet payment was confirmed",
		Long: `Look up the order a Waylet payment produced. Without --wait it asks once and
reports awaiting_payment while the payment is not confirmed. With --wait it
polls until the payment is confirmed or the wait ends; a duration needs the =
form, as in --wait=30m, and --wait alone waits 15 minutes.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := state.getApp()
			if err != nil {
				return err
			}
			view, err := a.PaymentStatus(cmd.Context(), args[0], wait)
			if err != nil {
				return err
			}
			return state.render(view)
		},
	}
	cmd.Flags().DurationVar(&wait, "wait", 0,
		"wait for the payment to be confirmed; --wait=DURATION, default 15m")
	cmd.Flags().Lookup("wait").NoOptDefVal = defaultWait.String()
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
