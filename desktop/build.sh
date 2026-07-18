#!/usr/bin/env bash
set -euo pipefail

# Wraps `wails build` with a config sync step: if a config.yaml exists at
# the repo root, it's copied into the fixed OS user-config location the
# app actually reads from (see README's Configuration section and
# internal/appapi.DefaultConfigPath). The app itself never reads the
# repo-root file directly -- only this script does the copying, and only
# at build time, so editing config.yaml here has no effect until the next
# build. The repo-root file is a convenience for keeping rules/categories
# under version control (its machine-specific absolute paths keep it out
# of git via .gitignore); it's optional, so this step is skipped if it's
# absent.
#
# Usage: same as `wails build`. Defaults to -tags webkit2_41 when no
# arguments are given; pass your own args to override, e.g. ./build.sh -clean

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
REPO_CONFIG="$REPO_ROOT/config.yaml"
REAL_CONFIG_DIR="${XDG_CONFIG_HOME:-$HOME/.config}/book-organiser"
REAL_CONFIG="$REAL_CONFIG_DIR/config.yaml"

if [ -f "$REPO_CONFIG" ]; then
  mkdir -p "$REAL_CONFIG_DIR"
  if [ -f "$REAL_CONFIG" ]; then
    cp "$REAL_CONFIG" "$REAL_CONFIG.bak"
  fi
  cp "$REPO_CONFIG" "$REAL_CONFIG"
  echo "Synced $REPO_CONFIG -> $REAL_CONFIG (previous version backed up to $REAL_CONFIG.bak)"
else
  echo "No repo-root config.yaml found at $REPO_CONFIG -- skipping config sync"
fi

cd "$SCRIPT_DIR"
if [ "$#" -eq 0 ]; then
  set -- -tags webkit2_41
fi
wails build "$@"
