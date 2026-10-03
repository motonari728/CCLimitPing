package spend

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/wavever/CCLimitPing/internal/pricing"
)

// syntheticModel marks a Claude Code transcript entry that never reached the
// API (a local error message rendered as an assistant turn). It has no cost and
// no tokens to count.
const syntheticModel = "<synthetic>"

// claudeEntry is the part of a Claude Code transcript line that carries usage.
type claudeEntry struct {
	Type      string `json:"type"`
	Timestamp string `json:"timestamp"`
	RequestID string `json:"requestId"`
	Message   struct {
		ID    string `json:"id"`
		Model string `json:"model"`
		Usage struct {
			InputTokens              int `json:"input_tokens"`
			CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
			CacheReadInputTokens     int `json:"cache_read_input_tokens"`
			OutputTokens             int `json:"output_tokens"`
		} `json:"usage"`
	} `json:"message"`
}

// readClaude totals Claude Code's transcript usage for [start, end) per model.
func readClaude(start, end time.Time) (map[string]pricing.Tokens, bool, error) {
	byModel := map[string]pricing.Tokens{}
	// Claude Code writes the same assistant message once per stream update, so
	// roughly half the usage lines in a transcript are repeats of a request
	// already counted. Keyed on the API's own ids, not on the file position.
	seen := map[string]bool{}
	available := false
	var firstErr error

	for _, root := range claudeRoots() {
		projects := filepath.Join(root, "projects")
		if !dirExists(projects) {
			continue
		}
		available = true
		files, err := transcripts(projects, start)
		if err != nil && firstErr == nil {
			firstErr = err
		}
		for _, path := range files {
			if err := readClaudeFile(path, start, end, seen, byModel); err != nil && firstErr == nil {
				firstErr = err
			}
		}
	}
	return byModel, available, firstErr
}

func readClaudeFile(path string, start, end time.Time, seen map[string]bool, byModel map[string]pricing.Tokens) error {
	marker := []byte(`"usage"`)
	return forEachLine(path, func(line []byte) {
		if !bytes.Contains(line, marker) {
			return
		}
		var e claudeEntry
		if err := json.Unmarshal(line, &e); err != nil {
			return
		}
		if e.Type != "assistant" || e.Message.Model == "" || e.Message.Model == syntheticModel {
			return
		}
		if !withinDay(e.Timestamp, start, end) {
			return
		}
		if key := e.Message.ID + "|" + e.RequestID; key != "|" {
			if seen[key] {
				return
			}
			seen[key] = true
		}
		u := e.Message.Usage
		tokens := byModel[e.Message.Model]
		tokens.Add(pricing.Tokens{
			Input:      u.InputTokens,
			CacheRead:  u.CacheReadInputTokens,
			CacheWrite: u.CacheCreationInputTokens,
			Output:     u.OutputTokens,
		})
		byModel[e.Message.Model] = tokens
	})
}

// claudeRoots returns the directories that hold Claude Code's transcripts:
// $CLAUDE_CONFIG_DIR when set (the CLI accepts a comma-separated list), else
// both well-known locations.
func claudeRoots() []string {
	if v := strings.TrimSpace(os.Getenv("CLAUDE_CONFIG_DIR")); v != "" {
		var out []string
		for _, dir := range strings.Split(v, ",") {
			if dir = strings.TrimSpace(dir); dir != "" {
				out = append(out, dir)
			}
		}
		return out
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	return []string{
		filepath.Join(home, ".claude"),
		filepath.Join(home, ".config", "claude"),
	}
}
