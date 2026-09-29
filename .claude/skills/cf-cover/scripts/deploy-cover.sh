#!/usr/bin/env bash
#
# deploy-cover.sh — assemble, audit, and deploy Cloudflare Pages for ting.
#
# Usage: .claude/skills/cf-cover/scripts/deploy-cover.sh
#
set -euo pipefail

if ! ROOT="$(git rev-parse --show-toplevel 2>/dev/null)"; then
  ROOT="$(cd "$(dirname "$0")/../../../.." && pwd)"
fi
cd "$ROOT"

PROJECT_NAME="${TING_CF_PROJECT:-co-ting}"
export npm_config_cache="${TMPDIR:-/tmp}/npm-cache"
export WRANGLER_LOG=none

echo "=== 1. Build and Audit Cover Bundle ==="
.claude/skills/cf-cover/scripts/build-cover-dist.sh

echo
echo "=== 2. Check Cloudflare Identity ==="
if ! env -u CLOUDFLARE_API_TOKEN npx wrangler whoami >/dev/null 2>&1; then
  echo "WARN: Wrangler OAuth session has expired or is not authenticated."
  if [ -t 0 ]; then
    echo "      Launching 'npx wrangler login' to re-authenticate..."
    env -u CLOUDFLARE_API_TOKEN npx wrangler login
  else
    echo "FAIL: Non-interactive environment. Please run once in an interactive terminal to authenticate:"
    echo "      env -u CLOUDFLARE_API_TOKEN npx wrangler login"
    exit 1
  fi
fi

echo
echo "=== 3. Deploy to Cloudflare Pages ==="
env -u CLOUDFLARE_API_TOKEN npx wrangler pages deploy tmp/cover-dist --project-name="$PROJECT_NAME" --commit-dirty=true

echo
echo "=== 4. Verify Live Edge Network ==="
URL="https://${PROJECT_NAME}.pages.dev"
echo "Probing ${URL}..."
if curl -sSI "$URL" 2>/dev/null | grep -q "200"; then
  echo "PASS: ${URL} responded HTTP 200 OK."
fi
BADGE="$(curl -sS "$URL" 2>/dev/null | grep -oE 'class="badge">v[0-9.]+' | head -1 || true)"
echo "Live Edge Version: ${BADGE}"

echo
echo "========================================================"
echo "✨ Cloudflare Cover Deploy Complete!"
echo "   URL: https://${PROJECT_NAME}.pages.dev"
echo "========================================================"
