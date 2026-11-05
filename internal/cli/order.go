package cli

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/felipe9962/repsol-cli/internal/app"
	"github.com/felipe9962/repsol-cli/internal/apperr"
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
	abandon  bool
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
--wait) or the owner gives it up with --abandon-payment. --force does not
replace that: it only skips the check of the order history, and the remembered
payment is looked up all the same. A payment that was paid and whose delivery is
still to come refuses the order with pending_order, and --abandon-payment does
not forget it either; repsol order payment ID --abandon does. A remembered
payment nobody paid within 24 hours is forgotten, with a notice, by the next
order.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := state.getApp()
			if err != nil {
				return err
			}
			in := app.PlaceOrderInput{
				Product:        opts.product,
				Quantity:       opts.quantity,
				Date:           opts.date,
				Pay:            opts.pay,
				Wait:           opts.wait,
				DryRun:         opts.dryRun,
				Force:          opts.force,
				AbandonPayment: opts.abandon,
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
		"place the order even when another delivery is pending in the history (a remembered Waylet payment is still checked)")
	flags.BoolVar(&opts.abandon, "abandon-payment", false,
		"forget the unpaid Waylet payment this profile remembers, or its damaged state file, and place the order")
	return cmd
}

// newOrderPaymentCommand builds order payment.
func newOrderPaymentCommand(state *rootState) *cobra.Command {
	var (
		wait    time.Duration
		abandon bool
	)
	cmd := &cobra.Command{
		Use:   "payment [PAYMENT_ID]",
		Short: "Check whether a Waylet payment was confirmed, or abandon it",
		Long: `Look up the order a Waylet payment produced. Without --wait it asks once and
reports awaiting_payment while the payment is not confirmed. With --wait it
polls until the payment is confirmed or the wait ends; a duration needs the =
form, as in --wait=30m, and --wait alone waits 15 minutes.

With --abandon it does not wait: it forgets the Waylet payment this profile
remembers, so it stops holding other orders back. PAYMENT_ID is optional then,
and must be the remembered payment when given. Use it only when the owner will
not pay that link; it also removes a state file that can no longer be read. It
looks the payment up first and says in a note when it was paid or could not be
checked: a paid payment is forgotten all the same, because the owner may have
cancelled that delivery.`,
		Args: func(cmd *cobra.Command, args []string) error {
			if abandon {
				return cobra.MaximumNArgs(1)(cmd, args)
			}
			return cobra.ExactArgs(1)(cmd, args)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := state.getApp()
			if err != nil {
				return err
			}
			id := ""
			if len(args) > 0 {
				id = args[0]
			}
			if abandon {
				if wait != 0 {
					return apperr.New(apperr.Usage, "abandon_with_wait",
						"--abandon and --wait cannot be used together").
						WithHint("drop --wait: abandoning a payment does not wait for it")
				}
				view, err := a.AbandonPayment(cmd.Context(), id)
				if err != nil {
					return err
				}
				return state.render(view)
			}
			view, err := a.PaymentStatus(cmd.Context(), id, wait)
			if err != nil {
				return err
			}
			return state.render(view)
		},
	}
	cmd.Flags().DurationVar(&wait, "wait", 0,
		"wait for the payment to be confirmed; --wait=DURATION, default 15m")
	cmd.Flags().Lookup("wait").NoOptDefVal = defaultWait.String()
	cmd.Flags().BoolVar(&abandon, "abandon", false,
		"forget the Waylet payment this profile remembers (the owner will not pay it), or its damaged state file")
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
