#!/bin/sh
# limitping installer — downloads the right prebuilt binary from the latest
# GitHub release. No Go required.
#
#   curl -fsSL https://raw.githubusercontent.com/wavever/CCLimitPing/main/install.sh | sh
#
# Override the install directory with LIMITPING_INSTALL_DIR=/path sh install.sh
set -eu

REPO="wavever/CCLimitPing"
BIN="limitping"
ALIAS="lmp" # short name, symlinked next to the binary

os=$(uname -s | tr '[:upper:]' '[:lower:]')
arch=$(uname -m)
case "$arch" in
  x86_64 | amd64) arch="amd64" ;;
  arm64 | aarch64) arch="arm64" ;;
  *) echo "limitping: unsupported architecture: $arch" >&2; exit 1 ;;
esac
case "$os" in
  darwin | linux) ;;
  *) echo "limitping: unsupported OS: $os (build from source: go build ./cmd/limitping)" >&2; exit 1 ;;
esac

asset="${BIN}_${os}_${arch}.tar.gz"
url="https://github.com/${REPO}/releases/latest/download/${asset}"

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

echo "Downloading ${url}"
if command -v curl >/dev/null 2>&1; then
  curl -fsSL "$url" -o "$tmp/$asset"
elif command -v wget >/dev/null 2>&1; then
  wget -qO "$tmp/$asset" "$url"
else
  echo "limitping: need curl or wget" >&2; exit 1
fi

tar -xzf "$tmp/$asset" -C "$tmp"

if [ -n "${LIMITPING_INSTALL_DIR:-}" ]; then
  dir="$LIMITPING_INSTALL_DIR"
elif [ -w /usr/local/bin ]; then
  dir="/usr/local/bin"
else
  dir="$HOME/.local/bin"
fi
mkdir -p "$dir"

if cp "$tmp/$BIN" "$dir/$BIN" 2>/dev/null; then
  chmod 0755 "$dir/$BIN"
else
  echo "limitping: cannot write to $dir; retrying with sudo"
  sudo cp "$tmp/$BIN" "$dir/$BIN"
  sudo chmod 0755 "$dir/$BIN"
fi

echo "Installed $BIN -> $dir/$BIN"

# Short alias: `lmp` behaves exactly like `limitping`. Two guards before we
# claim the name, because $dir usually precedes /usr/bin on PATH, so a symlink
# here silently shadows whatever it collides with. That is why the alias is not
# `lp`: that is CUPS's printing command, present on macOS and most Linux
# distributions, and shadowing it would break printing rather than anything
# obviously limitping's fault.
alias_path="$dir/$ALIAS"
alias_skip=""

# 1. The path itself must be free, or already our own link. `uninstall` applies
#    the same ownership test before removing.
if [ -e "$alias_path" ] || [ -L "$alias_path" ]; then
  alias_skip="$alias_path already exists and is not our symlink"
  if [ -L "$alias_path" ]; then
    alias_target=$(readlink "$alias_path")
    case "$alias_target" in
      /*) ;;
      *) alias_target="$dir/$alias_target" ;;
    esac
    if [ "$alias_target" = "$dir/$BIN" ]; then
      alias_skip=""
    fi
  fi
fi

# 2. And the name must not already resolve to some other command on PATH.
if [ -z "$alias_skip" ]; then
  alias_existing=$(command -v "$ALIAS" 2>/dev/null || true)
  if [ -n "$alias_existing" ] && [ "$alias_existing" != "$alias_path" ]; then
    alias_skip="'$ALIAS' already runs $alias_existing"
  fi
fi

if [ -n "$alias_skip" ]; then
  echo "NOTE: $alias_skip; skipped the short alias. Use the full \`$BIN\` name."
elif ln -sf "$BIN" "$alias_path" 2>/dev/null || sudo ln -sf "$BIN" "$alias_path" 2>/dev/null; then
  echo "Installed $ALIAS -> $dir/$BIN (short alias)"
else
  echo "NOTE: could not create the $ALIAS symlink in $dir; use the full \`$BIN\` name,"
  echo "      or link it yourself: ln -s $BIN $alias_path"
fi
case ":$PATH:" in
  *":$dir:"*) ;;
  *)
    echo
    echo "NOTE: $dir is not on your PATH. Add it, e.g.:"
    echo "  export PATH=\"$dir:\$PATH\""
    ;;
esac

# Install active-session detection hooks for whichever provider CLIs are set up.
# limitping only defers a ping while you're mid-turn when these hooks are present;
# without them it pings as soon as the window resets, without checking.
hooked=""
for p in claude codex; do
  if [ -d "$HOME/.$p" ] && "$dir/$BIN" hooks install "$p" >/dev/null 2>&1; then
    hooked="${hooked:+$hooked, }$p"
  fi
done
if [ -n "$hooked" ]; then
  echo
  echo "Installed active-session hooks for: $hooked"
  case "$hooked" in
    *codex*) echo "  Codex needs a one-time trust: run /hooks inside Codex. (Claude loads automatically.)" ;;
  esac
else
  echo
  echo "NOTE: no Claude/Codex config found yet — run 'limitping hooks install'"
  echo "  after setting them up to enable active-session detection."
fi

"$dir/$BIN" version 2>/dev/null || true
