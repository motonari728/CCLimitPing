package spend

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// claudeLine renders one assistant transcript entry the way Claude Code writes
// them, so the fixtures read like the files this parses.
func claudeLine(stamp time.Time, msgID, requestID, model string, in, cacheWrite, cacheRead, out int) string {
	return fmt.Sprintf(`{"type":"assistant","timestamp":%q,"requestId":%q,"sessionId":"s1","message":`+
		`{"id":%q,"model":%q,"usage":{"input_tokens":%d,"cache_creation_input_tokens":%d,`+
		`"cache_read_input_tokens":%d,"output_tokens":%d}}}`,
		stamp.UTC().Format(time.RFC3339Nano), requestID, msgID, model, in, cacheWrite, cacheRead, out)
}

func TestReadClaudeCountsOneEntryPerRequest(t *testing.T) {
	now := time.Now()
	dir := writeClaudeTranscript(t, "project-a", "session.jsonl", []string{
		// Streaming writes the same assistant message repeatedly; only one of
		// these was a billed request.
		claudeLine(now, "msg_1", "req_1", "claude-opus-5", 10, 100, 1000, 50),
		claudeLine(now, "msg_1", "req_1", "claude-opus-5", 10, 100, 1000, 50),
		claudeLine(now, "msg_2", "req_2", "claude-opus-5", 5, 0, 2000, 25),
		claudeLine(now, "msg_3", "req_3", "claude-haiku-4-5", 1, 0, 3, 7),
		// Yesterday's turn, still in a transcript touched today.
		claudeLine(now.AddDate(0, 0, -1), "msg_0", "req_0", "claude-opus-5", 999, 999, 999, 999),
		// A local error rendered as an assistant turn never reached the API.
		claudeLine(now, "msg_4", "req_4", "<synthetic>", 1, 1, 1, 1),
	})
	t.Setenv("CLAUDE_CONFIG_DIR", dir)

	start := startOfDay(now)
	byModel, available, err := readClaude(start, start.AddDate(0, 0, 1))
	if err != nil {
		t.Fatalf("readClaude() error = %v", err)
	}
	if !available {
		t.Fatal("readClaude() available = false, want true for an existing projects dir")
	}
	opus := byModel["claude-opus-5"]
	if opus.Input != 15 || opus.CacheWrite != 100 || opus.CacheRead != 3000 || opus.Output != 75 {
		t.Fatalf("claude-opus-5 tokens = %+v, want the two of today's requests counted once each", opus)
	}
	if got := byModel["claude-haiku-4-5"].Total(); got != 11 {
		t.Fatalf("claude-haiku-4-5 total = %d, want 11", got)
	}
	if _, ok := byModel["<synthetic>"]; ok {
		t.Fatal("synthetic entries were counted, want them skipped")
	}
}

func TestReadClaudeSkipsTranscriptsUntouchedToday(t *testing.T) {
	now := time.Now()
	dir := writeClaudeTranscript(t, "project-a", "old.jsonl", []string{
		claudeLine(now, "msg_1", "req_1", "claude-opus-5", 10, 0, 0, 10),
	})
	// A transcript is append-only, so one last written before today cannot hold
	// today's turns — whatever its contents claim.
	path := filepath.Join(dir, "projects", "project-a", "old.jsonl")
	stale := now.AddDate(0, 0, -2)
	if err := os.Chtimes(path, stale, stale); err != nil {
		t.Fatalf("Chtimes() error = %v", err)
	}
	t.Setenv("CLAUDE_CONFIG_DIR", dir)

	start := startOfDay(now)
	byModel, _, err := readClaude(start, start.AddDate(0, 0, 1))
	if err != nil {
		t.Fatalf("readClaude() error = %v", err)
	}
	if len(byModel) != 0 {
		t.Fatalf("byModel = %v, want nothing from a transcript untouched today", byModel)
	}
}

func TestReadClaudeReportsUnavailableWithoutAProjectsDir(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())

	start := startOfDay(time.Now())
	_, available, err := readClaude(start, start.AddDate(0, 0, 1))
	if err != nil {
		t.Fatalf("readClaude() error = %v", err)
	}
	if available {
		t.Fatal("readClaude() available = true, want false when Claude Code has never run here")
	}
}

func TestClaudeRootsSplitsTheConfigDirList(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", "/one, /two ,")
	if got := claudeRoots(); len(got) != 2 || got[0] != "/one" || got[1] != "/two" {
		t.Fatalf("claudeRoots() = %v, want both entries trimmed", got)
	}
}

func writeClaudeTranscript(t *testing.T, project, name string, lines []string) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "projects", project)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	body := ""
	for _, line := range lines {
		body += line + "\n"
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	return root
}
