package mcpserver

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/pelletier/go-toml/v2"

	"github.com/felipe9962/repsol-cli/internal/apperr"
)

// The install targets.
const (
	// targetClaudeCode is Claude Code.
	targetClaudeCode = "claude-code"
	// targetCursor is Cursor.
	targetCursor = "cursor"
	// targetCodex is Codex.
	targetCodex = "codex"
)

// The install statuses.
const (
	// statusInstalled means the entry did not exist and was written.
	statusInstalled = "installed"
	// statusUpdated means an existing entry was rewritten.
	statusUpdated = "updated"
	// statusUnchanged means the entry already matched and the file was left
	// untouched.
	statusUnchanged = "unchanged"
)

// codexHeader is the TOML table of the Codex entry.
const codexHeader = "[mcp_servers.repsol]"

// InstallResult is the outcome of installing the server entry into a harness
// configuration.
type InstallResult struct {
	// Target is the harness name.
	Target string `json:"target"`
	// Path is the configuration file that was written.
	Path string `json:"path"`
	// Status is installed, updated or unchanged.
	Status string `json:"status"`
}

// Install writes the MCP server entry of the harness target into its
// configuration, preserving every other entry.
//
// command is the executable a harness must run and args its arguments. home is
// the user's home directory and getenv reads the environment, so the caller
// decides where the harness keeps its files. A target this function does not
// know is a usage error.
func Install(target, command string, args []string, home string, getenv func(string) string) (InstallResult, error) {
	if getenv == nil {
		getenv = func(string) string { return "" }
	}

	switch target {
	case targetClaudeCode:
		dir := getenv("CLAUDE_CONFIG_DIR")
		if dir == "" {
			var err error
			if dir, err = requireHome(home); err != nil {
				return InstallResult{}, err
			}
		}
		return installJSON(target, filepath.Join(dir, ".claude.json"), map[string]any{
			"type":    "stdio",
			"command": command,
			"args":    args,
		})
	case targetCursor:
		dir, err := requireHome(home)
		if err != nil {
			return InstallResult{}, err
		}
		return installJSON(target, filepath.Join(dir, ".cursor", "mcp.json"), map[string]any{
			"command": command,
			"args":    args,
		})
	case targetCodex:
		dir := getenv("CODEX_HOME")
		if dir == "" {
			var err error
			if dir, err = requireHome(home); err != nil {
				return InstallResult{}, err
			}
			dir = filepath.Join(dir, ".codex")
		}
		return installCodex(filepath.Join(dir, "config.toml"), command, args)
	default:
		return InstallResult{}, apperr.New(apperr.Usage, "unknown_target",
			fmt.Sprintf("unknown install target %q", target)).
			WithHint("use one of claude-code, codex or cursor")
	}
}

// requireHome returns home, or a usage error when it is unknown.
func requireHome(home string) (string, error) {
	if home == "" {
		return "", apperr.New(apperr.Usage, "missing_home",
			"the home directory is unknown").
			WithHint("set HOME and try again")
	}
	return home, nil
}

// installJSON writes the entry under mcpServers.repsol of a JSON configuration,
// keeping every other key and the mode of an existing file.
func installJSON(target, path string, entry map[string]any) (InstallResult, error) {
	doc := map[string]any{}
	mode := fs.FileMode(0o600)

	data, err := os.ReadFile(path)
	switch {
	case err == nil:
		if info, statErr := os.Stat(path); statErr == nil {
			mode = info.Mode().Perm()
		}
		if len(bytes.TrimSpace(data)) > 0 {
			if err := json.Unmarshal(data, &doc); err != nil {
				return InstallResult{}, invalidJSON(path)
			}
		}
		if doc == nil {
			// A file holding "null" decodes to a nil map.
			doc = map[string]any{}
		}
	case errors.Is(err, fs.ErrNotExist):
	default:
		return InstallResult{}, configReadError(path, err)
	}

	servers := map[string]any{}
	switch raw := doc["mcpServers"].(type) {
	case nil:
	case map[string]any:
		servers = raw
	default:
		return InstallResult{}, apperr.New(apperr.Usage, "harness_config_invalid",
			fmt.Sprintf("%s has an mcpServers value that is not an object", path)).
			WithHint("edit " + path + " by hand")
	}

	result := InstallResult{Target: target, Path: path, Status: statusInstalled}
	if existing, ok := servers["repsol"]; ok {
		if sameJSONEntry(existing, entry) {
			result.Status = statusUnchanged
			return result, nil
		}
		result.Status = statusUpdated
	}

	servers["repsol"] = entry
	doc["mcpServers"] = servers

	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return InstallResult{}, apperr.New(apperr.Generic, "install_failed",
			fmt.Sprintf("%s could not be encoded", path)).WithCause(err)
	}
	out = append(out, '\n')
	if err := writeConfig(path, out, mode); err != nil {
		return InstallResult{}, err
	}
	return result, nil
}

// sameJSONEntry reports whether existing already has the command and args the
// caller wants. An install is unchanged when they match, whatever other keys
// the entry carries.
func sameJSONEntry(existing any, want map[string]any) bool {
	entry, ok := existing.(map[string]any)
	if !ok {
		return false
	}
	for _, key := range []string{"command", "args"} {
		existingValue, ok := entry[key]
		if !ok || !sameJSONValue(existingValue, want[key]) {
			return false
		}
	}
	return true
}

// sameJSONValue compares two decoded JSON values by their encoding, so the
// []any a file decodes to matches the []string the caller passes.
func sameJSONValue(a, b any) bool {
	encodedA, errA := json.Marshal(a)
	encodedB, errB := json.Marshal(b)
	return errA == nil && errB == nil && bytes.Equal(encodedA, encodedB)
}

// installCodex writes the [mcp_servers.repsol] block of a Codex configuration,
// replacing it in place or appending it, and keeps every other table.
func installCodex(path, command string, args []string) (InstallResult, error) {
	mode := fs.FileMode(0o600)
	data, err := os.ReadFile(path)
	existed := err == nil
	switch {
	case existed:
		if info, statErr := os.Stat(path); statErr == nil {
			mode = info.Mode().Perm()
		}
	case errors.Is(err, fs.ErrNotExist):
	default:
		return InstallResult{}, configReadError(path, err)
	}

	text := string(data)
	result := InstallResult{Target: targetCodex, Path: path}

	// An entry that already matches is left alone.
	if entry, ok := parseCodexEntry(text); ok && entry.Command == command && slices.Equal(entry.Args, args) {
		result.Status = statusUnchanged
		return result, nil
	}

	result.Status = statusInstalled
	if hasCodexSection(text) {
		result.Status = statusUpdated
	}

	updated := upsertCodex(text, codexBlock(command, args))
	entry, ok := parseCodexEntry(updated)
	if !ok || entry.Command != command || !slices.Equal(entry.Args, args) {
		return InstallResult{}, apperr.New(apperr.Usage, "codex_config_conflict",
			fmt.Sprintf("%s could not be updated without breaking it", path)).
			WithHint("edit " + path + " by hand and add the " + codexHeader + " table")
	}

	if err := writeConfig(path, []byte(updated), mode); err != nil {
		return InstallResult{}, err
	}
	return result, nil
}

// codexConfig is the part of a Codex configuration this package reads back.
type codexConfig struct {
	MCPServers map[string]codexEntry `toml:"mcp_servers"`
}

// codexEntry is one MCP server entry of a Codex configuration.
type codexEntry struct {
	Command string   `toml:"command"`
	Args    []string `toml:"args"`
}

// parseCodexEntry returns the repsol entry of a Codex configuration and whether
// the text parses and holds one.
func parseCodexEntry(text string) (codexEntry, bool) {
	if strings.TrimSpace(text) == "" {
		return codexEntry{}, false
	}
	var cfg codexConfig
	if err := toml.Unmarshal([]byte(text), &cfg); err != nil {
		return codexEntry{}, false
	}
	entry, ok := cfg.MCPServers["repsol"]
	return entry, ok
}

// hasCodexSection reports whether the text already holds the repsol table.
func hasCodexSection(text string) bool {
	for _, line := range splitLines(text) {
		if strings.TrimSpace(line) == codexHeader {
			return true
		}
	}
	return false
}

// upsertCodex replaces the repsol table of text with block, or appends it after
// a blank line. block is a slice of lines without a trailing empty line.
func upsertCodex(text string, block []string) string {
	lines := splitLines(text)

	start := -1
	for i, line := range lines {
		if strings.TrimSpace(line) == codexHeader {
			start = i
			break
		}
	}

	if start < 0 {
		out := lines
		if len(out) > 0 {
			out = append(out, "")
		}
		out = append(out, block...)
		return strings.Join(out, "\n") + "\n"
	}

	// The table runs up to the next header.
	end := len(lines)
	for i := start + 1; i < len(lines); i++ {
		if strings.HasPrefix(strings.TrimSpace(lines[i]), "[") {
			end = i
			break
		}
	}

	out := make([]string, 0, len(lines)+len(block)+1)
	out = append(out, lines[:start]...)
	out = append(out, block...)
	if end < len(lines) {
		out = append(out, "")
	}
	out = append(out, lines[end:]...)
	return strings.Join(out, "\n") + "\n"
}

// splitLines splits text into lines, dropping the empty element a trailing
// newline produces.
func splitLines(text string) []string {
	if text == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(text, "\n"), "\n")
}

// codexBlock returns the lines of the repsol table.
func codexBlock(command string, args []string) []string {
	quoted := make([]string, len(args))
	for i, arg := range args {
		quoted[i] = tomlString(arg)
	}
	return []string{
		codexHeader,
		"command = " + tomlString(command),
		"args = [" + strings.Join(quoted, ", ") + "]",
	}
}

// tomlString writes s as a TOML basic string. Backslashes and quotes are
// escaped, so a Windows path survives.
func tomlString(s string) string {
	replacer := strings.NewReplacer(
		`\`, `\\`,
		`"`, `\"`,
		"\n", `\n`,
		"\r", `\r`,
		"\t", `\t`,
	)
	return `"` + replacer.Replace(s) + `"`
}

// writeConfig creates the parent directory with a private mode and writes data
// to path, keeping the mode of an existing file.
func writeConfig(path string, data []byte, mode fs.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return apperr.New(apperr.Generic, "install_failed",
			fmt.Sprintf("the directory %s could not be created", dir)).WithCause(err)
	}
	if err := os.WriteFile(path, data, mode); err != nil {
		return apperr.New(apperr.Generic, "install_failed",
			fmt.Sprintf("%s could not be written", path)).WithCause(err)
	}
	return nil
}

// invalidJSON is the error of a harness configuration that is not a JSON
// object.
func invalidJSON(path string) error {
	return apperr.New(apperr.Usage, "harness_config_invalid",
		fmt.Sprintf("%s is not a JSON object", path)).
		WithHint("edit " + path + " by hand: the file must hold one JSON object")
}

// configReadError is the error of a harness configuration that could not be
// read.
func configReadError(path string, err error) error {
	return apperr.New(apperr.Generic, "install_failed",
		fmt.Sprintf("%s could not be read", path)).WithCause(err)
}
