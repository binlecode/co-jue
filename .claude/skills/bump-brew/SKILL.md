---
name: bump-brew
description: Bump Homebrew formula in binlecode/homebrew-ting, commit and push the tap to GitHub, and execute local brew upgrade + test verification. Trigger words: bump-brew, bump brew, release brew, update formula, 升级 brew, 更新 brew, Homebrew tap. Runs to completion automatically, ensuring the tap formula matches the git tag and the local installation is upgraded.
---

# Homebrew Formula Release (`bump-brew`)

Automates the Homebrew tap release cycle for **ting (听)**. A release is never done until the tap formula is bumped, pushed to GitHub, and the local Homebrew installation is upgraded and tested against the formula contract.

- **Tap Repository**: `github.com/binlecode/homebrew-tap` (`binlecode/tap`)
- **Formula Path**: `Formula/ting.rb`
- **Installation Commands**: `brew install binlecode/tap/ting` or `brew tap binlecode/tap && brew install ting`
- **Local Tap Path**: `$(brew --repository binlecode/tap)` (`/opt/homebrew/Library/Taps/binlecode/homebrew-tap`)
- **Workspace Clone Path**: `../homebrew-tap` (`/Users/binle/workspace_fullstack/homebrew-tap`)

---

## What this skill does

1. **Preflight verification**: Confirms the target git tag (`v${VERSION}`) exists on remote `origin` (GitHub).
2. **Active playback guard**: Checks `pgrep -fl 'Cellar/ting/'` to warn if an active `ting` session is playing. (Homebrew removes the old Cellar upon upgrade; an active player resolving its binaries into the old Cellar could become disrupted).
3. **Deterministic checksumming**: Fetches the GitHub tag tarball (`https://github.com/binlecode/ting/archive/refs/tags/v${VERSION}.tar.gz`) and computes its exact SHA-256.
4. **Formula bump**: Updates `url` and `sha256` in `Formula/ting.rb` inside the local tap repository.
5. **Tap sync**: Commits (`ting ${VERSION}`) and pushes to `binlecode/homebrew-tap` on GitHub, then fast-forwards the workspace clone `../homebrew-tap`.
6. **Local upgrade & verification**: Runs `brew upgrade binlecode/tap/ting` (or reinstall), executes `brew test binlecode/tap/ting`, and validates `ting --version`.

---

## 1. Automated Execution

Run the bundled release script:

```bash
.claude/skills/bump-brew/scripts/bump-formula.sh [version]
```

If no version argument is supplied, the script automatically reads the repo root's `VERSION` file (e.g. `0.23.0`).

### Example Output

```
=== 1. Preflight: Checking Tag on Remote ===
Target version: 0.23.0 (v0.23.0)
PASS: Tag v0.23.0 exists on origin.

=== 2. Check for Active Running Playback ===
PASS: No running ting processes detected in Cellar.

=== 3. Download Archive & Compute SHA-256 ===
Tarball SHA-256: 40119f6e67cbd2c1a5efcb6dec45383a74d9a079d8837f1a01ccc1f7b7ce7e77

=== 4. Locate and Update Tap Repository ===
Updating Formula/ting.rb...

=== 5. Commit and Push Tap ===
PASS: Tap repository updated and pushed to GitHub.

=== 6. Local Brew Upgrade & Contract Test ===
Upgrading ting...
Running brew test...
==> Testing binlecode/tap/ting
==> /opt/homebrew/Cellar/ting/0.25.3/bin/ting --version
==> /opt/homebrew/Cellar/ting/0.25.3/bin/ting-play --version
==> /opt/homebrew/Cellar/ting/0.25.3/bin/ting-play --status -j
==> /opt/homebrew/Cellar/ting/0.25.3/bin/ting q </dev/null 2>&1 || true
==> /opt/homebrew/Cellar/ting/0.25.3/bin/ting-play --engines -j

========================================================
✨ Homebrew Formula Release Complete!
   Installed: ting 0.25.3
   Tap Formula: binlecode/tap/ting (0.25.3)
========================================================
```

---

## 2. Release Sequence

When releasing a new version of `ting`:

1. **Test & Verify**: Run `tests/contract.sh --offline` and `bash -n shell/*`.
2. **Version Bump in Repo**: Update `VERSION`, commit, and create tag `vX.Y.Z`:
   ```bash
   git tag vX.Y.Z
   git push origin main --tags
   ```
3. **Bump Homebrew Formula (This Skill)**:
   ```bash
   .claude/skills/bump-brew/scripts/bump-formula.sh
   ```
4. **Deploy Web Cover**:
   Update `docs/cover.html` version badge and publish to Cloudflare Pages:
   ```bash
   .claude/skills/cf-cover/scripts/build-cover-dist.sh
   env -u CLOUDFLARE_API_TOKEN npx wrangler pages deploy tmp/cover-dist --project-name=co-ting --commit-dirty=true
   ```

---

## 3. Homebrew Tap Notes

- **Tap Naming & Resolution**:
  Homebrew requires fully-qualified syntax for untrusted or third-party taps:
  ```sh
  brew install binlecode/ting/ting
  ```
  Or explicitly tap first:
  ```sh
  brew tap binlecode/ting
  brew install ting
  ```
- **Trusting the Tap**:
  Homebrew 6.0+ may warn about third-party taps. Trust the tap once via:
  ```sh
  brew trust binlecode/ting
  ```
