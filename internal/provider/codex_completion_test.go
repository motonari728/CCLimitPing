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
		{"completed", `printf '\033]9;done\007'`, "", true},
		{"clean-without-marker", "exit 0", "completion unconfirmed", false},
		{"process-error", "exit 1", "interactive failed", false},
		{"timeout", "exec sleep 5", "timed out", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fakeCodexCLI(t, tc.script)
			res, err := triggerCodexWithTiming(context.Background(), config.ProviderConfig{}, false,
				codexInteractiveTiming{maxWait: 100 * time.Millisecond, exitGrace: 20 * time.Millisecond})
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
	fakeCodexCLI(t, "exec sleep 5")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	res, err := triggerCodexWithTiming(ctx, config.ProviderConfig{}, false,
		codexInteractiveTiming{maxWait: time.Second, exitGrace: 20 * time.Millisecond})
	if err == nil || res.TurnCompleted {
		t.Fatal(res, err)
	}
}

func TestCodexAwaitDrainsCompletionAfterProcessExit(t *testing.T) {
	markers := newCodexTurnMarkers()
	readDone := make(chan struct{})
	done := make(chan error, 1)
	done <- nil
	go func() {
		time.Sleep(5 * time.Millisecond)
		_, _ = markers.Write([]byte("\x1b]9;complete\x07"))
		close(readDone)
	}()
	terminal, completed, err := codexAwait(context.Background(), nil, nil, &limitedBuffer{limit: 4096},
		markers.completed, readDone, done, time.Second)
	if !terminal || !completed || err != nil {
		t.Fatal(terminal, completed, err)
	}
}

func TestCodexCompletionMarkerFragments(t *testing.T) {
	m := newCodexTurnMarkers()
	for _, fragment := range []string{"noise\x1b]", "9;", "finished", "\a"} {
		_, _ = m.Write([]byte(fragment))
	}
	select {
	case <-m.completed:
	default:
		t.Fatal("fragmented notification not detected")
	}
}
