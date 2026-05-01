package importer

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/v1alpha1store"
)

// Options controls one invocation of Importer.Import.
type Options struct {
	// Path is a file or directory on disk. Directories are walked
	// recursively; every *.yaml, *.yml, *.json file is decoded.
	Path string

	// Namespace is applied to any decoded object whose
	// metadata.namespace is blank. When blank itself, falls back to
	// v1alpha1.DefaultNamespace.
	Namespace string

	// Enrich toggles scanner invocation. When false, importer skips
	// every registered Scanner and writes no rows to enrichment_findings.
	Enrich bool

	// WhichScans narrows enrichment to the named Scanner.Name() values.
	// Empty slice + Enrich=true runs every registered scanner that
	// Supports() the object.
	WhichScans []string

	// DryRun skips the Upsert + findings write. Validation + scanner
	// invocation still happen so users can preview what an import would
	// produce. Scanner side-effects (external API calls) still occur.
	DryRun bool

	// ScannedBy is recorded as the provenance string on every finding
	// and as the AnnoLastScannedBy annotation. Defaults to
	// "importer-cli" when blank.
	ScannedBy string

	// PerObjectAuthorize, when non-nil, is invoked once per decoded
	// object after validation + ref/registry/remote-URL checks but
	// BEFORE Upsert. A non-nil error fails the per-doc
	// ImportResult with Status=ImportStatusFailed; the rest of the
	// stream still runs.
	//
	// HTTP callers (POST /v0/import) wire this from the same
	// PerKindHooks.Authorizers map the regular apply path consults so
	// the import surface enforces the same per-kind RBAC. Nil is the
	// non-HTTP default (admin context, no per-call gate).
	//
	// Object identity is fully populated by the time this fires —
	// metadata.namespace has been defaulted, validation has passed,
	// labels/annotations from scanners are NOT yet applied (those run
	// only on enrich + after authz, so an authz failure can't leak
	// scanner-derived state).
	PerObjectAuthorize func(ctx context.Context, obj v1alpha1.Object) error
}

// ImportResult is the per-object outcome of Importer.Import. One
// result is produced per decoded document, including ones that fail
// to validate or upsert. JSON tags are authoritative for the
// POST /v0/import wire format — the HTTP handler serializes this
// struct directly.
type ImportResult struct {
	// Source file path the object was decoded from. Empty if decoded
	// inline.
	Source string `json:"source,omitempty"`

	// Identity of the imported object. Zero if the document failed to
	// decode before reaching a typed object.
	Kind      string `json:"kind,omitempty"`
	Namespace string `json:"namespace,omitempty"`
	Name      string `json:"name,omitempty"`
	Version   string `json:"version,omitempty"`

	// Status is one of "created" | "updated" | "unchanged" | "failed"
	// | "dry-run". Matches the apply-handler vocabulary.
	Status string `json:"status"`

	// EnrichmentStatus is "skipped" (Enrich=false or no supporting
	// scanner) | "ok" (all supporting scanners succeeded) | "partial"
	// (at least one scanner errored, others succeeded) | "failed"
	// (every supporting scanner errored).
	EnrichmentStatus string `json:"enrichmentStatus,omitempty"`

	// EnrichmentErrors carries the formatted error from each scanner
	// that failed. Empty when EnrichmentStatus is "ok" or "skipped".
	EnrichmentErrors []string `json:"enrichmentErrors,omitempty"`

	// Error is the import-level failure message for Status="failed".
	Error string `json:"error,omitempty"`

	// Generation is the server-assigned generation after Upsert. Zero
	// for failed or dry-run results.
	Generation int64 `json:"generation,omitempty"`
}

const (
	ImportStatusCreated   = "created"
	ImportStatusUpdated   = "updated"
	ImportStatusUnchanged = "unchanged"
	ImportStatusFailed    = "failed"
	ImportStatusDryRun    = "dry-run"

	EnrichmentStatusSkipped = "skipped"
	EnrichmentStatusOK      = "ok"
	EnrichmentStatusPartial = "partial"
	EnrichmentStatusFailed  = "failed"

	defaultScannedBy = "importer-cli"
)

// Importer wires decoded v1alpha1 manifests through validation,
// enrichment, and persistence. One Importer is built per-server with
// the kinds it knows about + the scanners enterprise or OSS
// registered; callers invoke Import repeatedly per user request.
type Importer struct {
	// Stores maps Kind ("Agent", "MCPServer", ...) to the generic
	// v1alpha1store.Store backing that kind's table. Must be populated for
	// every kind the importer is expected to handle; missing entries
	// turn into Status="failed" results.
	stores map[string]*v1alpha1store.Store

	// Findings is the writer for v1alpha1.enrichment_findings. May be
	// nil, in which case findings are discarded and a warning is
	// logged once per scan.
	findings *FindingsStore

	// Scanners are consulted in registration order. Each one's
	// Supports(obj) gates whether Scan runs.
	scanners []Scanner

	// Scheme decodes incoming bytes. Defaults to v1alpha1.Default.
	scheme *v1alpha1.Scheme

	// Resolver forwards to obj.ResolveRefs on each decoded object. A
	// nil resolver skips ref checking. Callers may pass nil when
	// importing a set that is allowed to reference objects that don't
	// exist yet. Typical servers pass a
	// namespace-aware cross-kind resolver that hits the generic Store.
	resolver v1alpha1.ResolverFunc

	// RegistryValidator forwards to obj.ValidateRegistries on each
	// decoded object. A nil validator skips external-registry
	// checks — useful for offline imports, air-gapped deployments,
	// or tests. Typical servers pass registries.Dispatcher.
	registryValidator v1alpha1.RegistryValidatorFunc

	logger *slog.Logger
}

// Config carries the dependencies a new Importer needs.
type Config struct {
	Stores            map[string]*v1alpha1store.Store
	Findings          *FindingsStore
	Scanners          []Scanner
	Scheme            *v1alpha1.Scheme
	Resolver          v1alpha1.ResolverFunc
	RegistryValidator v1alpha1.RegistryValidatorFunc
	Logger            *slog.Logger
}

// New wires a configured Importer. Stores is required; everything else
// is optional.
func New(cfg Config) (*Importer, error) {
	if cfg.Stores == nil {
		return nil, errors.New("importer: Config.Stores is required")
	}
	scheme := cfg.Scheme
	if scheme == nil {
		scheme = v1alpha1.Default
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default().With("component", "importer")
	}
	return &Importer{
		stores:            cfg.Stores,
		findings:          cfg.Findings,
		scanners:          append([]Scanner(nil), cfg.Scanners...),
		scheme:            scheme,
		resolver:          cfg.Resolver,
		registryValidator: cfg.RegistryValidator,
		logger:            logger,
	}, nil
}

// Import walks opts.Path, decodes every YAML/JSON document below it,
// and runs each through the import pipeline.
//
// Document-level failures are captured in the returned ImportResults
// with Status="failed" and do not short-circuit the batch. A returned
// top-level error means the walk itself failed (bad path, filesystem
// error) — at that point no results were produced.
func (i *Importer) Import(ctx context.Context, opts Options) ([]ImportResult, error) {
	if opts.Path == "" {
		return nil, errors.New("importer: Options.Path is required")
	}
	files, err := collectFiles(opts.Path)
	if err != nil {
		return nil, err
	}
	if opts.ScannedBy == "" {
		opts.ScannedBy = defaultScannedBy
	}

	out := make([]ImportResult, 0, len(files))
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			out = append(out, ImportResult{
				Source: f,
				Status: ImportStatusFailed,
				Error:  fmt.Sprintf("read file: %v", err),
			})
			continue
		}
		out = append(out, i.importStream(ctx, f, data, opts)...)
	}
	return out, nil
}

// ImportBytes runs the import pipeline over an in-memory multi-doc
// YAML/JSON stream. Used by HTTP callers that already have the
// manifest bytes (e.g. `POST /v0/import`). Source is a free-form
// label (filename, request ID, etc.) recorded on every result for
// debugging; callers without one can pass "".
//
// Unlike Import, ImportBytes does NOT default opts.ScannedBy —
// callers that care about provenance (HTTP server, tests, custom
// pipelines) set it explicitly so the enrichment_findings.scanned_by
// column carries the right label. Blank ScannedBy is written as-is.
//
// Top-level decode failures return a single failed ImportResult —
// never an error — so callers can treat the return shape the same
// as Import.
func (i *Importer) ImportBytes(ctx context.Context, source string, data []byte, opts Options) []ImportResult {
	return i.importStream(ctx, source, data, opts)
}

// importStream decodes one multi-doc YAML/JSON blob and runs each
// document through importOne. Shared by Import (per-file loop) and
// ImportBytes (single call).
func (i *Importer) importStream(ctx context.Context, source string, data []byte, opts Options) []ImportResult {
	docs, err := i.scheme.DecodeMulti(data)
	if err != nil {
		return []ImportResult{{
			Source: source,
			Status: ImportStatusFailed,
			Error:  fmt.Sprintf("decode: %v", err),
		}}
	}
	out := make([]ImportResult, 0, len(docs))
	for _, d := range docs {
		obj, ok := d.(v1alpha1.Object)
		if !ok {
			out = append(out, ImportResult{
				Source: source,
				Status: ImportStatusFailed,
				Error:  fmt.Sprintf("decoded value does not satisfy v1alpha1.Object: %T", d),
			})
			continue
		}
		out = append(out, i.importOne(ctx, source, obj, opts))
	}
	return out
}

// importOne runs one decoded object through validate → enrich →
// upsert → findings-write. Never errors; failures come back as the
// ImportResult.
func (i *Importer) importOne(ctx context.Context, source string, obj v1alpha1.Object, opts Options) ImportResult {
	meta := obj.GetMetadata()
	kind := obj.GetKind()

	res := ImportResult{
		Source:           source,
		Kind:             kind,
		Namespace:        meta.Namespace,
		Name:             meta.Name,
		Version:          meta.Version,
		EnrichmentStatus: EnrichmentStatusSkipped,
	}

	// Default namespace chain: opts.Namespace wins over the v1alpha1
	// default, but an explicit metadata.namespace always wins.
	if meta.Namespace == "" {
		meta.Namespace = opts.Namespace
		if meta.Namespace == "" {
			meta.Namespace = v1alpha1.DefaultNamespace
		}
		obj.SetMetadata(*meta)
		res.Namespace = meta.Namespace
	}

	store, ok := i.stores[kind]
	if !ok || store == nil {
		res.Status = ImportStatusFailed
		res.Error = fmt.Sprintf("unknown or unconfigured kind %q", kind)
		return res
	}

	if err := v1alpha1.ValidateObject(obj); err != nil {
		res.Status = ImportStatusFailed
		res.Error = "validation: " + err.Error()
		return res
	}
	if err := v1alpha1.ResolveObjectRefs(ctx, obj, i.resolver); err != nil {
		res.Status = ImportStatusFailed
		res.Error = "refs: " + err.Error()
		return res
	}
	if err := v1alpha1.ValidateObjectRegistries(ctx, obj, i.registryValidator); err != nil {
		res.Status = ImportStatusFailed
		res.Error = "registries: " + err.Error()
		return res
	}
	// Per-object authz gate. Mirrors the apply pipeline's Authorize
	// call (pkg/registry/resource/apply.go:prepareApplyDoc). Wired by
	// the HTTP /v0/import handler from PerKindHooks.Authorizers so
	// callers without role grants for a kind can't bypass per-kind
	// RBAC by routing writes through the import endpoint.
	if opts.PerObjectAuthorize != nil {
		if err := opts.PerObjectAuthorize(ctx, obj); err != nil {
			res.Status = ImportStatusFailed
			res.Error = "authorize: " + err.Error()
			return res
		}
	}

	// Enrichment: mutate obj's annotations/labels in place, accumulate
	// per-source findings to write after Upsert. Scanners run against
	// the fully-populated object so they see user-authored labels too.
	var pendingFindings map[string][]Finding
	if opts.Enrich {
		pendingFindings, res.EnrichmentStatus, res.EnrichmentErrors = i.runScanners(ctx, obj, opts)
	}

	if opts.DryRun {
		res.Status = ImportStatusDryRun
		return res
	}

	specJSON, err := obj.MarshalSpec()
	if err != nil {
		res.Status = ImportStatusFailed
		res.Error = "marshal spec: " + err.Error()
		return res
	}

	upsertOpts := v1alpha1store.UpsertOpts{Labels: meta.Labels}
	if meta.Annotations != nil {
		upsertOpts.Annotations = meta.Annotations
	}
	up, err := store.Upsert(ctx, meta.Namespace, meta.Name, meta.Version, specJSON, upsertOpts)
	if err != nil {
		res.Status = ImportStatusFailed
		res.Error = "upsert: " + err.Error()
		return res
	}
	switch {
	case up.Created:
		res.Status = ImportStatusCreated
	case up.SpecChanged:
		res.Status = ImportStatusUpdated
	default:
		res.Status = ImportStatusUnchanged
	}
	res.Generation = up.Generation

	i.writeFindings(ctx, obj, opts, pendingFindings, &res)
	return res
}

// writeFindings persists per-scanner findings after a successful Upsert.
// A nil FindingsStore means the caller opted out of persisting findings
// (e.g. a mode that only wants annotation summaries); log and move on.
// Per-source write failures don't roll back the Upsert but downgrade
// EnrichmentStatus so callers see the detail table may be stale.
func (i *Importer) writeFindings(ctx context.Context, obj v1alpha1.Object, opts Options, pending map[string][]Finding, res *ImportResult) {
	if len(pending) == 0 {
		return
	}
	meta := obj.GetMetadata()
	if i.findings == nil {
		i.logger.Warn("findings produced but no FindingsStore configured; dropping",
			"kind", obj.GetKind(), "name", meta.Name, "version", meta.Version)
		return
	}
	for source, fs := range pending {
		if err := i.findings.Replace(ctx,
			obj.GetKind(), meta.Namespace, meta.Name, meta.Version,
			source, opts.ScannedBy, fs,
		); err != nil {
			res.EnrichmentErrors = append(res.EnrichmentErrors,
				fmt.Sprintf("write findings (%s): %v", source, err))
			if res.EnrichmentStatus == EnrichmentStatusOK {
				res.EnrichmentStatus = EnrichmentStatusPartial
			}
		}
	}
}

// runScanners fans each registered scanner that Supports(obj) over the
// object; accumulates annotations + labels into obj's metadata; returns
// per-source findings for the caller to persist after Upsert.
//
// When opts.WhichScans is non-empty, only scanners whose Name() matches
// one of its entries are considered. Unknown scanner names in
// WhichScans are silently ignored (they may be enterprise scanners
// present in a different build).
func (i *Importer) runScanners(ctx context.Context, obj v1alpha1.Object, opts Options) (map[string][]Finding, string, []string) {
	want := opts.WhichScans
	filtered := make([]Scanner, 0, len(i.scanners))
	for _, sc := range i.scanners {
		if len(want) > 0 && !slices.Contains(want, sc.Name()) {
			continue
		}
		if !sc.Supports(obj) {
			continue
		}
		filtered = append(filtered, sc)
	}
	if len(filtered) == 0 {
		return nil, EnrichmentStatusSkipped, nil
	}

	pending := make(map[string][]Finding, len(filtered))
	var errs []string
	ok := 0
	for _, sc := range filtered {
		r, err := sc.Scan(ctx, obj)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", sc.Name(), err))
			i.logger.Warn("scanner failed",
				"scanner", sc.Name(),
				"kind", obj.GetKind(),
				"name", obj.GetMetadata().Name,
				"error", err,
			)
			continue
		}
		mergeAnnotations(obj, r.Annotations)
		mergeLabels(obj, r.Labels)
		if len(r.Findings) > 0 {
			pending[sc.Name()] = r.Findings
		}
		ok++
	}

	// Stamp universal provenance annotations once we've run at least
	// one scanner successfully; skip otherwise so the user sees the
	// old last-scanned-at if a rescan failed entirely.
	if ok > 0 {
		mergeAnnotations(obj, map[string]string{
			AnnoLastScannedAt: time.Now().UTC().Format(time.RFC3339),
			AnnoLastScannedBy: opts.ScannedBy,
		})
	}

	status := EnrichmentStatusOK
	switch {
	case ok == 0:
		status = EnrichmentStatusFailed
	case len(errs) > 0:
		status = EnrichmentStatusPartial
	}
	return pending, status, errs
}

// mergeAnnotations writes src into obj.Metadata.Annotations, creating
// the map lazily. Existing keys are overwritten — scanners are
// authoritative for their own keys.
func mergeAnnotations(obj v1alpha1.Object, src map[string]string) {
	if len(src) == 0 {
		return
	}
	m := obj.GetMetadata()
	if m.Annotations == nil {
		m.Annotations = make(map[string]string, len(src))
	}
	maps.Copy(m.Annotations, src)
	obj.SetMetadata(*m)
}

// mergeLabels writes src into obj.Metadata.Labels, creating the map
// lazily. Existing keys are overwritten.
func mergeLabels(obj v1alpha1.Object, src map[string]string) {
	if len(src) == 0 {
		return
	}
	m := obj.GetMetadata()
	if m.Labels == nil {
		m.Labels = make(map[string]string, len(src))
	}
	maps.Copy(m.Labels, src)
	obj.SetMetadata(*m)
}

// collectFiles returns every regular file under path with a .yaml /
// .yml / .json suffix. A single-file path passes through as-is
// (suffix check skipped) so callers can point at an explicit manifest
// file without renaming it.
func collectFiles(path string) ([]string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("stat %q: %w", path, err)
	}
	if !info.IsDir() {
		return []string{path}, nil
	}
	var out []string
	err = filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(p))
		if ext == ".yaml" || ext == ".yml" || ext == ".json" {
			out = append(out, p)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk %q: %w", path, err)
	}
	return out, nil
}
