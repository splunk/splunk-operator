## Summary

- release scope
- version or patch intent

## Release Context

- source release branch:
- target branch: `main`
- changelog or release notes updated:
- release-candidate number:

## Validation Evidence

- release branch pipeline:
- MR pipeline to `main`:
- known exceptions or follow-up tickets:

## Publish Plan

- publish path: `main` manual jobs or dedicated `release_publish` rerun
- pinned source ref or pipeline ID if needed:
- target image, chart, and bundle destinations reviewed:

## Jira

- epic:
- story:
- follow-up:

## Checklist

- [ ] source branch uses `release/<version>` or `release-<version>`
- [ ] changelog or release notes are updated on the release branch
- [ ] release validation passed on the final release-branch tip
- [ ] candidate artifacts are built once on the release branch and promoted from `main`
- [ ] release publication inputs were reviewed before merge
