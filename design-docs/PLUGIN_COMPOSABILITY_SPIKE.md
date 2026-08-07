# Spike — Plugin Composability

> Status: DRAFT spike, 2026-08-06 · Author: Scott (ilackarms) + Claude
> Verified against OSS `main` @ `98ce4b08`. Companion docs: `design-docs/PLUGINS_AND_HARNESSES_RESEARCH.md` (v2), `docs/plugin-marketplace-compatibility.md`.

## 1. The ask

Product wants plugins to become **bundles** the same way agents already are. Today a
`Plugin` is a one-to-one pointer at a single git repo that gets materialized onto the
filesystem verbatim. The ask: let some subset of that bundle be **declaratively overlaid
with artifacts that already live in the registry** (Skills, MCP servers, prompts), so a
plugin is *compiled* from multiple pinned git sources into one flattened filesystem
bundle at consumption time — while the API keeps representing it as a collection of
reusable objects.

Why this matters:
- **Reuse/composability** — a curated Skill or MCPServer is defined once and included in
  N plugins, instead of being copy-vendored into N git repos.
- **Governance** — the composed surface (skills, hooks, executables, MCP servers) is
  visible and reviewable as registry objects, not opaque repo contents.
- **Cross-harness delivery** — the same composed bundle feeds the per-harness
  translation layer, so composed plugins ship to AgentCore today and kagent next, not
  just to desktop Claude Code.

Adjacent lanes (context, not scope): Christy's **BYO container path** (customers running
e.g. LangGraph in their own container) is the custom-code track and is untouched by
this; the **kagent runtime** is the imminent second harness target after AgentCore, so
nothing here may be AgentCore-specific.

## 2. Where the code actually is today (verified at `98ce4b08`)

The design-track history describes several things that are **no longer on main** — the
spike builds on what's actually there:

| Surface | State on main |
|---|---|
| `PluginSpec` (`pkg/api/v1alpha1/plugin.go:30-46`) | `Title/Description/IconURL/Harnesses/Source` — **`Source` is required** (`plugin_validate.go:24-26`), exactly one git repo (`{URL, Branch|Commit, Subfolder}`) or a digest-pinned OCI ref (unimplemented in the resolver). No composition concept. |
| `PluginStatus` (`plugin.go:59-69`) | `ResolvedSource{Type,Commit,Digest}` + `Manifest` (typed lossless plugin.json) + `Inventory` (scanned skills/commands/agents/hooks/mcp/executables). |
| Plugin controller (`internal/registry/controller/plugin_controller.go`) | Resolve-and-pin: ls-remote → shallow clone at commit → `bundle.FromDir` → `ParseManifest` + `BuildInventory` → status. Bundle is scanned and **discarded**; terminal-vs-retryable classification; ObservedGeneration-gated. |
| `internal/registry/plugins/` | Only `bundle/` (in-memory `CanonicalBundle`, `FromDir`, manifest/inventory scan) and `source/` (`Resolver` iface + `GitResolver`, GitHub-only, 2-min clone bound, 10k-file/128MiB ceilings). **`translate/` and `materialize/` were deleted** (`ea5e66fe`); their prior shape (identity claude-code adapter, `PathMapping` default-pass, `TranslationReport` drop-with-warning, `WriteDir`) is the recorded prior art to resurrect. |
| Agent composition (`agent.go:48-59`) | Top-level `AgentSpec.{Plugins,Skills,Instructions,MCPServers} []ResourceRef` — **the precedent**. Refs are pure identity `{Kind,Namespace,Name,Tag}` (`ref.go:11-16`); kind defaulted in place at admission; existence-checked by `ResolveRefs`; composition requires `compatibleHarnesses`. |
| Skill kind (`skill.go`) | Git-source pointer + resolve-and-pin controller (ls-remote only, no clone/scan); `SkillStatus.ResolvedSource.Commit`. |
| Fingerprinting (`pkg/types/fingerprint.go:191-288`) | Deployment change-detection already hashes `Status.ResolvedSource` of referenced Plugins/Skills when the deployment selects a harness. This is the ready-made re-deploy trigger for composition. |
| marketplace.json compat (`internal/registry/api/handlers/pluginmarketplace/`, `pkg/pluginmarketplace/translate.go`) | Read-only, default-off. Serves each Ready plugin as **its external git URL + resolved SHA** (`url` / `git-subdir` source forms). The registry serves **no bytes**. |
| Runtime consumption | **None in OSS.** `runtimetypes.Agent` has a `Skills []AgentSkillRef` field and kagent-side `GitRepo` translation, but the v1alpha1→runtime bridge never populates it; plugins don't reach runtimes at all. Enterprise clauderunner (AgentCore) installs plugins per-directory from pinned git at container start. |

Two prior decisions are directly in tension with this ask and must be explicitly
revisited, not silently ignored:

1. **6/17 architecture meeting:** "plugins fully self-contained, snapshot at package
   time — NO live skill references (re-package on skill update)." The composability ask
   reverses this. The original motive was translation-time simplicity and avoiding a
   resolution graph; the mitigation below is that refs resolve to **pinned commits
   recorded in status**, so the *materialized* plugin is still a self-contained snapshot
   — the liveness lives in the spec, the pin lives in the status. Same resolve-and-pin
   contract the kind already has.
2. **ADR (source-pointer pivot, `632dd93d`, later removed in `7f738b23`):** "the
   registry hosts nothing; plugin bytes flow harness←git." Composed plugins have **no
   single upstream URL**, which breaks the marketplace-serving story for them (§7). The
   ADR anticipated this with the "optional additive mirror-cache is not a reversal"
   clause — a compiled bundle is a *derived, reproducible* artifact, not a source of
   truth.

## 3. Design overview

A composed plugin is an **ordered, deterministic overlay**:

```
base git source (optional)          ← today's Spec.Source, becomes optional
  + Skill refs                      → skills/<name>/**
  + MCPServer refs                  → merged into .mcp.json
  + Command refs (Prompt kind)      → commands/<name>.md
  + Instructions ref (Prompt kind)  → AGENTS.md (append)
─────────────────────────────────────────────────────────
  = CanonicalBundle (claude-code-shaped superset)
  → translate.ToHarness(h)          → flattened harness FS
```

Key properties:

- **Spec = intent (refs), Status = pins.** The controller resolves every component to a
  concrete commit and records the full pin set in status. Given the status pins, the
  compile is **byte-deterministic and reproducible anywhere** — server, CLI, or runner
  can all produce the identical flattened bundle. This is what lets the registry keep
  "hosting nothing" for the deploy path while still enabling optional serving (§7).
- **Composition happens in canonical space, translation after.** The overlay engine is
  harness-agnostic; `translate` (resurrected) runs on the composed result. Nothing is
  AgentCore- or kagent-specific in the compile step.
- **Typed refs, not generic layers.** Mirrors `AgentSpec`'s composition block rather
  than inventing a `layers: []` free-form overlay. Each component kind has a *known
  destination* in the bundle, which is what makes collision rules, inventory scanning,
  validation, and translation tractable. (A generic multi-git-layer model is strictly
  more powerful and strictly harder to reason about; it can be added later as a new
  component type if a real need appears.)

## 4. API design

### 4.1 `PluginSpec` (additive, backwards-compatible)

```go
type PluginSpec struct {
    Title       string   `json:"title,omitempty"`
    Description string   `json:"description,omitempty"`
    IconURL     string   `json:"iconUrl,omitempty"`
    Harnesses   []string `json:"harnesses,omitempty"`

    // Source is the optional base layer. Existing plugins are unchanged:
    // source-only plugins keep exactly today's semantics.
    Source *PluginSource `json:"source,omitempty"`     // was required → optional

    // Composition — registry artifacts overlaid onto the base.
    Skills       []ComponentRef `json:"skills,omitempty"`       // → Skill
    MCPServers   []ComponentRef `json:"mcpServers,omitempty"`   // → MCPServer
    Commands     []ComponentRef `json:"commands,omitempty"`     // → Prompt
    Instructions *ComponentRef  `json:"instructions,omitempty"` // → Prompt
}

// ComponentRef identifies a registry artifact whose kind is determined by the
// field holding it — so unlike ResourceRef it carries no Kind. This removes
// the kind-defaulting/mismatch machinery entirely (the historical F-5 bug
// class, and agent_validate.go's persist-kind-in-place workaround, exist only
// because ResourceRef carries a field the schema already determines).
type ComponentRef struct {
    Namespace string `json:"namespace,omitempty"` // blank = referrer's namespace
    Name      string `json:"name"`
    Tag       string `json:"tag,omitempty"`       // blank = latest
}
```

Note: `Status.ResolvedComponents` still carries `Kind` — it's a flattened list
across component kinds, so entries must stay self-describing. And the existing
`ResolveRefs`/store-lookup plumbing speaks `ResourceRef`; the resolver constructs
one with the field's known kind (`ref.toResourceRef(KindSkill)`) — no parallel
machinery. `AgentSpec`'s composition block keeps `ResourceRef` for now (legacy
shape; a later migration can tolerate-and-ignore the persisted `kind` field) —
decoupled from this lane.

Validation changes (`plugin_validate.go`):
- Require **at least one** of `source` / `skills` / `mcpServers` / `commands` /
  `instructions` (a pure-composition plugin with no git repo is legal and expected —
  that's the "curated bundle" product case).
- Existence-check refs via `ResolveRefs` (Plugin gains a `ResolveRefs` method next to
  Agent's; `accessors.go` dispatcher already exists). No kind defaulting or kind
  mismatch checks — `ComponentRef` makes both impossible by construction.
- Reject duplicate skill/command *names* within the spec (the materialized path is
  keyed by name).

Deliberately **excluded from v1**:
- **`Plugins []ResourceRef` (nested plugin-includes-plugin) — out of scope, not merely
  deferred (Scott, 2026-08-06).** No harness supports nested plugins today (Claude Code
  doesn't; plugins are flat installable units everywhere we target), so the registry
  modeling something the runtime can't express would be invented semantics. It's also
  structurally hostile: whole-plugin overlay has no namespaced destination (two plugins
  legitimately own overlapping paths: `plugin.json`, `hooks/`, `.mcp.json`), forcing a
  general merge policy on exactly the least-mergeable files. Agents already compose
  multiple plugins at the `AgentSpec.Plugins` level, and Claude Code has native
  manifest `dependencies` for cross-plugin needs. Revisit only if harnesses themselves
  grow the concept.
- **Hooks / sub-agents as refs.** Locked plugin-only on 6/25; they arrive via the base
  source. This conveniently removes the hardest merge problem (hooks.json) from v1.
- **Per-ref config** (enable/disable, params, dest-path overrides). Refs stay pure
  identity, same as Agent's.

### 4.2 `PluginStatus`

```go
type PluginStatus struct {
    Status         `json:",inline"`
    ResolvedSource *PluginResolvedSource      `json:"resolvedSource,omitempty"` // base (unchanged)
    // NEW: pin set for every composed component, in spec order.
    ResolvedComponents []PluginResolvedComponent `json:"resolvedComponents,omitempty"`
    Manifest       *PluginManifest             `json:"manifest,omitempty"`  // now: of the COMPOSED bundle
    Inventory      *PluginInventory            `json:"inventory,omitempty"` // now: of the COMPOSED bundle
}

type PluginResolvedComponent struct {
    Kind      string `json:"kind"`
    Namespace string `json:"namespace"`
    Name      string `json:"name"`
    Tag       string `json:"tag"`              // the tag actually resolved (never blank)
    Commit    string `json:"commit,omitempty"` // for source-backed kinds (Skill)
    // Prompt/MCPServer are inline content kinds — pinned by (tag, contentHash):
    ContentHash string `json:"contentHash,omitempty"`
}
```

`Manifest`/`Inventory` being computed over the **composed** result is the governance
payoff: the UI, the marketplace catalog, and the enterprise approval flow all see the
true risk surface (every hook, executable, MCP server — whether it came from the base
repo or an overlay) without anyone cloning anything.

## 5. Resolution — plugin controller changes

`reconcile` (`plugin_controller.go:302`) grows from "resolve one source" to "resolve the
component set, then compose in memory, then scan":

1. Resolve base source (unchanged path) — skipped if `Source == nil`.
2. For each ref, **read the referenced object and gate on its readiness**:
   - `Skill` → require `Status.ResolvedSource.Commit` (the Skill controller's pin).
     Not-yet-resolved ⇒ **retryable** (`ComponentsPending`, backoff) — the skill
     controller will get there; missing object ⇒ terminal `ComponentMissing`.
     Then shallow-clone the skill's pinned commit (same `gitutil` path, same
     clone bounds) to obtain its file tree.
   - `Prompt` / `MCPServer` → inline content kinds, immutable-by-tag: read spec,
     record `(tag, contentHash)`. No I/O.
3. **Compose** (§6) into one `CanonicalBundle`.
4. `ParseManifest` + `BuildInventory` on the composed bundle (unchanged code).
5. Patch status: `ResolvedSource` + `ResolvedComponents` + composed
   `Manifest`/`Inventory`, `Ready=True/Resolved`.

Pinning/drift semantics — the important call:

- Refs with an **explicit tag** are stable forever (content kinds are immutable-by-tag).
- Refs with **blank tag ("latest")** float by design. The controller re-resolves on
  spec change (generation bump) and on the periodic **resync tick — but
  `pluginReconciled` gates on ObservedGeneration alone**, so today a resync would *not*
  re-resolve a Ready plugin. v1 keeps exactly that behavior: **a composed plugin's pin
  set freezes at last spec change**. This preserves the "snapshot" property from the
  6/17 decision and makes tag-floating drift a non-issue. If product later wants
  auto-refresh of floating refs, it's an explicit opt-in field
  (`spec.refreshPolicy: OnSpecChange | Periodic`), not a default.
- **Cross-object drift** (a referenced Skill's spec changes → new commit): the Skill's
  own controller re-pins the Skill, but the Plugin's recorded pin set is unchanged
  until the Plugin is touched — again, deliberate. The fingerprint machinery
  (`fingerprint.go:191-288`) hashes the **Plugin's** ResolvedSource today; it should be
  extended to hash `ResolvedComponents` too, so deployments redeploy exactly when the
  composed pin set actually changes.

New wrinkle the controller must handle: reconcile ordering across kinds. A Plugin
referencing a just-created Skill races the Skill controller. The retryable
`ComponentsPending` classification + rate-limited requeue already gives the right
convergence behavior with zero new machinery (same trick as enterprise
`resolveHarnessSkills` gating on the Skill pin).

## 6. Compose semantics (the deterministic flatten)

New package `internal/registry/plugins/compose` (sibling of `bundle/`):

```go
// Compose overlays resolved components onto an optional base, producing a new
// canonical bundle. Pure function of its inputs — no I/O, no clock, no maps
// iterated in random order. Byte-deterministic.
func Compose(base *bundle.CanonicalBundle, comps []ResolvedComponent) (*bundle.CanonicalBundle, *Report, error)
```

Placement rules (v1):

| Component | Destination | Merge rule |
|---|---|---|
| Skill `s` | `skills/<name>/**` (its repo tree, `SKILL.md` at root) | **Overlay wins (Scott, 2026-08-06).** If the base already has `skills/<name>/`, the ref **replaces the whole directory** (never a file-level interleave of base + ref trees). The replacement is recorded in the compose `Report` and surfaced in status, so shadowing is visible, not silent. Supports the "patch a vendored plugin's skill with the curated registry version" workflow directly. |
| Command (Prompt) | `commands/<name>.md` | Overlay wins, same as skills (whole-file replace, recorded). |
| MCPServer | entry in `.mcp.json` | **Structured merge by server name; overlay entry replaces a same-named base entry** (recorded in `Report`). Mapping fidelity (verified against `mcpserver.go:16-59`): `Remote{Type,URL,Headers}` → a remote `.mcp.json` entry directly; `Source.Package` with npm/pypi origin → a stdio entry (`command`/`args` from `MCPPackageLaunch`, or origin-type defaults); OCI-package origins have no faithful desktop `.mcp.json` form → **rejected at Plugin validation** with a clear error. |
| Instructions (Prompt) | `AGENTS.md` | **Append** with a `\n\n---\n\n` separator if the base has one; create otherwise. Only merge-by-concatenation in v1, and only here. |
| `plugin.json` manifest | `.claude-plugin/plugin.json` | Base's manifest **passes through untouched** (preserving the hard-won losslessness). Composed components land in default-discovered paths (`skills/`, `commands/`, `.mcp.json`), so no manifest surgery is needed. If there is **no base**, generate a minimal manifest from `PluginSpec{Title,Description}` + plugin name. |

Design rationale: every rule is either *namespaced placement with whole-unit
overlay-wins* (a skill directory or command file is replaced atomically, never
interleaved), *keyed structured merge* (.mcp.json, replace-by-server-name), or
*append* (AGENTS.md). There is still no generic file-level "last layer wins" across
arbitrary paths — overlay-wins operates on whole named components, which keeps
compile output explainable in a review UI ("skills/foo came from Skill X @ commit Y,
replacing the base's copy") via the `Report`'s provenance map. Ref-vs-ref duplicate
names within one spec remain a **validation error** (§4.1) — overlay-wins governs
overlay-vs-base only, where there's a defined precedence; two refs claiming the same
name has none.

Bundle ceilings (`MaxBundleFiles`/`MaxBundleBytes`) apply to the **composed** result,
not just per-source.

## 7. Consumption paths — where the flatten actually runs

The compile is a library; it runs at every consumption point, always from status pins:

1. **Deploy time (the reason product wants this).**
   - *AgentCore (now):* the enterprise clauderunner already installs each plugin from
     pinned git at container start. It gains the compose step: fetch base + component
     pins → `Compose` → `translate.ToHarness(claude-code)` → `WriteDir` →
     `--plugin-dir`. Env-var config carries the pin set (it's small — refs + SHAs), same
     delivery mechanism as P2; the P3 volume track is unaffected.
   - *kagent (next):* a composed plugin **cannot** be expressed as the single
     `GitRepo{URL,Ref,Path}` kagent mounts. Two options: (a) hand kagent N GitRepo refs
     — but overlay/merge semantics (.mcp.json, AGENTS.md) are not expressible as
     mounts; (b) **compose server-side at deploy time and push a derived OCI artifact**,
     which kagent's existing OCI skill-mount path consumes. Lean **(b)**: it reuses
     shipped kagent infra, and the artifact is a *reproducible derived cache* keyed by
     the pin-set hash — explicitly the "additive mirror-cache" the ADR allowed, not a
     return to registry-as-source-of-truth. This is Phase-2 work aligned with the
     kagent lane; nothing in the v1 API blocks or presumes it.
2. **`arctl` local pull.** `arctl plugin pull <ns>/<name>:<tag> [--harness claude-code]`
   → same library → materialize into `~/.claude/plugins/...`. This is the two-mode
   local story's mode 1, now compose-aware.
3. **marketplace.json compat endpoint — the one thing composability breaks.**
   `FromPlugin` (`pkg/pluginmarketplace/translate.go:46`) hands Claude Code the
   *external git URL + SHA*; a composed plugin has no such URL. **DECIDED (Scott,
   2026-08-06): v1 skips composed plugins in the compat catalog** (the handler already
   silently skips not-representable entries — OCI sources take the same path today);
   desktop consumption of composed plugins goes through `arctl plugin pull`. Serving
   them to unmodified Claude Code **waits on the git-backed marketplace**
   (solo-io/agentregistry-enterprise#1195, dependency recorded there 2026-08-06): the
   registry materializes each composed plugin into the hosted marketplace repo —
   derived bytes, reproducible from pins, keyed by the pin-set hash.

## 8. What this reverses, and why that's sound

| Prior decision | Disposition |
|---|---|
| "Plugins self-contained, no live skill refs, snapshot at package time" (6/17) | **Amended, not discarded.** The *spec* holds live refs (that's the product ask — reuse), but the *status pin set* is the snapshot, and materialization only ever reads pins. Floating tags freeze at last spec change (§5). The self-contained property survives where it matters: the deployed artifact. |
| "Registry hosts nothing" (source-pointer ADR) | **Held for v1.** The controller still scans-and-discards; all consumers reconstruct from pins. The kagent OCI artifact (Phase 2) and any bundle-serving endpoint are derived caches — reproducible, evictable, never authoritative. |
| Agent-level composition (`AgentSpec.{Skills,Plugins,...}`) | **Unchanged and complementary.** Agent composition answers "what does *this agent* run with"; plugin composition answers "what is *this reusable bundle* made of". An org curates composed plugins; agents then reference them — one level of indirection product currently lacks. |

## 9. Phasing

- **P1 — API + controller (OSS):** spec refs + validation + `ResolvedComponents` +
  composed manifest/inventory; `compose` package with the §6 rules; fingerprint
  extension. Fully testable hermetically (fake resolver + in-memory bundles).
- **P2 — consumption:** clauderunner compose-at-start (enterprise, AgentCore);
  `arctl plugin pull` (OSS). Resurrect `translate`/`materialize` (identity claude-code
  adapter) as the delivery tail.
- **P3 — kagent delivery:** deploy-time compose → derived OCI artifact → kagent
  skill-mount path. Lands with the kagent harness lane.
- **P4 (product call) — serving composed plugins** to unmodified Claude Code (flattened
  archive endpoint or git-backed marketplace).

## 10. Decisions & open questions

Resolved (Scott, 2026-08-06):

1. **Serving composed plugins:** skip in the compat catalog for v1; serving to
   unmodified Claude Code waits on the git-backed marketplace
   (solo-io/agentregistry-enterprise#1195 — dependency recorded on the issue).
2. **Floating-tag refresh:** freeze-at-spec-change (§5). No refresh policy field in v1.
3. **Nested plugin-in-plugin:** out of scope, not deferred — no target harness supports
   nesting (§4.1).
4. **Commands:** backed by the `Prompt` kind — markdown body materializes to
   `commands/<name>.md`, name = command name. No new kind.
5. **Composition refs are a new kindless `ComponentRef{Namespace,Name,Tag}`** (§4.1) —
   the field determines the kind; `ResourceRef`'s `Kind` was redundant and bug-prone.
6. **Collision policy: overlay wins** (§6) — a ref replaces same-named base content
   atomically, recorded in the compose report; ref-vs-ref duplicates stay a
   validation error.
7. **Approval gating: CONFIRMED already in place** (verified 2026-08-06 against
   enterprise main): `isApprovalGatedKind` = `v1alpha1.IsTaggedArtifactKind`
   (enterprise `internal/registry/approval/service.go`), and Plugin registers with
   default `KindStorageTaggedArtifact` (OSS `kinds.go:138-139`) — every tagged kind is
   auto-gated generically. The historical F-2 gap was closed. Nothing to do.

No open questions remain.
