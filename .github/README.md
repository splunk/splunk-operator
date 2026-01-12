# GitHub Workflows

## PR Testing Strategy

### Why Two Triggers?

GitHub's `pull_request` event doesn't expose secrets to fork PRs (for security). But we need secrets to run integration tests. The `pull_request_target` event does expose secrets—but it sets `GITHUB_SHA` to the **base branch**, not the PR. This means the default checkout gets the wrong code, and creates security risks if not handled carefully.

### How We Handle It

| Trigger | Branch | Why |
|---------|--------|-----|
| `pull_request_target` | `develop` | Enables secrets for fork PRs; requires manual approval |
| `pull_request` | All except `develop` | Standard trigger for trusted maintainers |

### Security Requirements

1. **Always use `approval-gate.yml`** as a dependency for jobs needing secrets
2. **Always specify `with.ref`** on all `actions/checkout` steps (enforced by `lint-workflows.yml`)
3. **Always pass the approval gate's `commit-sha`** to prevent testing unapproved code

### Checkout Patterns

**For workflows using approval-gate** (recommended for `pull_request_target`):

```yaml
jobs:
  approval-gate:
    uses: ./.github/workflows/approval-gate.yml

  build:
    needs: approval-gate
    steps:
      - uses: actions/checkout@v6
        with:
          ref: ${{ needs.approval-gate.outputs.commit-sha }}
```

**For simpler workflows** (e.g., `pull_request` or `push` triggers):

```yaml
# Preferred: Define ref once at workflow level, reuse in all jobs
env:
  CHECKOUT_REF: ${{ github.ref }}
jobs:
  build:
    steps:
      - uses: actions/checkout@v6
        with:
          ref: ${{ env.CHECKOUT_REF }}
```

> ⚠️ Without these safeguards, a malicious commit could be added after approval but before execution.
