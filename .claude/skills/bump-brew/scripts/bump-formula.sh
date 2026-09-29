#!/usr/bin/env bash
#
# bump-formula.sh — bump Homebrew formula for ting in binlecode/homebrew-ting,
# commit/push the tap, and perform local upgrade + test validation.
#
# Usage: .claude/skills/bump-brew/scripts/bump-formula.sh [version]
#
set -euo pipefail

if ! ROOT="$(git rev-parse --show-toplevel 2>/dev/null)"; then
  ROOT="$(cd "$(dirname "$0")/../../../.." && pwd)"
fi
cd "$ROOT"

VERSION="${1:-$(cat VERSION 2>/dev/null | tr -d '[:space:]')}"
VERSION="${VERSION#v}"
if [ -z "$VERSION" ]; then
  echo "FAIL: Version could not be determined from argument or VERSION file." >&2
  exit 1
fi

TAG="v${VERSION}"
TARBALL_URL="https://github.com/binlecode/ting/archive/refs/tags/${TAG}.tar.gz"

echo "=== 1. Preflight: Checking Tag on Remote ==="
echo "Target version: ${VERSION} (${TAG})"
if ! git ls-remote --tags origin "refs/tags/${TAG}" | grep -q "${TAG}"; then
  echo "FAIL: Tag ${TAG} does not exist on remote origin (GitHub)." >&2
  echo "      Push the tag first: git push origin ${TAG}" >&2
  exit 1
fi
echo "PASS: Tag ${TAG} exists on origin."

echo
echo "=== 2. Check for Active Running Playback ==="
if pgrep -fl 'Cellar/ting/' >/dev/null 2>&1; then
  echo "WARN: An active ting session is currently running from Cellar/ting/."
  echo "      Upgrading Homebrew will delete the old Cellar and may disrupt playback."
  echo "      Consider stopping playback or exiting ting first."
else
  echo "PASS: No running ting processes detected in Cellar."
fi

echo
echo "=== 3. Download Archive & Compute SHA-256 ==="
echo "Fetching: ${TARBALL_URL}"
TMP_TARBALL="$(mktemp "${TMPDIR:-/tmp}/ting-tarball.XXXXXX.tar.gz")"
trap 'rm -f "$TMP_TARBALL"' EXIT

curl -fsSL "$TARBALL_URL" -o "$TMP_TARBALL"
SHA256="$(shasum -a 256 "$TMP_TARBALL" | awk '{print $1}')"

if [ -z "$SHA256" ] || [ "${#SHA256}" -ne 64 ]; then
  echo "FAIL: Failed to compute valid SHA-256 for tarball." >&2
  exit 1
fi
echo "Tarball SHA-256: ${SHA256}"

echo
echo "=== 4. Locate and Update Tap Repository ==="
# Check and clean up legacy tap if co-existing locally
if brew tap 2>/dev/null | grep -qx "binlecode/ting"; then
  echo "Untapping legacy 'binlecode/ting' to prevent duplicate formula ambiguity..."
  brew untap binlecode/ting 2>/dev/null || true
fi

TAP_DIR="$(brew --repository binlecode/tap 2>/dev/null || true)"
if [ -z "$TAP_DIR" ] || [ ! -d "$TAP_DIR" ]; then
  TAP_DIR="/opt/homebrew/Library/Taps/binlecode/homebrew-tap"
  if [ ! -d "$TAP_DIR" ]; then
    echo "Cloning tap binlecode/tap..."
    git clone git@github.com-binlecode:binlecode/homebrew-tap.git "$TAP_DIR"
  fi
fi

FORMULA_FILE="${TAP_DIR}/Formula/ting.rb"
if [ ! -f "$FORMULA_FILE" ]; then
  echo "FAIL: Formula file not found at ${FORMULA_FILE}" >&2
  exit 1
fi

echo "Updating ${FORMULA_FILE}..."
# Replace url line (matching any scheme: https, file, etc.)
sed -i '' -E "s|url \".*\"|url \"${TARBALL_URL}\"|g" "$FORMULA_FILE"
# Replace sha256 line
sed -i '' -E "s|sha256 \"[a-f0-9]{64}\"|sha256 \"${SHA256}\"|g" "$FORMULA_FILE"

echo "--- Formula Diff ---"
git -C "$TAP_DIR" diff Formula/ting.rb || true

if git -C "$TAP_DIR" diff --quiet Formula/ting.rb; then
  echo "INFO: Formula is already up to date for version ${VERSION}."
else
  echo
  echo "=== 5. Commit and Push Tap ==="
  git -C "$TAP_DIR" add Formula/ting.rb
  git -C "$TAP_DIR" commit -m "ting ${VERSION}"
  git -C "$TAP_DIR" push origin main

  # Sync developer workspace checkout if present (dynamically located at ../homebrew-tap)
  WORKSPACE_TAP="$(cd "$ROOT/.." && pwd)/homebrew-tap"
  if [ -d "$WORKSPACE_TAP" ] && [ "$WORKSPACE_TAP" != "$TAP_DIR" ]; then
    echo "Syncing workspace clone at ${WORKSPACE_TAP}..."
    git -C "$WORKSPACE_TAP" pull --ff-only 2>/dev/null || true
  fi
  echo "PASS: Tap repository updated and pushed to GitHub."
fi

echo
echo "=== 6. Local Brew Upgrade & Contract Test ==="
echo "Upgrading ting..."
brew upgrade binlecode/tap/ting 2>/dev/null || brew reinstall binlecode/tap/ting

echo "Running brew test..."
brew test binlecode/tap/ting

INSTALLED_VER="$(ting --version 2>/dev/null || echo "unknown")"
echo
echo "========================================================"
echo "✨ Homebrew Formula Release Complete!"
echo "   Installed: ${INSTALLED_VER}"
echo "   Tap Formula: binlecode/tap/ting (${VERSION})"
echo "========================================================"
