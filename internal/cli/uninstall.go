package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/wavever/CCLimitPing/internal/config"
)

func newUninstallCmd() *cobra.Command {
	var keepConfig bool
	text := localizedText()
	cmd := &cobra.Command{
		Use:     "uninstall",
		Aliases: []string{"rm", "remove"},
		Short:   text.uninstallShort,
		Long:    text.uninstallLong,
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runUninstall(cmd.OutOrStdout(), cmd.ErrOrStderr(), keepConfig)
		},
	}
	cmd.Flags().BoolVar(&keepConfig, "keep-config", false, text.uninstallKeepConfig)
	return cmd
}

func runUninstall(out, errOut io.Writer, keepConfig bool) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locating current executable: %w", err)
	}
	// Invoked through the `lmp` alias, os.Executable can be the symlink itself;
	// removing that would leave the real binary installed.
	exe = resolveLinks(exe)

	// Strip our hook entries from the CLI configs first, so we don't leave hooks
	// pointing at a binary we're about to delete. Best-effort: never abort.
	removeHooksBestEffort(errOut)

	if err := removeAlias(exe, out); err != nil {
		fmt.Fprintf(errOut, "Could not remove the %s alias: %v\n", BinaryAlias, err)
	}
	if err := removeExecutable(exe, out, errOut); err != nil {
		return err
	}
	fmt.Fprintf(out, "Removed %s\n", exe)

	if keepConfig {
		fmt.Fprintln(out, "Config/cache preserved.")
		return nil
	}

	dir, err := config.Dir()
	if err != nil {
		return fmt.Errorf("locating config dir: %w", err)
	}
	removed, err := removeConfigDir(dir)
	if err != nil {
		return err
	}
	if removed {
		fmt.Fprintf(out, "Removed %s\n", dir)
	} else {
		fmt.Fprintf(out, "No config/cache dir found at %s\n", dir)
	}
	return nil
}

// removeAlias deletes the `lmp` symlink installed next to exe. It only removes
// a link that actually points at exe, so it can never take out an unrelated
// binary that happens to share the name.
func removeAlias(exe string, out io.Writer) error {
	alias := filepath.Join(filepath.Dir(exe), BinaryAlias)
	target, err := os.Readlink(alias)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		// Not a symlink (or unreadable): leave it alone.
		return nil
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(alias), target)
	}
	// Resolve both sides: either path can run through a symlinked parent
	// directory (/var -> /private/var on macOS), which is not a mismatch.
	if resolveLinks(target) != resolveLinks(exe) {
		return nil
	}
	if err := os.Remove(alias); err != nil {
		return err
	}
	fmt.Fprintf(out, "Removed %s\n", alias)
	return nil
}

// ensureAlias puts the short alias next to exe, pointing at it. `upgrade`
// replaces the binary in place and used to leave the alias entirely to
// install.sh, so upgrading into a version that advertises `lmp` in --help still
// left the user without the command.
//
// It applies the same two rules install.sh does. The path must be free or
// already our own link, and the name must not already resolve to another
// command on PATH — the alias lands in a directory that normally precedes
// /usr/bin, so claiming a taken name would shadow it. Every refusal is silent
// except a genuine collision, and none of them fail the upgrade: the binary is
// already in place by then.
func ensureAlias(exe string, out io.Writer) {
	alias := filepath.Join(filepath.Dir(exe), BinaryAlias)

	switch target, err := os.Readlink(alias); {
	case err == nil:
		if !filepath.IsAbs(target) {
			target = filepath.Join(filepath.Dir(alias), target)
		}
		if resolveLinks(target) != resolveLinks(exe) {
			fmt.Fprintf(out, "NOTE: %s points elsewhere; leaving it alone.\n", alias)
		}
		return // ours already, or someone else's to keep
	case !errors.Is(err, os.ErrNotExist):
		fmt.Fprintf(out, "NOTE: %s already exists and is not our symlink; skipped the short alias.\n", alias)
		return
	}

	if existing, err := exec.LookPath(BinaryAlias); err == nil && resolveLinks(existing) != resolveLinks(exe) {
		fmt.Fprintf(out, "NOTE: %q already runs %s; skipped the short alias.\n", BinaryAlias, existing)
		return
	}
	if err := os.Symlink(filepath.Base(exe), alias); err != nil {
		return // best effort; the upgrade itself succeeded
	}
	fmt.Fprintf(out, "Installed %s -> %s\n", BinaryAlias, exe)
}

// resolveLinks returns path with symlinks resolved, or path unchanged when it
// cannot be resolved (e.g. it no longer exists).
func resolveLinks(path string) string {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved
	}
	return path
}

func removeExecutable(path string, out, errOut io.Writer) error {
	if err := os.Remove(path); err == nil {
		return nil
	} else if errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("installed binary not found at %s", path)
	}

	if _, err := exec.LookPath("sudo"); err != nil {
		return fmt.Errorf("removing %s: permission denied; retry with sudo", path)
	}
	fmt.Fprintf(out, "Cannot remove %s without elevated permissions; retrying with sudo.\n", path)
	cmd := exec.Command("sudo", "rm", "-f", path)
	cmd.Stdin = os.Stdin
	cmd.Stdout = out
	cmd.Stderr = errOut
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("sudo remove %s: %w", path, err)
	}
	return nil
}

func removeConfigDir(path string) (bool, error) {
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return false, nil
	} else if err != nil {
		return false, fmt.Errorf("checking config/cache dir %s: %w", path, err)
	}
	if err := os.RemoveAll(path); err != nil {
		return false, fmt.Errorf("removing config/cache dir %s: %w", path, err)
	}
	return true, nil
}
