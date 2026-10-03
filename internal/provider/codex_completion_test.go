package provider

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/wavever/CCLimitPing/internal/config"
)

func TestCodexCompletionOutcomes(t *testing.T) {
	for _, tc := range []struct {
		name, script, wantError string
		completed               bool
	}{
		{"completed", `printf '%s\n' '{"type":"turn.completed","usage":{"input_tokens":1,"output_tokens":1}}'`, "", true},
		{"clean-without-event", "exit 0", "started no turn", false},
		{"process-error", "exit 1", "exec failed", false},
		{"completed-process-error", `printf '%s\n' '{"type":"turn.completed","usage":{"input_tokens":1,"output_tokens":1}}'; exit 1`, "exec failed", true},
		{"timeout", "exec sleep 5", "exec failed", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fakeCodexHome(t)
			fakeCodexCLI(t, tc.script)
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			res, err := triggerCodex(ctx, config.ProviderConfig{}, false)
			if res.TurnCompleted != tc.completed {
				t.Fatalf("completed=%v", res.TurnCompleted)
			}
			if tc.wantError == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tc.wantError) {
				t.Fatal(err)
			}
		})
	}
}

func TestCodexCompletionCancellation(t *testing.T) {
	fakeCodexHome(t)
	fakeCodexCLI(t, "exec sleep 5")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	res, err := triggerCodex(ctx, config.ProviderConfig{}, false)
	if err == nil || res.TurnCompleted {
		t.Fatal(res, err)
	}
}
