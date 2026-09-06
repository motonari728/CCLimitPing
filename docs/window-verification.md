# Codex and Spark window verification

Codex and Spark share an observation algorithm, not quota buckets. Normal Codex
uses `rate_limit`; Spark selects its model from `additional_rate_limits`.
Claude's existing window detection and scheduling are unchanged.

## Interactive commands

Every real Codex/Spark ping attempts the read-only quota API before and after the
CLI turn. Each read has a three-second total timeout including retries. This
does not consume model quota. These reads do not fetch reset-credit details.
The command does not wait one minute or start a detached verification process.
`ping all` continues to the next provider after the ordinary trigger and these
bounded reads. Explicit `schedule` commands use the same ping behavior.

CLI completion and window start are separate results. A turn-completion OSC 9
notification confirms completion; a timeout, nonzero exit or clean exit without
that notification is an error. A confirmed completed turn returns exit zero even
if the API or state file is unavailable or the window is unconfirmed. A failed
pre-read does not prevent an explicit manual ping. Cancellation returns promptly
without requiring a post-read.

An unconfirmed result with a saved baseline suggests `limitping status` after
60 seconds. If no baseline could be saved, the first successful status read
collects it; another observation at least 60 seconds later is needed. Already
confirmed windows need no recheck instruction. Status only reads quota and
observes state; it never sends a model request or waits for the comparison interval.

`status` still reads enabled providers only and accepts no provider selector.
An explicitly pinged disabled provider therefore warns that it will not appear
in status. Other enabled providers retain their existing read timeouts.

`status --json` retains the legacy `active` boolean (positive usage with a future
reset). Codex/Spark windows additionally expose `start_state`: `started`,
`not_started` or `unknown`, and `verification_due_at` when another sample is
needed. At 0% usage, `active: false` can coexist with `start_state: "started"`.
No new fields are emitted for Claude. Cache failures leave usable quota data
visible and the start state unknown; API failures retain the existing nonzero
status exit behavior. Human output labels unconfirmed reset times as estimates.

## Observation rules

Positive usage with a plausible future reset supports a started window. At zero
usage, two compatible samples at least 60 seconds apart can distinguish a fixed
absolute reset from a reset sliding with observation time. A fixed reset supports
started, including when the second observation is hours later. Sliding resets
require a 60–600 second pair and approximately match observation time plus the
reported duration. Timestamp tolerance is five seconds. A fresh pre-send read
can extend recently established sliding evidence; it cannot rely only on old
cached eligibility.

These are empirical rules, not a documented server-side guarantee. Inconsistent
or missing evidence remains unknown. Account/plan/duration changes, reset
boundaries, backwards observation time and reset-credit redemption invalidate
incompatible evidence. A pre-ping/post-ping pair alone cannot establish failure:
each attempt starts a new post-attempt failure baseline. Compatible previously
confirmed start evidence is retained across a manual ping.

## Automatic recovery

Foreground watch and background watch schedule observation-only checks while
start state is uncertain. The target is the five-hour window when present,
otherwise the weekly window. An active weekly window does not cancel recovery
of an unstarted five-hour window. One ping observes both windows in its own
bucket, never the other provider's bucket.

Automatic sends require fresh not-started evidence, usable state storage and
the existing alignment, activity and weekly/credit guards. After a send,
observation failure causes further reads, not immediate model retries. Confirmed
failure permits retries after at least 1, 5 and then 15 minutes. At most four
automatic attempts are allowed in a rolling hour per account and bucket.
The budget persists across restarts and target changes. Cooldown expires
automatically; fresh verification and all guards are still required to send.
Manual pings bypass that budget without clearing it.

Background status exposes the target, verification/backoff/cooldown state and
next eligibility time, not a guarantee that a ping will occur then. Ping history
counts each trigger outcome once; later verification events are not extra pings.

## State and platforms

State lives in `<config.Dir()>/state/codex/<account-hash>.json`, with separate
normal-Codex and Spark-model entries. The directory is `$XDG_CONFIG_HOME/limitping`
when set, otherwise the user's home `.config/limitping` on all platforms.
It contains observations, deadlines and attempt metadata, not tokens, raw API
responses, prompts or model output. New directories/files use private permissions
where supported. Same-directory replacement and short OS-backed file locks
protect updates; locks are not held across network or model requests.

A live per-bucket attempt claim makes a competing manual ping return promptly
with an actionable error. Pending verification alone does not block manual use.
An expired claim requires new observation evidence before automatic retry.
Missing state starts with unknown evidence. Corrupt/incompatible state or an
unwritable directory disables automatic sends; explicit manual pings can proceed
with a warning, without promising duplicate prevention during storage failure.
After repairing permissions or moving a corrupt state file aside, observations
resume. Do not remove healthy state to bypass retry budgets.

The state implementation supports Linux, macOS and Windows. Native Windows
Codex/Spark PTY triggering remains unsupported by the current PTY dependency;
Windows release targets do not imply working TUI pings. WSL with Linux binaries
uses the Linux PTY implementation. No Windows PTY replacement is part of this change.

`ping --dry-run` neither fetches quota nor writes state. Watch dry-run retains
its existing quota reads but does not persist verification or attempt state.
Offline tests cover observations, claims, budgets, CLI output and fake PTY
completion; no real quota pings are necessary for these tests.
