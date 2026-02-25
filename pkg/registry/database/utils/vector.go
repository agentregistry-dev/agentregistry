package utils

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
)

const SemanticMetadataKey = "aregistry.ai/semantic"

// AnnotateServerSemanticScore annotates a server JSON with a semantic score.
func AnnotateServerSemanticScore(server *apiv0.ServerJSON, score float64) {
	if server == nil {
		return
	}
	if server.Meta == nil {
		server.Meta = &apiv0.ServerMeta{}
	}
	if server.Meta.PublisherProvided == nil {
		server.Meta.PublisherProvided = map[string]any{}
	}
	server.Meta.PublisherProvided[SemanticMetadataKey] = map[string]any{
		"score": score,
	}
}

// VectorLiteral converts a slice of float32 values into the textual representation expected by pgvector.
func VectorLiteral(vec []float32) (string, error) {
	if len(vec) == 0 {
		return "", fmt.Errorf("vector must not be empty")
	}
	var b strings.Builder
	b.Grow(len(vec) * 12)
	b.WriteByte('[')
	for i, v := range vec {
		if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
			return "", fmt.Errorf("vector contains invalid value at index %d", i)
		}
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(strconv.FormatFloat(float64(v), 'g', -1, 32))
	}
	b.WriteByte(']')
	return b.String(), nil
}
