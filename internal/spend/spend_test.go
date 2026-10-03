package spend

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wavever/CCLimitPing/internal/pricing"
)

func TestForPricesEachModelAndRanksThem(t *testing.T) {
	now := time.Now()
	dir := writeClaudeTranscript(t, "project-a", "session.jsonl", []string{
		claudeLine(now, "msg_1", "req_1", "priced-model", 1000, 0, 0, 100),
		claudeLine(now, "msg_2", "req_2", "unpriced-model", 10, 0, 0, 1),
	})
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	stubPrices(t, map[string]pricing.Price{
		"priced-model": {InputPerToken: 1e-5, OutputPerToken: 1e-4},
	})

	day, err := Today(context.Background(), "claude")
	if err != nil {
		t.Fatalf("Today() error = %v", err)
	}
	if !day.Available || day.Tokens.Total() != 1111 {
		t.Fatalf("day = %+v, want 1111 tokens from an available provider", day)
	}
	if want := 0.02; day.CostUSD < want-1e-9 || day.CostUSD > want+1e-9 {
		t.Fatalf("day.CostUSD = %v, want %v", day.CostUSD, want)
	}
	// A model with no published rates leaves the total a lower bound, and the
	// day says so rather than passing the estimate off as complete.
	if day.Priced {
		t.Fatal("day.Priced = true, want false when a model had no rates")
	}
	if len(day.Models) != 2 || day.Models[0].Model != "priced-model" {
		t.Fatalf("day.Models = %+v, want the heaviest model first", day.Models)
	}
	if day.Models[1].Priced || day.Models[1].CostUSD != 0 {
		t.Fatalf("day.Models[1] = %+v, want the unpriced model counted but not costed", day.Models[1])
	}
}

func TestForIgnoresAnUnknownProvider(t *testing.T) {
	day, err := For(context.Background(), "gemini", time.Now())
	if err != nil {
		t.Fatalf("For() error = %v", err)
	}
	if day.Available || !day.Empty() {
		t.Fatalf("day = %+v, want an unavailable, empty day", day)
	}
}

func TestForEachLineSkipsOverlongLinesAndKeepsTheRest(t *testing.T) {
	path := filepath.Join(t.TempDir(), "transcript.jsonl")
	// Tool output routinely runs to megabytes; buffering one whole would cost
	// far more than the usage record it is standing between.
	body := "first\n" + strings.Repeat("x", maxLine+1024) + "\nlast\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	var got []string
	if err := forEachLine(path, func(line []byte) {
		got = append(got, strings.TrimSpace(string(line[:min(len(line), 16)])))
	}); err != nil {
		t.Fatalf("forEachLine() error = %v", err)
	}
	if len(got) != 2 || got[0] != "first" || got[1] != "last" {
		t.Fatalf("lines = %v, want the short lines either side of the huge one", got)
	}
}

func TestForEachLineReadsATrailingLineWithoutNewline(t *testing.T) {
	path := filepath.Join(t.TempDir(), "transcript.jsonl")
	if err := os.WriteFile(path, []byte("one\ntwo"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	var got []string
	if err := forEachLine(path, func(line []byte) { got = append(got, strings.TrimSpace(string(line))) }); err != nil {
		t.Fatalf("forEachLine() error = %v", err)
	}
	if len(got) != 2 || got[1] != "two" {
		t.Fatalf("lines = %v, want the unterminated last line too", got)
	}
}

func TestWithinDayExcludesUndatedRecords(t *testing.T) {
	start := startOfDay(time.Now())
	end := start.AddDate(0, 0, 1)
	if withinDay("", start, end) || withinDay("not-a-time", start, end) {
		t.Fatal("withinDay() accepted a record it could not date")
	}
	if !withinDay(start.Add(time.Hour).Format(time.RFC3339), start, end) {
		t.Fatal("withinDay() rejected a record from inside the day")
	}
	if withinDay(end.Format(time.RFC3339), start, end) {
		t.Fatal("withinDay() accepted tomorrow's first record")
	}
}

// stubPrices swaps the pricing lookup for a fixed table, keeping the tests off
// the live dataset.
func stubPrices(t *testing.T, table map[string]pricing.Price) {
	t.Helper()
	original := lookupPrice
	lookupPrice = func(_ context.Context, model string) (pricing.Price, bool) {
		p, ok := table[model]
		return p, ok
	}
	t.Cleanup(func() { lookupPrice = original })
}
