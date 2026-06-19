package store

import (
	"fmt"
	"io"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/static"
	"github.com/google/go-containerregistry/pkg/v1/types"
)

const (
	// ArtifactType is the manifest config media type for a plugin bundle. OCI
	// re-exposes it as Descriptor.ArtifactType when the artifact is referenced
	// (there is no top-level manifest.artifactType field in this client).
	ArtifactType = "application/vnd.agentregistry.plugin.bundle.config.v1+json"
	// LayerMediaType marks the single layer as the uncompressed canonical tar.
	LayerMediaType = "application/vnd.agentregistry.plugin.bundle.layer.v1.tar"
)

// buildArtifact wraps the canonical tar bytes in a single-layer OCI artifact:
// an OCI-format manifest whose config media type is ArtifactType and whose one
// layer is the uncompressed tar.
func buildArtifact(tarBytes []byte, annotations map[string]string) (v1.Image, error) {
	layer := static.NewLayer(tarBytes, types.MediaType(LayerMediaType))
	img, err := mutate.AppendLayers(empty.Image, layer)
	if err != nil {
		return nil, fmt.Errorf("append bundle layer: %w", err)
	}
	img = mutate.ConfigMediaType(img, types.MediaType(ArtifactType))
	img = mutate.MediaType(img, types.OCIManifestSchema1)
	if len(annotations) > 0 {
		annotated, ok := mutate.Annotations(img, annotations).(v1.Image)
		if !ok {
			return nil, fmt.Errorf("annotate artifact: unexpected type")
		}
		img = annotated
	}
	return img, nil
}

// artifactTarBytes extracts the single canonical-tar layer from a plugin
// artifact image.
func artifactTarBytes(img v1.Image) ([]byte, error) {
	layers, err := img.Layers()
	if err != nil {
		return nil, err
	}
	if len(layers) != 1 {
		return nil, fmt.Errorf("%w: expected exactly 1 layer, got %d", ErrInvalidBundle, len(layers))
	}
	// The layer is stored uncompressed with a non-gzip media type, so the
	// uncompressed stream is the literal tar we wrote.
	rc, err := layers[0].Uncompressed()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(rc)
}
