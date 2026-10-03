package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/wavever/CCLimitPing/internal/provider"
	"github.com/wavever/CCLimitPing/internal/usage"
)

// A ping's own output cannot answer the question it is run to answer: a ping is
// too small to move the used percentage, so the window state has to be read
// back from the provider afterwards.
func TestRunPingsReportsTheWindowStateAfterward(t *testing.T) {
	setLocale(t, "C")
	text := localizedText()
	p := fakeStatusProvider{
		name: "codex",
		usage: &usage.Usage{
			Provider:  "codex",
			Plan:      "plus",
			FiveHour:  usage.Window{ResetsAt: time.Now().Add(5 * time.Hour), WindowSeconds: 18000},
			FetchedAt: time.Now(),
		},
	}

	var out bytes.Buffer
	if err := runPings(context.Background(), &out, text, []provider.Provider{p}, false, false, "used"); err != nil {
		t.Fatalf("runPings() error = %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "✓ pinged") {
		t.Fatalf("output = %q, want the ping result", got)
	}
	if !strings.Contains(got, "codex (plus)") || !strings.Contains(got, "5h") {
		t.Fatalf("output = %q, want the window state after the ping", got)
	}
}

// A dry run sends nothing, so there is no new state to read back — and reading
// it would suggest the window was touched.
func TestRunPingsDryRunReportsNoWindowState(t *testing.T) {
	setLocale(t, "C")
	text := localizedText()
	read := 0
	p := fakeStatusProvider{
		name:   "codex",
		usage:  &usage.Usage{Provider: "codex"},
		onRead: func() { read++ },
	}

	var out bytes.Buffer
	if err := runPings(context.Background(), &out, text, []provider.Provider{p}, true, false, "used"); err != nil {
		t.Fatalf("runPings() error = %v", err)
	}
	if read != 0 {
		t.Fatalf("usage reads = %d, want none for a dry run", read)
	}
	if !strings.Contains(out.String(), "would run") {
		t.Fatalf("output = %q, want the dry-run command", out.String())
	}
}

func TestCommandLineNamesTheModelOnlyWhenTheCommandDoesNot(t *testing.T) {
	setLocale(t, "C")
	text := localizedText()

	cases := []struct {
		name string
		res  provider.TriggerResult
		want string
	}{{
		name: "model absent from the command is appended",
		res:  provider.TriggerResult{Command: "codex -c model_reasoning_effort=low ok", Model: "gpt-5.6-sol"},
		want: "codex -c model_reasoning_effort=low ok  (model: gpt-5.6-sol)",
	}, {
		name: "model already in the command is not repeated",
		res:  provider.TriggerResult{Command: "codex -m gpt-5.6-luna ok", Model: "gpt-5.6-luna"},
		want: "codex -m gpt-5.6-luna ok",
	}, {
		name: "unresolvable model adds nothing",
		res:  provider.TriggerResult{Command: "claude ."},
		want: "claude .",
	}}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := commandLine(text, &c.res); got != c.want {
				t.Fatalf("commandLine() = %q, want %q", got, c.want)
			}
		})
	}
}

func TestCommandLineIsLocalized(t *testing.T) {
	setLocale(t, "zh_CN.UTF-8")
	res := provider.TriggerResult{Command: "codex ok", Model: "gpt-5.6-sol"}
	if got := commandLine(localizedText(), &res); !strings.Contains(got, "模型: gpt-5.6-sol") {
		t.Fatalf("commandLine() = %q, want a localized model label", got)
	}
}
