---
name: cf-cover
description: Publish or redeploy the marketing/design cover page (docs/cover.html) to Cloudflare Pages as co-ting.pages.dev (project co-ting). Trigger words: 做CF, CF, cf-cover, publish cover, deploy cover, landing page, Cloudflare Pages. Builds + audits the deploy bundle and deploys it automatically via direct upload — wrangler runs on stored OAuth creds on disk, dropping any env token via `env -u CLOUDFLARE_API_TOKEN`. If auth is expired, immediately launches `wrangler login` in background, opens the browser OAuth URL, awaits completion, and finishes deployment.
---

# Publish the web cover (`cf-cover`)

Publishes `docs/cover.html` (the marketing/design cover page) to the public web as a **curated subset** of the repo — just the cover page and the local assets it references, served as its own clean site root. Deployed to **Cloudflare Pages** as `co-ting.pages.dev` (project `co-ting`) via `wrangler` **direct upload** (we hand Cloudflare a built folder — no repo access, no remote build step of its own).

Live URL: `https://co-ting.pages.dev`. Build scripts live in `scripts/` next to this file.

## What I can and can't do

- **Build + audit the bundle: automated** — run `.claude/skills/cf-cover/scripts/build-cover-dist.sh`. Safe, deterministic, strictly enforces 5 pre-publish gates.
- **Deploy: automated end-to-end** — stored OAuth credentials (§2) deploy non-interactively. If `whoami` shows creds are expired or missing, **immediately open the login page**: launch `env -u CLOUDFLARE_API_TOKEN npx wrangler login` in a background job (`run_in_background: true`), read the authorization URL from `job_output`, and open it in the default browser (`open "<url>"`). As soon as the background job completes with `exit 0`, proceed straight to deployment.
- **Scaffold cover if missing** — if `docs/cover.html` does not exist yet, author it according to §0 before running the build.

---

## 0. Decision gates & Cover Specification (read first)

1. **Curated subset, not the repo.** The site is the *built bundle* (`cover.html` → `index.html` + referenced local media), not the repository's source files.
2. **Design Language & Repository Facts**:
   - **Product Identity**: `ting / 听` — an agent-first multi-source media engine with a terminal face.
   - **Dual Interface**:
     - *Machine Face*: Single-line JSON contract (`-j`), 4-tier deterministic exit codes (`0` success, `1` syntax/usage, `2+` tool failure, `4` semantic no-op), detached daemon lifecycle (`-d` / `--status` / `--stop` / `--pause` / `--enqueue`).
     - *Human Face*: Go (Bubbletea) single-view TUI, 6 row sources, live filter `/`, `z` undo without confirmations, Kitty graphics protocol cover art, 13 curated themes.
   - **Three Shipped Engines**: YouTube (`ting-engine-yt`), Bilibili (`ting-engine-bili`), NetEase Cloud Music (`ting-engine-ne` with synchronized lyrics and real `access` tiers).
   - **Hard Invariant**: Zero new runtime dependencies (strictly locked to 5: `yt-dlp`, `jq`, `mpv`, `nc -U`, `curl`).
   - **Distribution**: `brew install binlecode/ting`.
   - **Authoritative Version**: Derived from the repository `VERSION` file (e.g., `0.23.0`).
3. **Cloudflare account exists.** Free tier is fully sufficient.
4. **Project Domain**: `co-ting.pages.dev` (project `co-ting`).

---

## 1. Build + audit the bundle (automated)

Run the deterministic build script:

```bash
.claude/skills/cf-cover/scripts/build-cover-dist.sh
```

The script prepares a clean throwaway `tmp/cover-dist/` bundle and mechanically checks 5 audit gates:

| Gate | Check | Success Condition |
|---|---|---|
| **Gate 1** | Path rewrite | No dangling `../` relative links remain in `index.html`. |
| **Gate 2** | Secret & Path leakage | Zero private keys, API tokens, passwords, or local `/Users/` paths. |
| **Gate 3** | Self-contained assets | Every referenced non-data `src="..."` file exists locally inside the bundle. |
| **Gate 4** | Outbound links | All external `href`s are inventoried and point to legitimate destinations (e.g. GitHub repo). |
| **Gate 5** | Version badge parity | The badge (`class="badge">vX.Y.Z`) in `index.html` strictly matches the root `VERSION` file. |

Exits non-zero on any failure. Optionally inspect locally:

```bash
open tmp/cover-dist/index.html
```

---

## 2. Deploy to Cloudflare Pages

Run wrangler via `npx` (no global install required). Direct upload grants Cloudflare zero repository access.

### 🔴 Drop `CLOUDFLARE_API_TOKEN` — `env -u` on EVERY wrangler call

This machine deploys using **OAuth credentials stored on disk** (`~/Library/Preferences/.wrangler/config/default.toml`), which work non-interactively and need no manual token configuration. However, local shell configurations (such as `~/.zshrc`) often export a personal `CLOUDFLARE_API_TOKEN` that lacks Cloudflare Pages permissions. Wrangler **prefers the environment token over stored OAuth**, causing:

```
Authentication error [code: 10000]
Failed to automatically retrieve account IDs
```

This error means **the wrong credential was picked**, not that you are logged out. Running `wrangler login` while the token is set will immediately be rejected.

Therefore, prefix **every single wrangler command** with `env -u CLOUDFLARE_API_TOKEN`:

```bash
# 1. Verify stored OAuth identity:
env -u CLOUDFLARE_API_TOKEN npx wrangler whoami

# 2. Deploy the built dist folder:
env -u CLOUDFLARE_API_TOKEN npx wrangler pages deploy tmp/cover-dist --project-name=co-ting --commit-dirty=true
```

---

### First-Time Project Creation

If the `co-ting` project does not yet exist on Cloudflare Pages:

```bash
env -u CLOUDFLARE_API_TOKEN npx wrangler pages project create co-ting --production-branch=main --force
```

> **Why `--force` is required:** Wrangler 4.x defaults `pages` project creation to Cloudflare Workers static assets, which breaks classic static folder deployments ("Could not detect a directory containing static files") and removes the site from `<project>.pages.dev`. Supplying `--force` pins the project to classic Cloudflare Pages. Once created, future deploys do not need `--force`.

---

### When auth is expired or missing: open login page immediately

If `whoami` reports that OAuth credentials have expired (`Not logged in. Your auth token has expired...`) or are missing:

1. **Launch `wrangler login` in background**:
   Run `env -u CLOUDFLARE_API_TOKEN npx wrangler login` as a background job (`run_in_background: true`).
2. **Open browser login page immediately**:
   Read `job_output` to obtain the OAuth URL, and immediately execute `open "<auth_url>"` in the terminal so the user's browser opens the Cloudflare authorization page without delay.
3. **Wait for login completion**:
   When the user authorizes in the browser, the background job settles with `exit 0` (`Successfully logged in`), updating `~/Library/Preferences/.wrangler/config/default.toml`.
4. **Deploy immediately**:
   Proceed directly to:
   ```bash
   env -u CLOUDFLARE_API_TOKEN npx wrangler pages deploy tmp/cover-dist --project-name=co-ting --commit-dirty=true
   ```

---

## 3. Verify (live)

Confirm deployment health directly on the edge network:

```bash
URL="https://co-ting.pages.dev"

# 1. Edge HTTP status:
curl -sSI "$URL" | head -1                                   # Expect: HTTP/2 200

# 2. Version badge propagation:
for i in 1 2 3; do curl -sS "$URL" | grep -oE 'class="badge">v[0-9.]+'; sleep 2; done

# 3. Eyeball layout & visuals in browser:
open "$URL"
```

### Stale edge cache vs genuine bundle failure

The Cloudflare edge may serve cached assets from the previous deploy for a few seconds after wrangler reports `Deployment complete`. A single immediate fetch returning the old badge is simply edge cache propagation:

```bash
# Check cache-busting URL:
curl -sS -H 'Cache-Control: no-cache' "$URL?cb=$RANDOM" | grep -oE 'class="badge">v[0-9.]+'

# Check deployment-specific hash URL printed by wrangler:
curl -sS "https://<deployment-hash>.co-ting.pages.dev" | grep -oE 'class="badge">v[0-9.]+'
```

- New badge on `<deployment-hash>.co-ting.pages.dev` + old badge on `co-ting.pages.dev` = edge cache, wait a few seconds.
- Old badge on `<deployment-hash>.co-ting.pages.dev` = bad local bundle; re-run `build-cover-dist.sh` and deploy again.

---

## 4. Release Checklist & Maintenance

When cutting a new release in `ting`:
1. Version is bumped in `VERSION` (e.g. `0.23.0` → `0.24.0`).
2. Update the footer/hero badge in `docs/cover.html` to match (`<span class="badge">v0.24.0</span>`).
3. Run `.claude/skills/cf-cover/scripts/build-cover-dist.sh` to assemble and audit.
4. Run deployment command to sync Cloudflare Pages:
   ```bash
   env -u CLOUDFLARE_API_TOKEN npx wrangler pages deploy tmp/cover-dist --project-name=co-ting --commit-dirty=true
   ```
5. Verify live at `https://co-ting.pages.dev`.
