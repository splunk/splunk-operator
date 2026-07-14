---
title: GitHub Mirror Runbook
nav_order: 9
parent: Develop & Contribute
---

# GitHub Mirror Runbook

GitLab (`cd.splunkdev.com/sok/splunk-operator`) is authoritative. `github.com/splunk/splunk-operator` is a **read-only, one-way mirror**: a CI job sanitizes GitLab `main` and `develop` (stripping the GitLab-only paths in `gitlab-ci/gitlab-only-paths.conf`) and pushes them to GitHub.

This page is the operational runbook for the mirror — in particular, how it affects **open GitHub pull requests**. It lives under `docs/splunk_private/`, so it is not published to the public GitHub site.

## Why `main` and `develop` behave differently

The GitLab-only paths exist **only on `develop`**, never on `main` or release tags. The mirror push job (`gitlab-ci/github-mirror-push.sh`) acts on this:

| Branch | filter-repo | Commit SHAs | Push |
|---|---|---|---|
| `main` | skipped (no excluded paths in history) | preserved, identical to GitLab | fast-forward |
| `develop` | scoped rewrite (boundary commit → tip only) | ancestors before the boundary preserved; boundary-and-after re-minted | fast-forward in steady state (see below) |

Because `main`'s SHAs are never rewritten, **`main`-based PRs are never disrupted by the mirror.** The entire runbook below concerns `develop`-based PRs.

### Scoped rewrite — what gets re-minted, what is preserved

The GitLab-only paths were not present from `develop`'s root; they were introduced partway through its history. The push job exploits this with a **scoped** filter-repo rewrite instead of a whole-branch one:

- It finds the **earliest** commit that introduces any excluded path (the *boundary*), then rewrites only `^<boundary-parents> HEAD` — the boundary commit and its descendants.
- Every commit **before** the boundary never held an excluded path, so filter-repo leaves it untouched and it keeps its **original GitLab SHA**.
- Only the boundary-and-after range is re-minted, and the rewritten range is reattached onto the last preserved ancestor.

Why not rewrite the whole branch: filter-repo **drops GPG signatures**, so even an unchanged, path-free ancestor would get a new SHA once it passes through the rewrite — and that cascades new SHAs through *all* of history. Scoping confines the churn to the range that genuinely must change.

The strip is by-range but the guarantee is **whole-history**: because the boundary is the earliest commit that ever held an excluded path, the excluded paths are unreachable from *every* published commit. `github-mirror-verify.sh` asserts this independently.

## The forward-moving guard

`github-mirror-push.sh` classifies every push by comparing our HEAD to the current GitHub tip:

- **first** — ref absent on GitHub → plain push.
- **ff** — GitHub tip is an ancestor of our HEAD → plain push, no `--force`.
- **nonff** — histories diverge → **refused** unless `MIRROR_ALLOW_NON_FF=1` is set, then force-push.

`git filter-repo` is deterministic: identical inputs produce identical output SHAs. So in steady state, even the rewritten `develop` is a fast-forward over the previously-mirrored tip (`push_mode=ff`) and needs no force. A **non-fast-forward only arises on first cutover or an exclusion-list change** — both of which must be deliberate, drained operations. The guard makes those the only two times `MIRROR_ALLOW_NON_FF=1` is used; a surprise `nonff` in a routine run is a signal to stop and investigate.

## When the mirror runs in the pipeline

Both mirror jobs (`github-mirror-push` and `github-mirror-cutover`) run on the pipeline **DAG** via `needs:`, not in stage order. They wait only for the two `verify`-stage guards (`private-docs-reference-check` and `ai-config-files-check`) and the blocking `test`-stage jobs (`unit-tests`, `kubectl-splunk-tests`, `helm-chart-tests`) — **not** for `integration`. So the mirror publishes once code is proven safe-to-mirror and tests pass, and a **flaky `integration` job no longer skips it**.

---

## Scenario 1 — First mirror (cutover)

**Disruptive, one-time.** GitHub `develop` is replaced by the scoped filter-repo rewrite of GitLab `develop`. The old GitHub tip is **not** an ancestor of the new one, so this is a non-fast-forward history replacement.

Run these steps **before** enabling the mirror:

1. **Announce + freeze.** Pick a cutover window; notify the team and any external contributors that GitHub `develop` history will be replaced on that date.
2. **Inventory open `develop` PRs** and split by origin:
   ```bash
   gh pr list --repo splunk/splunk-operator --state open --base develop \
     --json number,isCrossRepository,author,title
   ```
3. **Internal PRs** Confirm the equivalent GitLab MR exists or updates are not needed anymore, then close the GitHub PR.
4. **External fork PRs** GitHub `develop` was rebuilt; after cutover external contributors may need to rebase their branch onto the new `develop`. Their commits and authorship are safe in their fork; only the PR's base view might be stale until rebased.
5. **`main` PRs → no action** (cutover is a no-op/fast-forward for `main`).
6. **Run the cutover via the `github-mirror-cutover` CI job.** Trigger the manual `github-mirror-cutover` job on a protected `develop` (or `main`) push pipeline. That job sets `MIRROR_ALLOW_NON_FF=1` itself, so the expected non-fast-forward is permitted for this one run without arming the steady-state auto job.
7. **Verify** with `gitlab-ci/github-mirror-verify.sh` (excluded paths absent from all history).

## Scenario 2 — Future mirror runs (steady state)

**Non-disruptive.** The steady-state auto job `github-mirror-push` runs on every protected `develop`/`main` push pipeline once armed (`PIPELINE_GITHUB_MIRROR_PUSH_ENABLED=true`, set after the first cutover). Because filter-repo is deterministic, unchanged commits keep identical re-minted SHAs, the old `develop` tip stays an ancestor of the new tip, and the push is `push_mode=ff` (no force). It is fast-forward-only (no `MIRROR_ALLOW_NON_FF`), so a non-fast-forward fails closed rather than being auto-published.

1. **For open PRs: no mirror-induced disruption.** A fast-forward run preserves the merge base, so — unlike a cutover — it never invalidates a PR's diff or forces a rebase; it behaves like a normal push to `develop`. An ordinary *content* conflict can still surface if a PR touches the same lines that landed on `develop` (exactly as in any repo); that can be resolved by the maintainer at cherry-pick time on GitLab (step 2).
2. **Per-contribution actions are separate** and happen at merge time via the [external-contribution flow](../develop/Contributing.md#maintainer-workflow-for-external-contributions) (cherry-pick onto GitLab → merge the MR → manually close the GitHub PR), not as part of a mirror run.
3. **Safety net:** the guard runs every push. If a steady-state run ever reports `nonff` on `develop`, it means the filter-repo inputs changed unexpectedly (most likely an unplanned exclusion-list edit — treat as Scenario 3).

## Scenario 3 — Exclusion-list config change

**Disruptive, like a mini-cutover for `develop`.** Editing `gitlab-ci/gitlab-only-paths.conf` changes filter-repo's inputs, so the rewritten `develop` is a new lineage (not a forward continuation) and the run is a non-fast-forward. Adding a path strips it from all history; removing a path makes that content reappear throughout history.

1. **Treat it as a planned mini-cutover.** Schedule and announce it.
2. **Drain/notify open `develop` PRs** as in Scenario 1, steps 3–4 — external fork PRs may need a one-time rebase onto the rebuilt `develop`.
3. **Check the `main` side-effect.** If you *add* a path that exists in `main`'s history, `main` flips to a rewrite and gets a one-time force-push too, which would disrupt `main`-based PRs. Check first:
   ```bash
   git -C <gitlab-clone> log --oneline main -- <new-path>
   ```
   If it returns anything, drain/notify open `main` PRs as well.
4. **Run the cutover via the `github-mirror-cutover` CI job.** Merge the `gitlab-only-paths.conf` change to `develop` first — the job reads the config from the branch it runs on — then trigger the manual `github-mirror-cutover` job on the resulting `develop` push pipeline. Once steady state is armed, that same pipeline also auto-runs `github-mirror-push`, which hits the expected non-fast-forward and **fails closed by design** — ignore that failure and run the manual cutover.
5. **Verify** with `gitlab-ci/github-mirror-verify.sh` that the new exclusion set is absent from all history.

---

## Quick reference

| Event | `develop` PRs | `main` PRs | `MIRROR_ALLOW_NON_FF` |
|---|---|---|---|
| First cutover | post-boundary PRs: merge base invalidated → close internal, rebase external. Pre-boundary PRs: unaffected | none | `1` (drained first) |
| Steady-state run | untouched | none | unset (ff) |
| Exclusion-list change | PRs newer than the (possibly earlier) boundary: one-time rebase; older: unaffected | none, unless added path is in `main` | `1` (drained first; armed auto-push fails closed — run manual cutover) |
