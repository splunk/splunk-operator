# Maintaining the Root Agent Harness

This companion governs two root harness surfaces:

| Path | Purpose |
|---|---|
| `AGENTS.md` | Always-loaded, task-neutral routing plus global safety and completion boundaries |
| `DEVELOPMENT.md` | On-demand implementation, generation, build, and validation workflow |

Preserve the established routing-first sections and wording in `AGENTS.md`
unless they are inaccurate. Prefer small additions at the owning section over
reorganizing or replacing the guide. Specialized skills keep their rationale
in a sibling `MAINTENANCE.md`.

Before adding guidance, ask:

1. Does it change where an agent starts, a decision it makes, or a safety
   boundary it must observe?
2. Is the detail already owned by the `Makefile`, package documentation, or
   user/developer documentation?
3. Can a path-first route replace a volatile list or copied workflow?
4. Does it apply to research, review, design, and triage as well as code
   changes? If not, it belongs in `DEVELOPMENT.md` or a specialized skill.

Keep project-wide routes, pre-action safety, and completion evidence in
`AGENTS.md`. Put the general code-change workflow and validation choices in
`DEVELOPMENT.md`. Keep detailed setup in the existing developer documentation,
deterministic mechanics in scripts and Make targets, and specialized repeated
procedures in skills.

Rely on skill frontmatter for discovery rather than linking a skill from
`AGENTS.md` solely to trigger it. `DEVELOPMENT.md` may invoke a skill at the
specific decision points where its workflow is required.

After changing either root harness file:

- Verify every referenced local path exists.
- Check that commands match the current `Makefile`.
- Run `bash gitlab-ci/validate-no-private-refs.sh`; public harness files must not
  reference paths removed from the GitHub mirror.
- Confirm the diff preserves the established guide unless a deletion is
  necessary and explained.
- Re-read each file in its loading context and remove duplicated guidance.
- Confirm routing still covers CI, production ownership, monitoring hardening,
  telemetry, dashboards, and operational links.
- Run `git diff --check`.
