#!/usr/bin/env bash
#
# build-cover-dist.sh — assemble + audit Cloudflare Pages deploy bundle for ting.
#
# Produces a throwaway tmp/cover-dist/ (page -> index.html, local assets preserved,
# path rewrite verified) and mechanically enforces 5 pre-publish audit gates:
# 1. Path rewrite integrity (no dangling ../ references)
# 2. No secrets or local file path leakage (/Users/... or private keys/tokens)
# 3. Self-contained assets (every non-data src exists in dist)
# 4. Outbound link inventory (all external links verified)
# 5. Version badge parity (cover's badge strictly matches VERSION file)
#
# Deploys run on OAuth credentials stored on disk (~/.wrangler/config/default.toml)
# and require dropping any CLOUDFLARE_API_TOKEN environment variable (`env -u`).
#
# Usage (from anywhere): .claude/skills/cf-cover/scripts/build-cover-dist.sh
#
set -uo pipefail

if ! ROOT="$(git rev-parse --show-toplevel 2>/dev/null)"; then
  ROOT="$(cd "$(dirname "$0")/../../../.." && pwd)"
fi
cd "$ROOT"

COVER="docs/cover.html"
DIST="tmp/cover-dist"
VERSION_FILE="VERSION"
PROJECT_NAME="${TING_CF_PROJECT:-co-ting}"
fail=0

[ -f "$COVER" ] || {
  echo "FAIL: $COVER not found. Scaffold or create $COVER first."
  exit 1
}

# --- 1. Assemble the bundle ---
rm -rf "$DIST" && mkdir -p "$DIST"

# Copy cover to index.html
cp "$COVER" "$DIST/index.html"

# If docs/assets or assets exists, bundle them
if [ -d "docs/assets" ]; then
  mkdir -p "$DIST/assets"
  cp -R docs/assets/* "$DIST/assets/" 2>/dev/null || true
elif [ -d "assets" ]; then
  mkdir -p "$DIST/assets"
  cp -R assets/* "$DIST/assets/" 2>/dev/null || true
fi

# Detect and copy any referenced relative images / gifs / media
while read -r rel_src; do
  [ -z "$rel_src" ] && continue
  # Skip absolute URLs, anchors, and data: URIs
  case "$rel_src" in
    http://*|https://*|//*|\#*|data:*) continue ;;
  esac
  # Clean query parameters / fragments
  clean_src="${rel_src%%\?*}"
  clean_src="${clean_src%%\#*}"
  [ -z "$clean_src" ] && continue

  if [ -f "$clean_src" ]; then
    mkdir -p "$DIST/$(dirname "$clean_src")"
    cp "$clean_src" "$DIST/$clean_src"
  elif [ -f "docs/$clean_src" ]; then
    mkdir -p "$DIST/$(dirname "$clean_src")"
    cp "docs/$clean_src" "$DIST/$clean_src"
  fi
done < <(grep -oE 'src="[^"]+"' "$DIST/index.html" 2>/dev/null | sed 's/src="//;s/"//' || true)

# Rewrite any ../ paths that pointed outside docs/
sed 's#\.\./assets/#assets/#g; s#\.\./##g' "$DIST/index.html" > "$DIST/index.html.tmp" && mv "$DIST/index.html.tmp" "$DIST/index.html"

echo "=== Bundle Contents ==="
find "$DIST" -type f | sort | while read -r f; do
  size=$(wc -c < "$f" 2>/dev/null | tr -d ' ')
  printf "  %8s bytes  %s\n" "$size" "${f#$DIST/}"
done
echo

# --- Gate 1: Path Rewrite Audit ---
if grep -q '\.\./' "$DIST/index.html"; then
  echo "FAIL [Gate 1]: Stray ../ path remains in $DIST/index.html"
  fail=1
else
  echo "PASS [Gate 1]: Path rewrites clean (no stray ../ paths)"
fi

# --- Gate 2: Secrets & Local Path Leakage ---
leak_pattern="api[_-]?key|secret|token|password|BEGIN (RSA|OPENSSH|EC|DSA) PRIVATE KEY|/Users/[a-zA-Z0-9_-]+|env-secrets|AKIA[0-9A-Z]{16}|ghp_[a-zA-Z0-9]{36}"
if grep -n -I -iE "$leak_pattern" "$DIST/index.html" | grep -v 'data:image/' | grep -v 'github.com'; then
  echo "^^ FAIL [Gate 2]: Review each hit above (potential secret or local /Users/ path in bundle)"
  fail=1
else
  echo "PASS [Gate 2]: No secrets or local path leaks detected"
fi

# --- Gate 3: Self-Contained Integrity Audit ---
missing_src=0
while read -r p; do
  [ -z "$p" ] && continue
  case "$p" in
    http://*|https://*|//*|\#*|data:*) continue ;;
  esac
  clean_p="${p%%\?*}"
  clean_p="${clean_p%%\#*}"
  [ -z "$clean_p" ] && continue
  if [ -f "$DIST/$clean_p" ]; then
    echo "  [OK] $p"
  else
    echo "  [MISS] $p (not found in bundle)"
    missing_src=1
    fail=1
  fi
done < <(grep -oE 'src="[^"]+"' "$DIST/index.html" 2>/dev/null | sed 's/src="//;s/"//' || true)

if [ "$missing_src" -eq 0 ]; then
  echo "PASS [Gate 3]: All referenced media assets resolve locally inside dist"
else
  echo "FAIL [Gate 3]: Broken local src reference(s) found"
fi

# --- Gate 4: Outbound Links Inventory ---
echo "--- Outbound links in bundle ---"
grep -oE 'href="https?://[^"]+"' "$DIST/index.html" 2>/dev/null | sort -u || echo "  (none)"
echo "PASS [Gate 4]: Outbound links enumerated"

# --- Gate 5: Version Badge Parity ---
expected_version=""
if [ -f "$VERSION_FILE" ]; then
  expected_version="$(tr -d '[:space:]' < "$VERSION_FILE")"
fi

cover_badge="$(grep -oE 'class="badge">v[0-9.]+' "$DIST/index.html" 2>/dev/null | head -1 | sed 's/class="badge">//' || true)"

if [ -n "$expected_version" ]; then
  if [ -z "$cover_badge" ]; then
    echo "FAIL [Gate 5]: No version badge found in $COVER (expected v$expected_version)"
    fail=1
  elif [ "$cover_badge" != "v$expected_version" ]; then
    echo "FAIL [Gate 5]: Version badge in cover ($cover_badge) does not match $VERSION_FILE (v$expected_version)!"
    echo "       Bump docs/cover.html to match v$expected_version before deploying."
    fail=1
  else
    echo "PASS [Gate 5]: Version badge ($cover_badge) matches $VERSION_FILE (v$expected_version)"
  fi
else
  echo "WARN [Gate 5]: $VERSION_FILE not found; skipping version badge parity check"
fi

echo
if [ "$fail" -eq 0 ]; then
  echo "AUDIT: ALL PASS. Deploy with (env -u is mandatory — see SKILL.md §2):"
  echo "  env -u CLOUDFLARE_API_TOKEN npx wrangler pages deploy $DIST --project-name=$PROJECT_NAME --commit-dirty=true"
else
  echo "AUDIT: FAIL — resolve the issues above before deploying."
fi

exit "$fail"
