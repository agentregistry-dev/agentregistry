# ADR 0001 — Plugins are pinned source pointers, not registry-hosted bundles

- **Status:** Accepted. **Supersedes decision D2** ("OCI artifacts for both: canonical store + materialized output") from the 2026-06-16 plugins/harness design review.
- **Date:** 2026-06-22
- **Code:** landed behind draft PR #554 (`ilackarms/plugins-harness`), starting at commit `5c09ada9`.

## Context

The first cut of the Plugin kind (D2) had the registry **host** each plugin's
bytes: on publish, a controller fetched the origin, normalized it into a
canonical bundle, pushed that bundle to an OCI registry as a content-addressed
artifact, and recorded the artifact ref in status. Deploys then pulled the
bundle back from the registry's OCI store and materialized it.

That model made the registry a **binary artifact store**: it had to run/secure
an OCI registry, key the upload, handle idempotency, garbage-collect orphaned
bundles (which pulled in finalizers), and fail closed when no registry was
configured. D2's stated rationale was "translate + materialize need the
content, so the registry must store it."

Two things changed that calculus:

1. **Materialization moved to deploy time.** The harness filesystem is built
   from the source *when an agent is deployed*, not at publish. So the registry
   never needs the bytes resident — it only needs to know *where the bytes are*
   and *which exact revision*.
2. **The harness ecosystem is already source-based.** Agents and skills declare
   a `Repository` pointer (`{URL, Branch, Commit, Subfolder}`) and the thing that
   runs them does the cloning (the CLI for local; kagent for Kubernetes). And
   Claude Code's plugin marketplace is itself a *catalog of per-plugin external
   git pointers* — an unmodified install clones each plugin's own repo.

## Decision

A **Plugin is a pinned pointer to an external source**, and the registry hosts
**no plugin bytes**.

- `PluginSpec` is user intent only: `Origin` = a **git** source (or, later, an
  OCI digest), reusing the shared `v1alpha1.Repository` type. **Git-first**; the
  OCI origin type stays declared but unimplemented.
- The Plugin controller **resolves and pins** the pointer out of band: it
  resolves the ref (branch/tag/HEAD) to a concrete commit SHA via
  `git ls-remote`, shallow-clones that commit to scan the source, and records
  the server-determined data in `PluginStatus` — `ResolvedSource{Type, Commit}`
  plus the parsed `Manifest` and derived `Inventory`. It stores nothing.
- Deploys materialize the harness filesystem from the source (clone the pinned
  commit → translate → write). *(Deploy-time wiring is the deferred next step.)*

### Why source-pointer over hosted-OCI (D2)

- **The registry no longer needs the bytes.** D2's premise dissolves once
  materialization is deploy-time: the byte store was solving a problem we no
  longer have. Pointer + resolved-SHA is sufficient for reproducibility.
- **Consistency.** Plugins become structural siblings of agents/skills (same
  `Repository` pointer, same "the runner clones it" model) instead of a bespoke
  artifact pipeline.
- **Marketplace serving is git-native (see below).** A byte-hosting registry is
  *redundant* with how Claude Code already fetches plugins.
- **Less operational surface.** No OCI registry to run/secure/credential, no
  upload-idempotency edge cases, no bundle GC, no finalizers, no
  fail-closed-when-unconfigured branch. The git-injection guard (`safeGitRef`)
  is the only new attack surface and is unit-tested.

### Why git-first (not OCI)

The shared `Repository` type and the mature `gitutil` clone/checkout path make
git the lower-friction, more-consistent default, and Claude Code marketplaces
are git-native. The `Origin` union keeps OCI as a declared type so it can be
added later with no API churn (it's the natural durability escape hatch — see
below).

## Consequences

### Resolved: Source durability (the real cost of this pivot)

Because the registry holds no bytes, a **deleted or force-pushed upstream makes
a pinned commit unreachable**, and a deploy (or a fresh install) would then
fail. This is the one genuine regression versus hosted-OCI. Mitigations, in
order of when they apply:

1. **The pin guarantees reproducibility while upstream exists.**
   `status.ResolvedSource.Commit` freezes the exact revision, so a moving branch
   or a deleted tag does not silently change what deploys. This is strictly
   better than an unpinned pointer and matches what agents/skills do today.
2. **OCI origin for durability-sensitive plugins.** Once the OCI origin type is
   implemented, a publisher who needs guaranteed retention points at an
   immutable, digest-pinned OCI artifact in a registry they control. The byte
   durability then lives in *their* registry, not ours — same as today's
   container images.
3. **Optional registry-managed mirror (future, additive — NOT a reversal).** If
   operators want the registry itself to guarantee availability, we can add an
   **opt-in content cache**: on successful resolve, the controller also copies
   the pinned bundle into a registry-side blob/OCI store and records a
   `status.mirror` ref; deploys fall back to the mirror when the origin is
   unreachable. Crucially this is a **cache, not the source of truth** — the
   origin pointer remains authoritative, the mirror is best-effort and
   GC-able, and it is entirely optional. This re-uses the *concept* of the
   deleted OCI store without re-coupling the data model to it.

**v1 decision:** pointer-only, no bytes, durability via the origin's own
retention (git host today; OCI origin when implemented). The optional mirror is
deferred and sketched, not built. Document the upstream-deletion risk in user
docs.

### Resolved: Marketplace serving without hosting bytes (1e)

Confirmed against Claude Code docs (code.claude.com/docs/en/plugin-marketplaces,
.../discover-plugins, .../plugins-reference): a Claude Code marketplace is a
**catalog of pointers**, not a byte host. A marketplace.json `plugins[].source`
may be an external per-plugin git source — `github` (`owner/repo` + `sha`),
`url` (git URL + `sha`), or `git-subdir` (`url` + `path` + `sha`) — and on
`/plugin install` the harness **clones each plugin's own source repo at the
pinned `sha`**. Plugins do not live inside the marketplace repo.

This maps **directly** onto our model:

| marketplace.json `source` field | comes from |
| --- | --- |
| `source: "git-subdir"` / `"github"` / `"url"` | `Plugin.Spec.Origin` type |
| `url` / `repo` | `Origin.Git.Repository.URL` |
| `path` | `Origin.Git.Repository.Subfolder` |
| `sha` | **`status.ResolvedSource.Commit`** (the controller's pin) |

So 1e (marketplace serving) is: the registry emits a marketplace.json catalog
whose entries point at each plugin's external git origin **pinned to the
resolved SHA**; an unmodified Claude Code install clones the bytes from git
directly. The registry serves the *index*, never the bytes. The catalog can be
git-hosted or served as a direct `marketplace.json` HTTPS endpoint (we use
absolute git URLs per entry, avoiding the relative-path limitation of
direct-URL marketplaces).

The controller's resolve-and-pin output (`ResolvedSource.Commit`) is exactly the
`sha` the marketplace.json needs — the pivot makes 1e *more* natural, not less.

Caveats: private plugin repos require the *user's own* git auth at install time
(the registry cannot broker credentials); Codex has no documented marketplace
mechanism today (this is Claude Code-specific).

### Other consequences

- **No DB migration.** `status` is JSONB; the `content_hash` column is the
  generic spec hash, unaffected.
- **Status shape is breaking** for any consumer that read `status.content` or
  the `StorageNotConfigured`/`Published`/`BundleInvalid` reasons — replaced by
  `status.resolvedSource` and `Resolved`/`OriginUnresolvable`/`OriginUnsupported`/
  `SourceInvalid`. No external consumers exist yet.
- **New runtime dependency:** the controller and (later) deploys shell out to
  `git` server-side. Only github.com is supported initially (matches skills).
  Resolve/clone is bounded by context timeout plus file-count/byte ceilings.

## Status / ratification

This ADR **reverses confirmed decision D2** and records the accepted
source-pointer model. The code is landed behind draft PR #554 so CI runs and
reviewers see real code. Marketplace serving (1e), arctl integration (1f), and
cloud deploy-time materialization proceed as follow-on work on top of this
foundation.
