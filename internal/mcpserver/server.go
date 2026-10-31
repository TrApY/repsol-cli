// Package mcpserver exposes the app use cases as MCP tools and installs the
// server entry into the configuration of an MCP harness.
//
// Every tool returns the same JSON the CLI prints, and a failure is a tool
// error whose text is the error envelope. The envelope carries no secret: the
// app errors are the ones the transport already redacts.
package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/felipe9962/repsol-cli/internal/app"
	"github.com/felipe9962/repsol-cli/internal/output"
)

// serverName is the implementation name MCP clients see.
const serverName = "repsol"

// defaultOrderLimit is how many orders order_list returns when the caller does
// not choose a limit. It matches the CLI default.
const defaultOrderLimit = 10

// emptyIn is the input of the tools that take no argument.
type emptyIn struct{}

// scheduleListIn is the input of schedule_list.
type scheduleListIn struct {
	// Product is a product alias or a numeric material id.
	Product string `json:"product,omitempty" jsonschema:"product alias (butano, propano, ...) or numeric material id; defaults to butano"`
}

// orderCreateIn is the input of order_create. There is deliberately no force
// field: an agent must ask the owner before placing a second delivery while one
// is pending.
type orderCreateIn struct {
	// Product is a product alias or a numeric material id.
	Product string `json:"product,omitempty" jsonschema:"product alias (butano, propano, ...) or numeric material id; defaults to butano"`
	// Quantity is the number of bottles. Zero means one.
	Quantity int `json:"quantity,omitempty" jsonschema:"number of bottles; defaults to 1"`
	// Date is the requested delivery date as YYYY-MM-DD, or empty for the
	// earliest offered date.
	Date string `json:"date,omitempty" jsonschema:"delivery date as YYYY-MM-DD; defaults to the earliest offered date"`
	// DryRun plans the order without placing it.
	DryRun bool `json:"dry_run,omitempty" jsonschema:"run every check and return the planned order without placing it"`
}

// orderListIn is the input of order_list.
type orderListIn struct {
	// Limit is how many orders to return. Zero means the default.
	Limit int `json:"limit,omitempty" jsonschema:"how many orders to return, between 1 and 100; defaults to 10"`
}

// ToolNames returns the names of the registered tools, in registration order.
func ToolNames() []string {
	return []string{
		"catalog_list",
		"address_show",
		"schedule_list",
		"order_create",
		"order_list",
		"order_last",
	}
}

// NewServer builds the MCP server over the app use cases. Every tool answers
// with the JSON the corresponding CLI command prints.
func NewServer(a *app.App, version string) *mcp.Server {
	tools := &toolSet{app: a}
	server := mcp.NewServer(&mcp.Implementation{
		Name:    serverName,
		Title:   "Repsol",
		Version: version,
	}, nil)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "catalog_list",
		Description: "List the bottled-gas products the delivery agency sells at the stored address, with prices and per-order limits.",
	}, tools.catalogList)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "address_show",
		Description: "Show the delivery address stored on the profile and the agency that serves it.",
	}, tools.addressShow)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "schedule_list",
		Description: "List the delivery dates the agency offers for a product at the stored address.",
	}, tools.scheduleList)
	mcp.AddTool(server, &mcp.Tool{
		Name: "order_create",
		Description: "Place a real pay-on-delivery order, or preview it with dry_run. " +
			"The order is placed at once, with no confirmation. " +
			"It fails when another delivery is still pending; " +
			"report that to the owner instead of trying again.",
	}, tools.orderCreate)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "order_list",
		Description: "List the newest orders of the profile, newest first, with their delivery dates and pending flag.",
	}, tools.orderList)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "order_last",
		Description: "Show the newest order of the profile.",
	}, tools.orderLast)

	return server
}

// toolSet holds the app the tool handlers call.
type toolSet struct {
	app *app.App
}

// catalogList handles catalog_list.
func (t *toolSet) catalogList(ctx context.Context, _ *mcp.CallToolRequest, _ emptyIn) (*mcp.CallToolResult, any, error) {
	items, err := t.app.Catalog(ctx)
	return toolResult(items, err)
}

// addressShow handles address_show.
func (t *toolSet) addressShow(_ context.Context, _ *mcp.CallToolRequest, _ emptyIn) (*mcp.CallToolResult, any, error) {
	view, err := t.app.ShowAddress()
	return toolResult(view, err)
}

// scheduleList handles schedule_list.
func (t *toolSet) scheduleList(ctx context.Context, _ *mcp.CallToolRequest, in scheduleListIn) (*mcp.CallToolResult, any, error) {
	view, err := t.app.Schedule(ctx, in.Product)
	return toolResult(view, err)
}

// orderCreate handles order_create. There is no force: a pending delivery
// refuses the call, and the owner decides whether to place another one.
func (t *toolSet) orderCreate(ctx context.Context, _ *mcp.CallToolRequest, in orderCreateIn) (*mcp.CallToolResult, any, error) {
	quantity := in.Quantity
	if quantity == 0 {
		quantity = 1
	}
	result, err := t.app.PlaceOrder(ctx, app.PlaceOrderInput{
		Product:  in.Product,
		Quantity: quantity,
		Date:     in.Date,
		DryRun:   in.DryRun,
	})
	return toolResult(result, err)
}

// orderList handles order_list.
func (t *toolSet) orderList(ctx context.Context, _ *mcp.CallToolRequest, in orderListIn) (*mcp.CallToolResult, any, error) {
	limit := in.Limit
	if limit == 0 {
		limit = defaultOrderLimit
	}
	orders, err := t.app.ListOrders(ctx, limit)
	return toolResult(orders, err)
}

// orderLast handles order_last.
func (t *toolSet) orderLast(ctx context.Context, _ *mcp.CallToolRequest, _ emptyIn) (*mcp.CallToolResult, any, error) {
	order, err := t.app.LastOrder(ctx)
	return toolResult(order, err)
}

// toolResult renders v as the CLI's JSON in a successful tool result, or turns
// err into the error envelope of a tool error.
func toolResult(v any, err error) (*mcp.CallToolResult, any, error) {
	if err != nil {
		return toolError(err), nil, nil
	}
	var buf bytes.Buffer
	if renderErr := output.Render(&buf, output.FormatJSON, v); renderErr != nil {
		return toolError(renderErr), nil, nil
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: buf.String()}},
	}, nil, nil
}

// toolError returns a tool error whose text is the error envelope. The envelope
// never carries a secret.
func toolError(err error) *mcp.CallToolResult {
	data, marshalErr := json.Marshal(output.EnvelopeFor(err))
	if marshalErr != nil {
		data = []byte(`{"code":"output_failed","message":"the error could not be rendered"}`)
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(data)}},
		IsError: true,
	}
}
