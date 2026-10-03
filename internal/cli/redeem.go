package cli

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/spf13/cobra"

	"github.com/wavever/CCLimitPing/internal/config"
	"github.com/wavever/CCLimitPing/internal/provider"
	"github.com/wavever/CCLimitPing/internal/usage"
)

// newRedeemCmd spends a banked Codex reset credit. It is a separate, explicit
// command because redeeming is irreversible: `status` only ever reports the
// credits, and the automatic path (auto_redeem) is opt-in.
func newRedeemCmd() *cobra.Command {
	var dryRun bool
	text := localizedText()
	cmd := &cobra.Command{
		Use:     "redeem",
		Aliases: []string{"r"},
		Short:   text.redeemShort,
		Long:    text.redeemLong,
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			return runRedeem(cmd.Context(), cmd.OutOrStdout(), text, provider.NewCodex(cfg.Codex), dryRun)
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, text.redeemDryRunFlag)
	return cmd
}

func runRedeem(ctx context.Context, out io.Writer, text cliText, p *provider.Codex, dryRun bool) error {
	readCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	u, err := p.ReadUsage(readCtx)
	cancel()
	if err != nil {
		return err
	}
	credit, ok := nextRedeemableCredit(u, time.Now())
	if !ok {
		return fmt.Errorf("%s", text.redeemNoneAvailable)
	}

	// The expiry is unknown when only the count survived (see below).
	if !credit.ExpiresAt.IsZero() {
		expires := credit.ExpiresAt.Local()
		fmt.Fprintf(out, text.redeemPlanFmt,
			expires.Format(text.statusCreditTimeLayout)+" "+fmtZone(expires),
			fmtDurDays(text, time.Until(credit.ExpiresAt)))
	}
	if dryRun {
		fmt.Fprint(out, text.redeemDryRunNote)
		return nil
	}

	outcome, err := p.RedeemResetCredit(ctx)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, text.redeemOutcomeFmt, redeemOutcomeText(text, outcome))
	return nil
}

// nextRedeemableCredit returns the soonest-expiring credit that is still
// spendable, ignoring the auto-redeem timing policy: an explicit `redeem` is
// the user asking for it now.
func nextRedeemableCredit(u *usage.Usage, now time.Time) (usage.ResetCredit, bool) {
	if u.ResetCredits == nil {
		return usage.ResetCredit{}, false
	}
	var target usage.ResetCredit
	found := false
	for _, c := range u.ResetCredits.Credits {
		if !c.Redeemable(now) {
			continue
		}
		if !found || c.ExpiresAt.Before(target.ExpiresAt) {
			target, found = c, true
		}
	}
	// The detail endpoint is private and may go away, leaving only the count
	// from the usage response; trust that rather than refusing to redeem.
	if !found && u.ResetCredits.AvailableCount > 0 && len(u.ResetCredits.Credits) == 0 {
		return usage.ResetCredit{}, true
	}
	return target, found
}

func redeemOutcomeText(text cliText, outcome string) string {
	switch outcome {
	case provider.RedeemReset:
		return text.redeemDone
	case provider.RedeemNothingToReset:
		return text.redeemNothing
	case provider.RedeemNoCredit:
		return text.redeemNoCredit
	case provider.RedeemAlreadyRedeemed:
		return text.redeemAlready
	default:
		return fmt.Sprintf(text.redeemUnknownFmt, outcome)
	}
}
