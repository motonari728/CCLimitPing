package scheduler

import (
	"context"
	"errors"
	"time"

	"github.com/wavever/CCLimitPing/internal/codexstate"
	"github.com/wavever/CCLimitPing/internal/provider"
)

// runVerifiedTarget is shared by Codex and Spark, never by Claude. A successful
// transport does not advance a quota schedule without observation evidence.
func (s *Scheduler) runVerifiedTarget(ctx context.Context, t Target, p provider.VerifiedTrigger) {
	name := t.Provider.Name()
	backoff := minBackoff
	aligned := t.AlignStart.IsZero()
	wait := func(reason string, d time.Duration) bool {
		if d <= 0 {
			d = time.Second
		}
		s.live.set(name, reason, time.Now().Add(d))
		return sleepCtx(ctx, d)
	}
	for ctx.Err() == nil {
		s.live.set(name, "checking quota…", time.Time{})
		rctx, cancel := context.WithTimeout(ctx, readTimeout)
		u, err := t.Provider.ReadUsage(rctx)
		cancel()
		if err != nil {
			d := backoff
			var httpErr *provider.UsageHTTPError
			if errors.As(err, &httpErr) && !httpErr.RetryAfter.IsZero() {
				d = usageRateLimitWait(httpErr.RetryAfter, time.Now())
			} else if errors.As(err, &httpErr) && httpErr.StatusCode == 429 {
				d = rateLimitPause
			}
			s.log.Printf("[%s] quota read failed: %v (retry in %s)", name, err, d)
			if !wait("quota read failed", d) {
				return
			}
			backoff = nextBackoff(backoff)
			continue
		}
		backoff = minBackoff
		if s.redeemExpiringCredit(ctx, t, u) {
			continue
		}
		if s.weeklyExhausted(u) {
			d := u.Weekly.Remaining()
			if d > 5*time.Minute {
				d = 5 * time.Minute
			}
			if !wait("weekly limit reached", d) {
				return
			}
			continue
		}
		v := u.Verification
		if v == nil || v.Warning != "" {
			if v != nil {
				s.log.Printf("[%s] %s; automatic ping deferred", name, v.Warning)
			}
			if !wait("quota state unavailable", codexstate.Interval) {
				return
			}
			continue
		}
		if !aligned {
			aligned = true
			if d := time.Until(t.AlignStart); d > 0 {
				if !wait("waiting for align_start", d) {
					return
				}
				continue // re-read after alignment; user activity may have started it
			}
		}
		if v.Recovery == "window_started" {
			// Observe at rollover; the verification interval counts toward the buffer.
			d := time.Until(v.NextEligible)
			if d > 5*time.Minute {
				d = 5 * time.Minute
			}
			if !wait(v.Target+" started", d) {
				return
			}
			continue
		}
		if v.Recovery != "ready" || v.NextEligible.After(time.Now()) {
			d := time.Until(v.NextEligible)
			if d <= 0 || d > codexstate.Interval {
				d = codexstate.Interval
			}
			if !wait(v.Target+" "+v.Recovery, d) {
				return
			}
			continue
		}
		if !v.PreviousReset.IsZero() {
			if d := time.Until(v.PreviousReset.Add(s.cfg.ResetBuffer.Duration)); d > 0 {
				if d > 5*time.Minute {
					d = 5 * time.Minute
				}
				if !wait("waiting for reset_buffer", d) {
					return
				}
				continue // polling and verification count toward the same fixed deadline
			}
		}
		if desc, active, err := activeProviderTask(ctx, t.Provider); err != nil || active {
			if !wait(desc+" active or activity unavailable", activeTaskPoll) {
				return
			}
			continue
		}
		s.live.set(name, "checking and sending ping…", time.Time{})
		res, err := p.TriggerAutomatic(ctx, s.cfg.WeeklyThreshold, s.cfg.ResetBuffer.Duration)
		if errors.Is(err, codexstate.ErrBusy) || errors.Is(err, codexstate.ErrDeferred) {
			if !wait("ping deferred", codexstate.Interval) {
				return
			}
			continue
		}
		if err != nil && res == nil {
			// No trigger occurred: do not manufacture a failed ping-history entry.
			d := minBackoff
			var httpErr *provider.UsageHTTPError
			if errors.As(err, &httpErr) && (!httpErr.RetryAfter.IsZero() || httpErr.StatusCode == 429) {
				d = usageRateLimitWait(httpErr.RetryAfter, time.Now())
			}
			s.log.Printf("[%s] pre-ping check failed: %v; observing again in %s", name, err, d)
			if !wait("pre-ping check unavailable", d) {
				return
			}
			continue
		}
		if err != nil {
			s.log.Printf("[%s] ping failed: %v; verifying quota before retry", name, err)
			s.notify(name+": ping failed", "Verifying quota before another attempt")
		} else {
			s.log.Printf("[%s] ping trigger returned; checking window%s", name, triggerCost(res))
			s.notify(name+": CLI trigger returned", "Turn completion is unverified; checking quota separately")
		}
		if res != nil && res.Verification != nil && res.Verification.Warning != "" {
			s.log.Printf("[%s] %s", name, res.Verification.Warning)
		}
		if !wait("verifying quota", codexstate.Interval) {
			return
		}
	}
}
