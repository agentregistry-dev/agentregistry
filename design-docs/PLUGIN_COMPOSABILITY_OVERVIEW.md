# Plugin Composability — Overview

> One-page summary for reviewers. Full design: `design-docs/PLUGIN_COMPOSABILITY_SPIKE.md`.
> Status: design review · 2026-08-07 · Owner: Scott (ilackarms)

## What & why

Today a registry `Plugin` is a pointer at **one git repo**, materialized onto the
harness filesystem verbatim. This change makes a Plugin a **composable bundle** — the
same way an Agent is already a composition of registry artifacts:

```yaml
kind: Plugin
metadata: { name: incident-response, tag: v2 }
spec:
  source:                      # optional base — a git repo, as today
    type: git
    git: { repository: { url: https://github.com/acme/ir-plugin } }
  skills:                      # overlaid from the registry
    - { name: log-triage, tag: v3 }
    - { name: runbook-search }          # blank tag = latest, frozen at apply
  mcpServers:
    - { name: pagerduty }
  commands:                    # Prompt kind → commands/<name>.md
    - { name: declare-incident }
  instructions: { name: ir-guidelines } # Prompt kind → AGENTS.md
```

The registry compiles this into **one flattened, harness-shaped filesystem bundle** at
consumption time (deploy, `arctl pull`). In the API it stays a collection of reusable
objects: define a Skill once, include it in N plugins.

**Why product wants it:** reuse (no copy-vendoring curated skills into N repos),
governance (the composed surface — every hook, executable, MCP server — is visible as
registry objects and reviewable in the approval flow), and cross-harness delivery (the
composed bundle feeds the existing per-harness translation layer: AgentCore now,
kagent next).

## How it works (three sentences)

The spec holds **intent** (refs, possibly floating tags); the controller resolves every
component to a concrete pin (git commit, or tag+content-hash for inline kinds) and
records the full pin set in **status** — pins freeze until the spec changes. Compilation
is a **pure function of the pin set**: deterministic overlay of components onto the
optional base (skills → `skills/<name>/` keyed by the SKILL.md-declared name per the
Agent Skills spec, MCP servers → keyed `.mcp.json` merge,
instructions → `AGENTS.md` append; overlay replaces same-named base content, recorded in
a provenance report). Because any consumer reproduces byte-identical output from the
pins, the registry keeps **hosting nothing** — server, CLI, and runner all compile
independently. (Registry tags are replace-on-change, so inline-kind pins carry a
content hash that consumers re-verify at compile time, failing closed on mismatch —
spike §5.)

## What deliberately does NOT change

- **Existing plugins**: a source-only Plugin behaves exactly as today (`source` merely
  becomes optional).
- **Agent-level composition** (`AgentSpec.{Plugins,Skills,...}`): unchanged,
  complementary — agents compose plugins; plugins compose components.
- **"Registry hosts nothing"**: the controller still scans-and-discards; compiled
  bundles are derived, reproducible artifacts, never a source of truth.
- **The snapshot property**: materialization only ever reads status pins, so deployed
  content is still a frozen snapshot; the liveness lives in the spec.

## Scope cuts (decided)

| Decision | Call |
|---|---|
| Nested plugin-in-plugin | **Out of scope** — no target harness supports nesting; agents already compose multiple plugins. |
| Hooks / sub-agents as refs | Base-source-only (per 6/25 decision) — keeps `hooks.json` merging out entirely. |
| Ref type | New kindless `ComponentRef{Namespace,Name,Tag}` — the field name determines the kind. |
| Collision policy | **Overlay wins**, atomically per named component, recorded in the report; duplicate names *within* one spec are a validation error. |
| Formats (cross-lane, per the AR-Kagent sync + BYO contract ent#1265) | Canonical bundle stays the Claude-shaped superset; **agent-plugins.org** becomes P2's second translate target (input + delivery), skill dirs keyed by SKILL.md-declared name, fail-closed MCP headers for any server-staged bundle. Spike §9a. |
| Floating tags | Freeze at spec change; no auto-refresh in v1. |
| Serving composed plugins to unmodified Claude Code | **v1 skips them** in the marketplace.json compat catalog (they have no single upstream git URL); gated on the git-backed marketplace (enterprise #1195). Desktop consumption arrives with `arctl plugin pull` in **P2** — between P1 and P2 a composed plugin is declarable and governable but not yet consumable, an accepted gap of shipping P1 first. |
| Approval gating | Already covered — Plugin is a tagged artifact kind and the enterprise approval gate is generic over those. |

## Phasing

1. **P1 (OSS, starting now):** spec refs + validation, `Status.ResolvedComponents`,
   controller resolves refs (gating on referenced Skill pins), pure `compose` package,
   composed Manifest/Inventory, fingerprint extension so deployments redeploy when a
   pin set changes.
2. **P2:** consumption — `arctl plugin pull` (OSS) + clauderunner compose-at-start
   (enterprise/AgentCore); resurrects the previously-deleted translate/materialize
   layer as the delivery tail.
3. **P3:** kagent delivery — deploy-time compose → derived OCI artifact into kagent's
   existing skill-mount path (rides the kagent harness lane).
4. **P4:** composed-plugin serving via git-backed marketplace (tracked on ent#1195).

## What reviewers should push on

- §6 of the spike — the compose/merge rules table (placement, overlay-wins, `.mcp.json`
  fidelity mapping).
- §5 — the pin/freeze semantics and the cross-kind reconcile race (Plugin waiting on a
  referenced Skill's controller).
- §7 — the consumption-path plan, especially the kagent OCI-artifact lean.
