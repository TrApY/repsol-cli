package mcpserver

import (
	"context"
	"encoding/json"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/felipe9962/repsol-cli/internal/app"
	"github.com/felipe9962/repsol-cli/internal/config"
	"github.com/felipe9962/repsol-cli/internal/fakerepsol"
	"github.com/felipe9962/repsol-cli/internal/gigya"
	"github.com/felipe9962/repsol-cli/internal/httpx"
	"github.com/felipe9962/repsol-cli/internal/repsolapi"
)

// testProfile is the profile the MCP test app works on.
const testProfile = "default"

// testNow is the clock the MCP test app starts at. It matches the fake's today.
var testNow = time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)

// newTestApp wires an App to a fresh fakerepsol server, with the synthetic
// session and address stored, mirroring internal/app's newOrderApp.
func newTestApp(t *testing.T) *app.App {
	t.Helper()

	fake := fakerepsol.New()
	t.Cleanup(fake.Close)
	fake.SetToday(time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC))

	transport := httpx.New("repsol-cli/test", nil)
	transport.Backoff = func(int) time.Duration { return 0 }

	dir := t.TempDir()
	store := config.Store{Paths: config.Paths{
		Config:      filepath.Join(dir, "config.toml"),
		Credentials: filepath.Join(dir, "credentials.toml"),
		Lock:        filepath.Join(dir, "credentials.toml.lock"),
	}}
	if err := store.UpdateCredentials(testProfile, func(cred *config.Credentials) error {
		*cred = config.Credentials{
			LoginToken: fakerepsol.ValidToken,
			GMID:       fakerepsol.ValidGMID,
		}
		return nil
	}); err != nil {
		t.Fatalf("storing credentials: %v", err)
	}

	now := func() time.Time { return testNow }
	a := app.New(app.Deps{
		API:     &repsolapi.Client{BaseURL: fake.APIURL(), HTTP: transport},
		Places:  &repsolapi.PlacesClient{BaseURL: fake.PlacesURL(), HTTP: transport},
		Gigya:   &gigya.Client{BaseURL: fake.GigyaURL(), HTTP: transport, Now: now},
		Store:   store,
		Profile: testProfile,
		Now:     now,
	})
	if _, err := a.SetAddress(context.Background(), app.SetAddressInput{Text: "Calle Falsa 1, Pueblo"}); err != nil {
		t.Fatalf("SetAddress: %v", err)
	}
	return a
}

// newSession connects an in-memory MCP client to a server built on a.
func newSession(t *testing.T, a *app.App) *mcp.ClientSession {
	t.Helper()
	ctx := context.Background()

	server := NewServer(a, "test")
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server Connect: %v", err)
	}
	t.Cleanup(func() { _ = serverSession.Close() })

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client Connect: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}

// toolText returns the single text content of a tool result.
func toolText(t *testing.T, result *mcp.CallToolResult) string {
	t.Helper()
	if len(result.Content) != 1 {
		t.Fatalf("result content length = %d, want 1", len(result.Content))
	}
	text, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("result content type = %T, want *mcp.TextContent", result.Content[0])
	}
	return text.Text
}

func TestToolsListed(t *testing.T) {
	ctx := context.Background()
	session := newSession(t, newTestApp(t))

	result, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}

	names := make([]string, 0, len(result.Tools))
	byName := make(map[string]*mcp.Tool, len(result.Tools))
	for _, tool := range result.Tools {
		names = append(names, tool.Name)
		byName[tool.Name] = tool
	}
	// The SDK lists tools in a stable order of its own, so compare the sets.
	slices.Sort(names)
	want := ToolNames()
	slices.Sort(want)
	if !slices.Equal(names, want) {
		t.Errorf("tool names = %v, want %v", names, want)
	}

	create, ok := byName["order_create"]
	if !ok {
		t.Fatal("order_create tool is not registered")
	}
	schema, ok := create.InputSchema.(map[string]any)
	if !ok {
		t.Fatalf("order_create input schema type = %T, want map[string]any", create.InputSchema)
	}
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("order_create schema properties type = %T, want map[string]any", schema["properties"])
	}
	for _, name := range []string{"product", "quantity", "date", "dry_run"} {
		if _, ok := properties[name]; !ok {
			t.Errorf("order_create schema is missing property %q", name)
		}
	}
	if _, ok := properties["force"]; ok {
		t.Error("order_create schema exposes force, which the MCP tool must not offer")
	}
}

func TestOrderListTool(t *testing.T) {
	ctx := context.Background()
	a := newTestApp(t)
	if _, err := a.PlaceOrder(ctx, app.PlaceOrderInput{Quantity: 1}); err != nil {
		t.Fatalf("PlaceOrder: %v", err)
	}

	session := newSession(t, a)
	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "order_list",
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatalf("CallTool order_list: %v", err)
	}
	if result.IsError {
		t.Fatalf("order_list returned a tool error: %s", toolText(t, result))
	}

	var orders []map[string]any
	if err := json.Unmarshal([]byte(toolText(t, result)), &orders); err != nil {
		t.Fatalf("unmarshaling order_list text: %v", err)
	}
	if len(orders) == 0 {
		t.Fatal("order_list returned no orders")
	}
	if got := orders[0]["reference"]; got != "20000001" {
		t.Errorf("orders[0][reference] = %v, want %q", got, "20000001")
	}
}

func TestOrderCreatePendingIsToolError(t *testing.T) {
	ctx := context.Background()
	session := newSession(t, newTestApp(t))

	first, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "order_create",
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatalf("first CallTool order_create: %v", err)
	}
	if first.IsError {
		t.Fatalf("first order_create returned a tool error: %s", toolText(t, first))
	}

	second, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "order_create",
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatalf("second CallTool order_create: %v", err)
	}
	if !second.IsError {
		t.Fatal("second order_create succeeded, want a tool error")
	}

	var envelope map[string]any
	if err := json.Unmarshal([]byte(toolText(t, second)), &envelope); err != nil {
		t.Fatalf("unmarshaling error envelope: %v", err)
	}
	if got := envelope["code"]; got != "pending_order" {
		t.Errorf("envelope code = %v, want %q", got, "pending_order")
	}
	if got := envelope["exit_code"]; got != float64(8) {
		t.Errorf("envelope exit_code = %v, want 8", got)
	}
}
