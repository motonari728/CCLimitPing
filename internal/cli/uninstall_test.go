package cli

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestRemoveAliasOnlyRemovesOurOwnLink(t *testing.T) {
	newInstall := func(t *testing.T) (dir, exe string) {
		t.Helper()
		dir = t.TempDir()
		exe = filepath.Join(dir, "limitping")
		if err := os.WriteFile(exe, []byte("binary"), 0o755); err != nil {
			t.Fatal(err)
		}
		return dir, exe
	}

	t.Run("removes a link pointing at the binary", func(t *testing.T) {
		dir, exe := newInstall(t)
		alias := filepath.Join(dir, BinaryAlias)
		if err := os.Symlink("limitping", alias); err != nil {
			t.Fatal(err)
		}
		if err := removeAlias(exe, io.Discard); err != nil {
			t.Fatalf("removeAlias: %v", err)
		}
		if _, err := os.Lstat(alias); !os.IsNotExist(err) {
			t.Fatal("alias survived")
		}
	})

	t.Run("keeps an unrelated binary of the same name", func(t *testing.T) {
		dir, exe := newInstall(t)
		alias := filepath.Join(dir, BinaryAlias)
		if err := os.WriteFile(alias, []byte("someone else's lmp"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := removeAlias(exe, io.Discard); err != nil {
			t.Fatalf("removeAlias: %v", err)
		}
		if _, err := os.Stat(alias); err != nil {
			t.Fatalf("removeAlias deleted a real binary it does not own: %v", err)
		}
	})

	t.Run("keeps a link pointing somewhere else", func(t *testing.T) {
		dir, exe := newInstall(t)
		other := filepath.Join(dir, "other")
		if err := os.WriteFile(other, []byte("other"), 0o755); err != nil {
			t.Fatal(err)
		}
		alias := filepath.Join(dir, BinaryAlias)
		if err := os.Symlink("other", alias); err != nil {
			t.Fatal(err)
		}
		if err := removeAlias(exe, io.Discard); err != nil {
			t.Fatalf("removeAlias: %v", err)
		}
		if _, err := os.Lstat(alias); err != nil {
			t.Fatalf("removeAlias deleted a link it does not own: %v", err)
		}
	})

	t.Run("no alias installed", func(t *testing.T) {
		_, exe := newInstall(t)
		if err := removeAlias(exe, io.Discard); err != nil {
			t.Fatalf("removeAlias: %v", err)
		}
	})
}

// `upgrade` replaces the binary in place; without this the alias only ever came
// from install.sh, so upgrading into a version whose --help advertises `lmp`
// still left the user without the command.
func TestEnsureAliasCreatesAndKeepsOwnership(t *testing.T) {
	newInstall := func(t *testing.T) (dir, exe string) {
		t.Helper()
		dir = t.TempDir()
		exe = filepath.Join(dir, "limitping")
		if err := os.WriteFile(exe, []byte("binary"), 0o755); err != nil {
			t.Fatal(err)
		}
		// Keep the real PATH out of it: an `lmp` installed on this machine must
		// not decide the outcome of a test about a temp directory.
		t.Setenv("PATH", dir)
		return dir, exe
	}

	t.Run("creates the link when the name is free", func(t *testing.T) {
		dir, exe := newInstall(t)
		var out bytes.Buffer
		ensureAlias(exe, &out)

		target, err := os.Readlink(filepath.Join(dir, BinaryAlias))
		if err != nil {
			t.Fatalf("alias was not created: %v", err)
		}
		if target != "limitping" {
			t.Fatalf("alias points at %q, want limitping", target)
		}
		if !strings.Contains(out.String(), BinaryAlias) {
			t.Fatalf("creation was not reported: %q", out.String())
		}
	})

	t.Run("is idempotent across repeated upgrades", func(t *testing.T) {
		dir, exe := newInstall(t)
		ensureAlias(exe, io.Discard)
		var out bytes.Buffer
		ensureAlias(exe, &out)

		if _, err := os.Readlink(filepath.Join(dir, BinaryAlias)); err != nil {
			t.Fatalf("alias lost on the second run: %v", err)
		}
		if out.Len() != 0 {
			t.Fatalf("re-announced an alias it already owned: %q", out.String())
		}
	})

	t.Run("keeps an unrelated binary of the same name", func(t *testing.T) {
		dir, exe := newInstall(t)
		alias := filepath.Join(dir, BinaryAlias)
		if err := os.WriteFile(alias, []byte("someone else's lmp"), 0o755); err != nil {
			t.Fatal(err)
		}
		var out bytes.Buffer
		ensureAlias(exe, &out)

		data, err := os.ReadFile(alias)
		if err != nil || string(data) != "someone else's lmp" {
			t.Fatalf("ensureAlias overwrote a real binary: %v %q", err, data)
		}
		if !strings.Contains(out.String(), "skipped") {
			t.Fatalf("collision was not reported: %q", out.String())
		}
	})

	t.Run("keeps a link pointing somewhere else", func(t *testing.T) {
		dir, exe := newInstall(t)
		other := filepath.Join(dir, "other")
		if err := os.WriteFile(other, []byte("other"), 0o755); err != nil {
			t.Fatal(err)
		}
		alias := filepath.Join(dir, BinaryAlias)
		if err := os.Symlink("other", alias); err != nil {
			t.Fatal(err)
		}
		ensureAlias(exe, io.Discard)

		if target, _ := os.Readlink(alias); target != "other" {
			t.Fatalf("alias now points at %q, want the link left alone", target)
		}
	})

	t.Run("refuses when the name is already a command on PATH", func(t *testing.T) {
		dir, exe := newInstall(t)
		elsewhere := t.TempDir()
		if err := os.WriteFile(filepath.Join(elsewhere, BinaryAlias), []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		t.Setenv("PATH", elsewhere+string(os.PathListSeparator)+dir)

		var out bytes.Buffer
		ensureAlias(exe, &out)

		if _, err := os.Lstat(filepath.Join(dir, BinaryAlias)); !os.IsNotExist(err) {
			t.Fatal("shadowed a command that already exists on PATH")
		}
		if !strings.Contains(out.String(), "already runs") {
			t.Fatalf("shadowing was not reported: %q", out.String())
		}
	})
}

// The alias repair hangs off `version` because that is the only entry point an
// older `upgrade` gives a new build: every released runUpgrade ends by running
// `<new binary> version`. Break this wiring and upgrading from a version that
// predates the alias silently produces a binary that advertises `lmp` without
// installing one — which is exactly what shipped in v1.0.0. Built and run as a
// subprocess because the repair keys off os.Executable.
func TestVersionCommandRepairsTheAliasForOlderUpgraders(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a binary")
	}
	dir := t.TempDir()
	exe := filepath.Join(dir, "limitping")

	build := exec.Command("go", "build", "-o", exe, "github.com/wavever/CCLimitPing/cmd/limitping")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}

	// PATH holds only dir, so the alias lookup cannot see a real lmp installed
	// on the machine running the tests.
	run := exec.Command(exe, "version")
	run.Env = append(os.Environ(), "PATH="+dir)
	out, err := run.CombinedOutput()
	if err != nil {
		t.Fatalf("version: %v\n%s", err, out)
	}

	target, err := os.Readlink(filepath.Join(dir, BinaryAlias))
	if err != nil {
		t.Fatalf("`version` did not install the alias: %v\noutput: %s", err, out)
	}
	if target != "limitping" {
		t.Fatalf("alias points at %q, want limitping", target)
	}
	if !strings.HasPrefix(string(out), "limitping ") {
		t.Fatalf("version output no longer starts with the version line: %q", out)
	}
}
