---
name: release
description: Create a new GitHub release for this repo. Finds the previous release tag and increments the minor version unless an explicit version is given (e.g. "/release 2.0.0"). Tags main and publishes a categorized release via gh.
allowed-tools: Bash(git *), Bash(gh *), Read, Grep
---

# Create Release

Create a GitHub release for this repo using semver tags (`vX.Y.Z`). No files are modified and nothing is committed — this only tags the current main and publishes a release.

## Determine version

1. Find the previous version: `git tag --list 'v*' --sort=-v:refname | head -1`.
2. Pick the new version:
   - If the user supplied an explicit version (e.g. `/release 2.0.0`), use it, `v`-prefixed. Abort if that tag already exists.
   - Otherwise increment the **minor**: `vX.Y.Z` → `vX.(Y+1).0`.
   - If no previous tag exists, start at `v1.0.0`.

## Pre-flight checks

3. Verify you are on `main`. Abort if not — releases are cut from main only.
4. Verify the working tree is clean (`git status --porcelain`, ignoring untracked files). Abort if there are uncommitted changes.
5. Pull latest (`git pull --ff-only`) and verify local main matches `origin/main`.

## Tag and push

6. Create an annotated tag: `git tag -a v<version> -m "Release v<version>"`.
7. Push the tag: `git push origin v<version>`.

## Generate release notes

8. Collect commit messages since the previous tag (`git log <prev-tag>..HEAD --oneline`). If there is no previous tag, use all commits. Exclude `Update changelog for *` noise commits.
9. Group commits into categories by prefix/content:
   - **Features** — `feat`, `feature`, `add`
   - **Fixes** — `fix`, `bugfix`
   - **Other** — everything else
10. Create the release marked as latest:
    ```bash
    gh release create v<version> --title "v<version>" --notes "<generated notes>" --latest
    ```

## Post-release

11. Report the release URL and the version jump (e.g. `v1.2.0 → v1.3.0`) to the user.
