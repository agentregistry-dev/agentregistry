package database

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// vectorLiteral converts a slice of float32 values into the textual representation expected by pgvector.
func vectorLiteral(vec []float32) (string, error) {
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
