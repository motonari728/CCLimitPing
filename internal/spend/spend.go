// Package spend reports how many tokens the local Claude Code / Codex CLIs
// consumed on a given day and what that would have cost at API rates.
//
// The providers' usage endpoints only publish rate-limit percentages — never a
// token count — so the numbers come from the transcripts the CLIs already write
// to disk (~/.claude/projects, ~/.codex/sessions), the same source ccusage and
// CodexBar read. That makes this a local-machine view: work done from another
// machine or from the web app is not in these logs and is not counted.
package spend

import (
	"bufio"
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/wavever/CCLimitPing/internal/pricing"
)

// ModelSpend is one model's share of a day.
type ModelSpend struct {
	Model   string
	Tokens  pricing.Tokens
	CostUSD float64
	// Priced is false when the pricing dataset has no rates for this model, in
	// which case its tokens are counted but its cost is not.
	Priced bool
}

// Day is a provider's local token consumption over one local calendar day.
type Day struct {
	Provider string
	// Date is local midnight of the day covered.
	Date time.Time
	// Available reports whether the provider's transcript directory exists at
	// all. False means this CLI has never run on this machine, which is very
	// different from "it ran and spent nothing".
	Available bool
	Tokens    pricing.Tokens
	CostUSD   float64
	// Priced is false when at least one model that consumed tokens had no
	// published rates, making CostUSD a lower bound.
	Priced bool
	// Models is the per-model breakdown, most tokens first.
	Models []ModelSpend
}

// Empty reports whether the day recorded no tokens at all.
func (d Day) Empty() bool { return d.Tokens.Total() == 0 }

// lookupPrice resolves a model's rates. It is a variable so tests can price
// their fixtures without reaching for the live dataset.
var lookupPrice = func(ctx context.Context, model string) (pricing.Price, bool) {
	return pricing.Default().Lookup(ctx, model)
}

// Today returns provider's consumption so far in the current local day.
func Today(ctx context.Context, provider string) (Day, error) {
	return For(ctx, provider, time.Now())
}

// For returns provider's consumption over the local day containing at. An
// unknown provider yields an unavailable Day rather than an error: reading
// local transcripts is a best-effort extra, never a reason for status to fail.
func For(ctx context.Context, provider string, at time.Time) (Day, error) {
	start := startOfDay(at)
	day := Day{Provider: provider, Date: start, Priced: true}

	var (
		byModel   map[string]pricing.Tokens
		available bool
		err       error
	)
	switch provider {
	case "claude":
		byModel, available, err = readClaude(start, start.AddDate(0, 0, 1))
	case "codex":
		byModel, available, err = readCodex(start, start.AddDate(0, 0, 1))
	default:
		return day, nil
	}
	// An unreadable transcript is reported, but whatever was read is still
	// totalled: a partial day beats no day at all.
	day.Available = available

	for model, tokens := range byModel {
		ms := ModelSpend{Model: model, Tokens: tokens}
		if price, ok := lookupPrice(ctx, model); ok {
			ms.CostUSD, ms.Priced = price.CostOf(tokens), true
			day.CostUSD += ms.CostUSD
		} else if tokens.Total() > 0 {
			day.Priced = false
		}
		day.Tokens.Add(tokens)
		day.Models = append(day.Models, ms)
	}
	sort.Slice(day.Models, func(i, j int) bool {
		if a, b := day.Models[i].Tokens.Total(), day.Models[j].Tokens.Total(); a != b {
			return a > b
		}
		return day.Models[i].Model < day.Models[j].Model
	})
	return day, err
}

func startOfDay(t time.Time) time.Time {
	t = t.Local()
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}

// withinDay reports whether an RFC3339 transcript stamp falls in [start, end).
// An unparseable or missing stamp is excluded: a record that cannot be dated
// cannot be attributed to today.
func withinDay(stamp string, start, end time.Time) bool {
	if stamp == "" {
		return false
	}
	t, err := time.Parse(time.RFC3339, stamp)
	if err != nil {
		return false
	}
	return !t.Before(start) && t.Before(end)
}

// transcripts returns the .jsonl files under root that were last written on or
// after notBefore. A transcript is append-only, so one untouched since before
// the day cannot hold any of its records — which is what keeps this cheap on a
// history of hundreds of megabytes.
func transcripts(root string, notBefore time.Time) ([]string, error) {
	var out []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			// An unreadable corner of the tree must not lose the rest of it.
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if filepath.Ext(path) != ".jsonl" {
			return nil
		}
		info, ierr := d.Info()
		if ierr != nil || info.ModTime().Before(notBefore) {
			return nil
		}
		out = append(out, path)
		return nil
	})
	return out, err
}

// maxLine caps how much memory one transcript line may cost. Usage records are
// small; the multi-megabyte lines in these logs are tool output and file
// contents, which carry no usage, so skipping them loses nothing.
const maxLine = 4 << 20

// forEachLine calls fn for every newline-delimited record in path. fn must not
// retain line, which is reused across calls.
func forEachLine(path string, fn func(line []byte)) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	r := bufio.NewReaderSize(f, 256<<10)
	var line []byte
	skipping := false
	for {
		chunk, err := r.ReadSlice('\n')
		if !skipping {
			if len(line)+len(chunk) > maxLine {
				line, skipping = line[:0], true
			} else {
				line = append(line, chunk...)
			}
		}
		if errors.Is(err, bufio.ErrBufferFull) {
			continue // partial line: keep reading until the newline turns up
		}
		if !skipping && len(line) > 0 {
			fn(line)
		}
		line, skipping = line[:0], false
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
	}
}

// dirExists reports whether path is an existing directory.
func dirExists(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.IsDir()
}
