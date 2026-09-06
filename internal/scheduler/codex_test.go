package scheduler

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/wavever/CCLimitPing/internal/provider"
	"github.com/wavever/CCLimitPing/internal/usage"
)

type verifiedStub struct{ stubProvider }

func (p *verifiedStub) TriggerAutomatic(ctx context.Context, _ float64) (*provider.TriggerResult, error) {
	return p.Trigger(ctx, false)
}

func TestVerifiedSchedulerGates(t *testing.T) {
	for _, tc := range []struct {
		name, recovery string
		warn, active   bool
		weekly         float64
		want           int
	}{
		{"unknown", "verifying", false, false, 0, 0},
		{"ready", "ready", false, false, 0, 1},
		{"cooldown", "cooldown", false, false, 0, 0},
		{"storage-error", "ready", true, false, 0, 0},
		{"active-user", "ready", false, true, 0, 0},
		{"weekly-guard", "ready", false, false, 100, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := &usage.Verification{Target: "five_hour", Recovery: tc.recovery}
			if tc.warn {
				v.Warning = "cannot write state"
			}
			p := &verifiedStub{stubProvider: stubProvider{active: tc.active, usage: &usage.Usage{
				Weekly: usage.Window{UsedPercent: tc.weekly, ResetsAt: time.Now().Add(time.Hour)}, Verification: v}}}
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
			defer cancel()
			s := New(testConfig(), []Target{{Provider: p}}, false, false, io.Discard)
			s.Run(ctx)
			_, n := p.counts()
			if n != tc.want {
				t.Fatalf("triggers=%d", n)
			}
		})
	}
}
