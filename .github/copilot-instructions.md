# GitHub Copilot Instructions

Read and follow `AGENTS.md` for repo-local coding and workflow guidance.

Before creating or updating a pull request, review the PR authoring rules in `AGENTS.md` and follow `.github/PULL_REQUEST_TEMPLATE.md`.

## Keeping the PR Description Up-to-Date

The `report_progress` tool is the **only** way to update an existing PR's description. The `create_pull_request` tool only works when no PR exists yet — it cannot edit the body of an existing PR.

Whenever you make meaningful progress on a task:
1. Call `report_progress` with an accurate `prDescription` that follows `.github/PULL_REQUEST_TEMPLATE.md`.
2. Use the **exact top-level headings** from the template: `# Description`, `# Change Type`, `# Changelog`. The CI labeler enforces a `# Description` section (single `#`) and will fail if `##` headings are used or the section is missing.
3. Populate every required section of the template (Description, Change Type, Changelog).
4. Do **not** consider the task complete until the PR description has been updated via `report_progress`.

> **Important:** The `prDescription` you pass to `report_progress` becomes the literal GitHub PR body. Always start it with `# Description` (not `## Description`) so the CI labeler can find the required section.
