package mcpserver

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"

	"github.com/felipe9962/repsol-cli/internal/apperr"
)

// noEnv is a getenv that answers empty for every variable.
func noEnv(string) string { return "" }

// readJSONFile decodes a JSON file into a generic object.
func readJSONFile(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile %s: %v", path, err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("Unmarshal %s: %v", path, err)
	}
	return doc
}

// requireStatus asserts the status of an install result.
func requireStatus(t *testing.T, got InstallResult, want string) {
	t.Helper()
	if got.Status != want {
		t.Fatalf("status = %q, want %q", got.Status, want)
	}
}

func TestInstallClaudeCode(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, ".claude.json")
	const original = `{"theme":"dark","mcpServers":{"other":{"command":"x"}}}`
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	command := filepath.Join(home, "bin", "repsol")
	result, err := Install("claude-code", command, []string{"mcp"}, home, noEnv)
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if result.Target != "claude-code" {
		t.Errorf("target = %q, want %q", result.Target, "claude-code")
	}
	if result.Path != path {
		t.Errorf("path = %q, want %q", result.Path, path)
	}
	requireStatus(t, result, "installed")

	doc := readJSONFile(t, path)
	if doc["theme"] != "dark" {
		t.Errorf("theme = %v, want %q", doc["theme"], "dark")
	}
	servers, ok := doc["mcpServers"].(map[string]any)
	if !ok {
		t.Fatalf("mcpServers type = %T, want map[string]any", doc["mcpServers"])
	}
	if _, ok := servers["other"]; !ok {
		t.Error("the other harness entry was dropped")
	}
	entry, ok := servers["repsol"].(map[string]any)
	if !ok {
		t.Fatalf("mcpServers.repsol type = %T, want map[string]any", servers["repsol"])
	}
	if entry["type"] != "stdio" {
		t.Errorf("repsol type = %v, want %q", entry["type"], "stdio")
	}
	if entry["command"] != command {
		t.Errorf("repsol command = %v, want %q", entry["command"], command)
	}
	if args, ok := entry["args"].([]any); !ok || len(args) != 1 || args[0] != "mcp" {
		t.Errorf("repsol args = %v, want [mcp]", entry["args"])
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o644 {
		t.Errorf("mode = %o, want 0644", got)
	}

	again, err := Install("claude-code", command, []string{"mcp"}, home, noEnv)
	if err != nil {
		t.Fatalf("second Install: %v", err)
	}
	requireStatus(t, again, "unchanged")

	other := filepath.Join(home, "other", "repsol")
	updated, err := Install("claude-code", other, []string{"mcp"}, home, noEnv)
	if err != nil {
		t.Fatalf("third Install: %v", err)
	}
	requireStatus(t, updated, "updated")
	doc = readJSONFile(t, path)
	servers = doc["mcpServers"].(map[string]any)
	entry = servers["repsol"].(map[string]any)
	if entry["command"] != other {
		t.Errorf("repsol command = %v, want %q", entry["command"], other)
	}
	if doc["theme"] != "dark" {
		t.Errorf("theme = %v after update, want %q", doc["theme"], "dark")
	}
}

func TestInstallCodex(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, ".codex", "config.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	const original = `[mcp_servers.other]
command = "other-bin"
args = ["serve"]

[mcp_servers.repsol]
command = "old"
args = ["mcp"]
`
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	command := filepath.Join(home, "bin", "repsol")
	result, err := Install("codex", command, []string{"mcp"}, home, noEnv)
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if result.Path != path {
		t.Errorf("path = %q, want %q", result.Path, path)
	}
	requireStatus(t, result, "updated")

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	text := string(data)
	if got := strings.Count(text, "[mcp_servers.repsol]"); got != 1 {
		t.Errorf("[mcp_servers.repsol] headers = %d, want 1\n%s", got, text)
	}
	if !strings.Contains(text, "[mcp_servers.other]") {
		t.Errorf("the other harness entry was dropped\n%s", text)
	}

	var parsed struct {
		MCPServers map[string]struct {
			Command string   `toml:"command"`
			Args    []string `toml:"args"`
		} `toml:"mcp_servers"`
	}
	if err := toml.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("toml.Unmarshal: %v", err)
	}
	other, ok := parsed.MCPServers["other"]
	if !ok {
		t.Fatal("mcp_servers.other is missing after install")
	}
	if other.Command != "other-bin" {
		t.Errorf("mcp_servers.other.command = %q, want %q", other.Command, "other-bin")
	}
	repsol, ok := parsed.MCPServers["repsol"]
	if !ok {
		t.Fatal("mcp_servers.repsol is missing after install")
	}
	if repsol.Command != command {
		t.Errorf("mcp_servers.repsol.command = %q, want %q", repsol.Command, command)
	}
	if len(repsol.Args) != 1 || repsol.Args[0] != "mcp" {
		t.Errorf("mcp_servers.repsol.args = %v, want [mcp]", repsol.Args)
	}

	again, err := Install("codex", command, []string{"mcp"}, home, noEnv)
	if err != nil {
		t.Fatalf("second Install: %v", err)
	}
	requireStatus(t, again, "unchanged")
}

func TestInstallCursorCreatesFile(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, ".cursor", "mcp.json")
	command := filepath.Join(home, "bin", "repsol")

	result, err := Install("cursor", command, []string{"mcp"}, home, noEnv)
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if result.Path != path {
		t.Errorf("path = %q, want %q", result.Path, path)
	}
	requireStatus(t, result, "installed")

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("mode = %o, want 0600", got)
	}
	dirInfo, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatalf("Stat dir: %v", err)
	}
	if got := dirInfo.Mode().Perm(); got != 0o700 {
		t.Errorf("parent dir mode = %o, want 0700", got)
	}

	doc := readJSONFile(t, path)
	servers, ok := doc["mcpServers"].(map[string]any)
	if !ok {
		t.Fatalf("mcpServers type = %T, want map[string]any", doc["mcpServers"])
	}
	entry, ok := servers["repsol"].(map[string]any)
	if !ok {
		t.Fatalf("mcpServers.repsol type = %T, want map[string]any", servers["repsol"])
	}
	if entry["command"] != command {
		t.Errorf("repsol command = %v, want %q", entry["command"], command)
	}
}

func TestInstallUnknownTarget(t *testing.T) {
	_, err := Install("vim", "repsol", []string{"mcp"}, t.TempDir(), noEnv)
	if err == nil {
		t.Fatal("Install accepted an unknown target")
	}
	var typed *apperr.Error
	if !errors.As(err, &typed) {
		t.Fatalf("error type = %T, want *apperr.Error", err)
	}
	if typed.Category != apperr.Usage {
		t.Errorf("category = %v, want %v", typed.Category, apperr.Usage)
	}
}
